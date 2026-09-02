#!/usr/bin/env python3
"""Interactive kagent chat with streaming, tools, sessions, and HITL."""

import json
import os
import queue
import re
import select
import codecs
import subprocess
import sys
import tempfile
import termios
import threading
import time
import tty
import urllib.error
import urllib.parse
import urllib.request
import uuid


class ExitChat(Exception):
    pass


def stop_process(process):
    if process.poll() is not None:
        return
    process.terminate()
    try:
        process.wait(timeout=2)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait()


def wait_for_agent(kubectl, context, agent, timeout=120):
    path = f"/api/v1/namespaces/kagent/services/{agent}:8080/proxy/.well-known/agent-card.json"
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            result = subprocess.run(
                [kubectl, "--context", context, "-n", "kagent", "get", "--raw", path],
                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False, timeout=5,
            )
        except subprocess.TimeoutExpired:
            continue
        if result.returncode == 0:
            return
        time.sleep(1)
    raise RuntimeError(f"agent '{agent}' is not answering through its Service after {timeout}s")


def start_forward(kubectl, context, port):
    log = tempfile.TemporaryFile(mode="w+")
    process = subprocess.Popen(
        [kubectl, "--context", context, "-n", "kagent", "port-forward", "--address", "127.0.0.1",
         "svc/kagent-controller", f"{port}:8083"],
        stdout=log, stderr=subprocess.STDOUT, text=True,
    )
    for _ in range(80):
        log.seek(0)
        if f"Forwarding from 127.0.0.1:{port}" in log.read():
            return process, log
        if process.poll() is not None:
            break
        time.sleep(0.25)
    log.seek(0)
    detail = log.read().strip()
    stop_process(process)
    log.close()
    raise RuntimeError(f"port-forward did not open on 127.0.0.1:{port}" + (f":\n{detail}" if detail else ""))


def read_message(prompt):
    if not sys.stdin.isatty():
        try:
            return input(prompt)
        except EOFError:
            return None
    print(prompt, end="", flush=True)
    fd = sys.stdin.fileno()
    previous = termios.tcgetattr(fd)
    decoder = codecs.getincrementaldecoder("utf-8")()
    chars = []
    try:
        tty.setcbreak(fd)
        while True:
            char = os.read(fd, 1)
            if char == b"\x1b":
                return None
            if char in (b"\r", b"\n"):
                print()
                return "".join(chars)
            if char in (b"\x7f", b"\b"):
                if chars:
                    chars.pop()
                    print("\b \b", end="", flush=True)
                continue
            if char == b"\x03":
                raise KeyboardInterrupt
            if char == b"\x04" and not chars:
                return None
            text = decoder.decode(char)
            if text.isprintable():
                chars.append(text)
                print(text, end="", flush=True)
    finally:
        termios.tcsetattr(fd, termios.TCSADRAIN, previous)


def parts(message):
    return (message or {}).get("parts") or []


def safe_text(value):
    """Remove terminal controls from untrusted agent, tool, and API text."""
    if not isinstance(value, str):
        return ""
    value = re.sub(r"\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))", "", value)
    return "".join(
        char for char in value
        if char in "\n\t" or (ord(char) >= 32 and not 0x7F <= ord(char) <= 0x9F)
    )


def event_text(event):
    message = (event.get("status") or {}).get("message") or {}
    if message.get("role") != "agent":
        return ""
    return "".join(safe_text(p.get("text", "")) for p in parts(message) if p.get("kind") == "text")


def tool_events(event):
    message = (event.get("status") or {}).get("message") or {}
    for part in parts(message):
        if part.get("kind") != "data":
            continue
        kind = (part.get("metadata") or {}).get("kagent_type") or (part.get("metadata") or {}).get("adk_type")
        if kind in {"function_call", "function_response"}:
            yield kind, part.get("data") or {}, part.get("metadata") or {}


def approval_from(event):
    if (event.get("status") or {}).get("state") != "input-required":
        return None
    calls = []
    hint = ""
    for kind, data, metadata in tool_events(event):
        long_running = metadata.get("kagent_is_long_running") or metadata.get("adk_is_long_running")
        if kind != "function_call" or data.get("name") != "adk_request_confirmation" or not long_running:
            continue
        args = data.get("args") or {}
        confirmation = args.get("toolConfirmation") or {}
        payload = confirmation.get("payload") or {}
        hint = hint or confirmation.get("hint", "")
        part_calls = []
        for item in payload.get("hitl_parts") or []:
            call = item.get("originalFunctionCall") or {}
            if call:
                part_calls.append(call)
        if not part_calls:
            call = args.get("originalFunctionCall") or {}
            if call:
                part_calls.append(call)
        calls.extend(part_calls)
    if not calls:
        return None
    ids = [call.get("id") for call in calls]
    if any(not call_id for call_id in ids) or len(ids) != len(set(ids)):
        raise RuntimeError("HITL request has missing or duplicate tool-call IDs")
    return {"task_id": event.get("taskId"), "context_id": event.get("contextId"),
            "hint": safe_text(hint), "calls": calls}


class StreamView:
    def __init__(self, agent, tool_mode="summary"):
        self.agent = agent
        self.tool_mode = tool_mode
        self.context = None
        self.state = None
        self.reply = ""
        self.approval = None
        self.denied = False
        self.started_reply = False
        self.tool_calls = {}
        self.message_text = {}
        self.partial_reply = ""

    def clear_spinner(self):
        if sys.stdout.isatty():
            print("\r\x1b[2K", end="", flush=True)

    def consume(self, event):
        if event.get("contextId"):
            self.context = event["contextId"]
        status = event.get("status") or {}
        if status.get("state"):
            self.state = status["state"]
        approval = approval_from(event)
        if approval:
            self.approval = approval
        for kind, data, _ in tool_events(event):
            name = data.get("name", "unknown")
            if name == "adk_request_confirmation":
                continue
            if self.tool_mode != "off":
                self.clear_spinner()
            if kind == "function_call":
                self.tool_calls[data.get("id", "")] = name
                args = json.dumps(data.get("args") or {}, sort_keys=True)
                if self.tool_mode != "off":
                    print(f"Tool: {safe_text(name)} {safe_text(args)}")
            else:
                name = self.tool_calls.get(data.get("id", ""), name)
                response = data.get("response") or {}
                failed = response.get("isError") is not False
                raw = json.dumps(response)
                if "approval request filed" in raw or "not permitted" in raw:
                    self.denied = True
                if self.tool_mode != "off":
                    print(f"Tool: {safe_text(name)} {'failed' if failed else 'completed'}")
                    if self.tool_mode == "verbose":
                        print("  result: " + safe_text(raw))
        text = event_text(event)
        message = (event.get("status") or {}).get("message") or {}
        metadata = message.get("metadata") or {}
        partial = metadata.get("kagent_adk_partial") is True or metadata.get("adk_partial") is True
        message_id = message.get("messageId") or event.get("taskId") or str(len(self.message_text))
        previous = self.message_text.get(message_id, "")
        if text and text != previous:
            self.clear_spinner()
            if not self.started_reply:
                print(f"{self.agent}: ", end="", flush=True)
                self.started_reply = True
            if self.partial_reply and text.startswith(self.partial_reply):
                addition = text[len(self.partial_reply):]
            else:
                addition = text[len(previous):] if text.startswith(previous) else text
            print(addition, end="", flush=True)
            self.message_text[message_id] = text
            self.reply += addition
            if partial:
                self.partial_reply += addition
            elif self.partial_reply:
                self.partial_reply = ""
        if event.get("kind") == "artifact-update":
            artifact_text = "".join(
                p.get("text", "") for p in parts(event.get("artifact")) if p.get("kind") == "text"
            )
            artifact_text = safe_text(artifact_text)
            if artifact_text and not self.reply:
                self.clear_spinner()
                print(f"{self.agent}: {artifact_text}", end="", flush=True)
                self.reply = artifact_text
                self.started_reply = True


def read_stream(process, view):
    events = queue.Queue()
    errors = []

    def reader():
        try:
            decoder = json.JSONDecoder()
            utf8 = codecs.getincrementaldecoder("utf-8")()
            buffer = ""
            while True:
                raw = os.read(process.stdout.fileno(), 4096)
                chunk = utf8.decode(raw, final=not raw)
                if not chunk:
                    break
                buffer += chunk
                while buffer.strip():
                    start = len(buffer) - len(buffer.lstrip())
                    try:
                        event, end = decoder.raw_decode(buffer, start)
                    except json.JSONDecodeError:
                        break
                    events.put(event)
                    buffer = buffer[end:]
            if buffer.strip():
                events.put(RuntimeError("kagent stream ended with invalid JSON: " + safe_text(buffer[:200])))
        except BaseException as error:
            events.put(error)
        finally:
            events.put(None)

    stdout_thread = threading.Thread(target=reader, daemon=True)
    stdout_thread.start()
    def stderr_reader():
        total = 0
        chunks = []
        for chunk in iter(lambda: process.stderr.read(4096), ""):
            total += len(chunk)
            chunks.append(chunk)
            while sum(map(len, chunks)) > 64 * 1024:
                chunks.pop(0)
        errors.append("".join(chunks))

    stderr_thread = threading.Thread(target=stderr_reader, daemon=True)
    stderr_thread.start()
    started = time.monotonic()
    frames = "|/-\\"
    frame = 0
    tty_input = sys.stdin.isatty() and sys.stdout.isatty()
    fd = sys.stdin.fileno() if tty_input else None
    previous = termios.tcgetattr(fd) if tty_input else None
    try:
        if tty_input:
            tty.setcbreak(fd)
        done = False
        while not done:
            try:
                line = events.get(timeout=0.1)
            except queue.Empty:
                line = ""
            if line is None:
                done = True
            elif isinstance(line, Exception):
                raise line
            elif line:
                if not isinstance(line, dict):
                    raise RuntimeError("kagent stream contained a non-object JSON value")
                view.consume(line)
            if process.poll() is None and tty_input:
                elapsed = int(time.monotonic() - started)
                if not view.started_reply:
                    print(f"\r{view.agent}: thinking {frames[frame % 4]} {elapsed}s (Esc to exit)", end="", flush=True)
                    frame += 1
                ready, _, _ = select.select([fd], [], [], 0)
                if ready and os.read(fd, 1) == b"\x1b":
                    stop_process(process)
                    raise ExitChat
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            stop_process(process)
            raise RuntimeError("kagent closed its output but did not exit")
        stdout_thread.join(timeout=2)
        stderr_thread.join(timeout=2)
    except BaseException:
        stop_process(process)
        raise
    finally:
        if tty_input:
            termios.tcsetattr(fd, termios.TCSADRAIN, previous)
        view.clear_spinner()
    stderr = "".join(errors).strip()
    if process.returncode != 0:
        raise RuntimeError(stderr or f"kagent exited {process.returncode}")
    if view.started_reply:
        print()
    if view.state == "input-required" and view.approval:
        return view
    if view.state != "completed" or not view.reply:
        detail = f": {stderr}" if stderr else ""
        raise RuntimeError(f"task did not complete with a reply (state {view.state!r}){detail}")
    return view


def invoke(kagent, url, agent, message, session, tool_mode="summary"):
    command = [kagent, "--kagent-url", url, "invoke", "--stream", "--agent", agent, "--task", message]
    if session:
        command.extend(["--session", session])
    process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, bufsize=1)
    return read_stream(process, StreamView(agent, tool_mode))


def api_json(url, method="GET", body=None):
    data = json.dumps(body).encode() if body is not None else None
    request = urllib.request.Request(url, data=data, method=method,
                                     headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.load(response)
    except (urllib.error.URLError, json.JSONDecodeError) as error:
        raise RuntimeError(f"controller API request failed: {error}") from error


def sessions(base):
    envelope = api_json(base + "/api/sessions")
    data = envelope.get("data") or []
    return data.get("sessions", []) if isinstance(data, dict) else data


def session_detail(base, session_id):
    segment = urllib.parse.quote(session_id, safe="")
    envelope = api_json(base + "/api/sessions/" + segment)
    return envelope.get("data") or {}


def visible_history(detail, show_tools=True):
    output = []
    events = detail.get("events") or []
    events = sorted(enumerate(events), key=lambda item: (item[1].get("created_at", ""), item[0]))
    for _, wrapper in events:
        raw = wrapper.get("data")
        try:
            event = json.loads(raw) if isinstance(raw, str) else raw
        except json.JSONDecodeError:
            continue
        if not isinstance(event, dict):
            continue
        author = event.get("author", "")
        content = event.get("content") or {}
        role = content.get("role")
        event_parts = content.get("parts") or []
        for part in event_parts:
            if part.get("text") and role == "user" and author == "user":
                output.append(("You", safe_text(part["text"])))
            elif part.get("text") and role == "model":
                output.append(("Agent", safe_text(part["text"])))
            elif show_tools and part.get("function_call"):
                call = part["function_call"]
                output.append(("Tool", safe_text(f"{call.get('name')} {json.dumps(call.get('args') or {}, sort_keys=True)}")))
            elif show_tools and part.get("function_response"):
                response = part["function_response"]
                failed = (response.get("response") or {}).get("isError") is not False
                output.append(("Tool", safe_text(f"{response.get('name')} {'failed' if failed else 'completed'}")))
    return output


def show_history(base, session_id, agent):
    detail = session_detail(base, session_id)
    stored = (detail.get("session") or {}).get("agent_id", "")
    expected = "kagent__NS__" + agent.replace("-", "_")
    if stored and stored != expected:
        owner = stored.replace("kagent__NS__", "").replace("_", "-")
        raise RuntimeError(f"session belongs to {owner}, not {agent}")
    print(f"History for {agent} ({session_id})")
    for speaker, text in visible_history(detail):
        label = agent if speaker == "Agent" else speaker
        print(f"{label}: {text}")
    print()


def show_sessions(base):
    items = sessions(base)
    if not items:
        print("No sessions found.\n")
        return
    print("Recent sessions")
    for item in items[:20]:
        agent = item.get("agent_id") or item.get("agent_name") or item.get("agent") or "unknown"
        agent = agent.replace("kagent__NS__", "").replace("_", "-")
        print(f"  {safe_text(item.get('id'))}  {safe_text(agent)}  {safe_text(item.get('name', ''))}")
    print()


def send_hitl(base, agent, approval, decision_data):
    message_id = str(uuid.uuid4())
    label = "HITL decision submitted"
    body = {"jsonrpc": "2.0", "id": message_id, "method": "message/stream", "params": {"message": {
        "kind": "message", "role": "user", "messageId": message_id,
        "taskId": approval["task_id"], "contextId": approval["context_id"],
        "parts": [{"kind": "data", "data": decision_data, "metadata": {}},
                  {"kind": "text", "text": label}],
    }}}
    request = urllib.request.Request(base + f"/api/a2a/kagent/{agent}/",
                                     data=json.dumps(body).encode(), method="POST",
                                     headers={"Content-Type": "application/json", "Accept": "text/event-stream",
                                              "x-user-id": "admin@kagent.dev"})
    try:
        response = urllib.request.urlopen(request, timeout=300)
    except urllib.error.URLError as error:
        raise RuntimeError(f"HITL decision failed: {error}") from error
    view = StreamView(agent)
    try:
        for raw in response:
            line = raw.decode().strip()
            if line.startswith("data:"):
                payload = line[5:].lstrip()
                if payload and payload != "[DONE]":
                    event = json.loads(payload)
                    if not isinstance(event, dict):
                        raise RuntimeError("HITL stream contained a non-object JSON value")
                    if event.get("error"):
                        raise RuntimeError("HITL response failed: " + safe_text(json.dumps(event["error"])))
                    event = event.get("result", event)
                    if not isinstance(event, dict):
                        raise RuntimeError("HITL result contained a non-object value")
                    view.consume(event)
    except (OSError, json.JSONDecodeError) as error:
        raise RuntimeError(f"HITL response stream failed: {error}") from error
    finally:
        response.close()
    if view.started_reply:
        print()
    if view.state == "input-required" and view.approval:
        return view
    if view.state != "completed" or not view.reply:
        raise RuntimeError(f"HITL task did not complete with a reply (state {view.state!r})")
    return view


def ask_user_decision(call):
    questions = (call.get("args") or {}).get("questions") or []
    answers = []
    print("Agent needs your input")
    for question in questions:
        print("  " + safe_text(question.get("question", "Answer required")))
        choices = question.get("choices") or []
        if choices:
            print("  Choices: " + safe_text(", ".join(choices)))
        answer = read_message("  Answer: ")
        if answer is None:
            raise ExitChat
        values = [item.strip() for item in answer.split(",") if item.strip()]
        answers.append({"answer": values})
    return {"decision_type": "approve", "ask_user_answers": answers}


def prompt_approval(approval):
    print("Human approval required")
    if approval.get("hint"):
        print("  " + approval["hint"])
    calls = approval.get("calls") or []
    if len(calls) == 1 and calls[0].get("name") == "ask_user":
        return ask_user_decision(calls[0])
    decisions = {}
    reasons = {}
    for call in calls:
        print(f"  Tool: {safe_text(call.get('name'))}")
        print("  Args: " + safe_text(json.dumps(call.get("args") or {}, sort_keys=True)))
        while True:
            answer = read_message("  Approve? [y/N]: ")
            if answer is None:
                raise ExitChat
            answer = answer.strip().lower()
            if answer in {"y", "yes", "", "n", "no"}:
                break
        approved = answer in {"y", "yes"}
        decisions[call.get("id", "")] = "approve" if approved else "reject"
        if not approved:
            reason = read_message("  Rejection reason (optional): ")
            if reason is None:
                raise ExitChat
            if reason.strip():
                reasons[call.get("id", "")] = reason.strip()
    unique = set(decisions.values())
    if len(decisions) == 1 and not reasons:
        return {"decision_type": next(iter(unique))}
    result = {"decision_type": "batch", "decisions": decisions}
    if reasons:
        result["rejection_reasons"] = reasons
    return result


HELP = """Commands:
  /help           show commands
  /session        show the active session ID
  /sessions       list recent sessions
  /history        reload and show active session history
  /resume <id>    switch to a session and show its history
  /new            start a new session
  /retry          resend the last message explicitly
  /tools off|summary|verbose
                  choose tool activity detail
  /exit, /quit    end chat
"""


def run(agent, context, port, kagent, kubectl, session):
    wait_for_agent(kubectl, context, agent)
    forward, log = start_forward(kubectl, context, port)
    base = f"http://127.0.0.1:{port}"
    last_message = None
    tool_mode = "summary"
    try:
        print(f"Active agent: {agent}")
        print("Esc to exit")
        print("Type /help for commands.")
        if session:
            show_history(base, session, agent)
        else:
            print()
        while True:
            message = read_message("You: ")
            if message is None or message.strip() in {"/exit", "/quit"}:
                print("Chat ended.")
                return 0
            message = message.strip()
            if not message:
                continue
            if message == "/help":
                print(HELP)
                continue
            if message == "/session":
                print((session or "No active session yet.") + "\n")
                continue
            if message == "/sessions":
                show_sessions(base)
                continue
            if message == "/history":
                if session:
                    show_history(base, session, agent)
                else:
                    print("No active session yet.\n")
                continue
            if message.startswith("/resume "):
                candidate = message.split(None, 1)[1]
                try:
                    uuid.UUID(candidate)
                    show_history(base, candidate, agent)
                except (ValueError, RuntimeError) as error:
                    print(f"Cannot resume: {error}\n")
                    continue
                session = candidate
                continue
            if message == "/new":
                session = None
                last_message = None
                print("Started a new session.\n")
                continue
            if message.startswith("/tools "):
                value = message.split(None, 1)[1].lower()
                if value not in {"off", "summary", "verbose"}:
                    print("Usage: /tools off|summary|verbose\n")
                else:
                    tool_mode = value
                    print(f"Tool display: {value}\n")
                continue
            if message == "/retry":
                if not last_message:
                    print("Nothing to retry.\n")
                    continue
                message = last_message
                print(f"Retrying: {message}")
            elif message.startswith("/"):
                print("Unknown command. Type /help.\n")
                continue
            else:
                last_message = message
            view = invoke(kagent, base, agent, message, session, tool_mode)
            session = view.context or session
            while view.approval:
                decision_data = prompt_approval(view.approval)
                view = send_hitl(base, agent, view.approval, decision_data)
                session = view.context or session
            if view.denied:
                print("Kaimahi denied a tool call and may have filed an approval request.")
                print("Approve it separately with 'make approvals' and 'make approve', then type /retry.\n")
            else:
                print()
    finally:
        stop_process(forward)
        log.close()


def selftest():
    view = StreamView("agent")
    view.consume({"kind": "status-update", "contextId": "s1", "status": {"state": "working", "message": {
        "parts": [{"kind": "data", "metadata": {"kagent_type": "function_call"},
                   "data": {"id": "1", "name": "get", "args": {"x": 1}}}]}}})
    view.consume({"kind": "status-update", "contextId": "s1", "status": {"state": "working", "message": {
        "role": "agent", "parts": [{"kind": "text", "text": "hello"}]}}})
    view.consume({"kind": "status-update", "contextId": "s1", "final": True,
                  "status": {"state": "completed"}})
    assert view.context == "s1" and view.state == "completed" and view.reply == "hello"
    approval = approval_from({"kind": "status-update", "taskId": "t1", "contextId": "s1",
        "status": {"state": "input-required", "message": {"parts": [{"kind": "data",
        "metadata": {"kagent_type": "function_call", "kagent_is_long_running": True},
        "data": {"name": "adk_request_confirmation", "args": {"originalFunctionCall":
        {"id": "c1", "name": "delete", "args": {"path": "/tmp/x"}}}}}]}}})
    assert approval["calls"][0]["name"] == "delete"
    ask = approval_from({"kind": "status-update", "taskId": "t2", "contextId": "s2",
        "status": {"state": "input-required", "message": {"parts": [{"kind": "data",
        "metadata": {"kagent_type": "function_call", "kagent_is_long_running": True},
        "data": {"name": "adk_request_confirmation", "args": {"originalFunctionCall":
        {"id": "c2", "name": "ask_user", "args": {"questions": [{"question": "Which?"}]}}}}}]}}})
    assert ask["calls"][0]["name"] == "ask_user"
    history = visible_history({"events": [
        {"created_at": "2", "data": json.dumps({"author": "agent", "content": {"role": "model",
            "parts": [{"text": "answer"}]}})},
        {"created_at": "1", "data": json.dumps({"author": "user", "content": {"role": "user",
            "parts": [{"text": "question"}]}})},
    ]})
    assert history == [("You", "question"), ("Agent", "answer")]
    stream_view = StreamView("agent")
    stream_view.consume({"kind": "status-update", "contextId": "s2", "status": {"state": "submitted",
        "message": {"role": "user", "parts": [{"kind": "text", "text": "do not echo"}]}}})
    assert stream_view.reply == ""
    chunks = StreamView("agent")
    chunks.consume({"kind": "status-update", "status": {"state": "working", "message": {
        "role": "agent", "messageId": "partial-1", "metadata": {"kagent_adk_partial": True},
        "parts": [{"kind": "text", "text": "Hello"}]}}})
    chunks.consume({"kind": "status-update", "status": {"state": "working", "message": {
        "role": "agent", "messageId": "partial-2", "metadata": {"kagent_adk_partial": True},
        "parts": [{"kind": "text", "text": ", world"}]}}})
    chunks.consume({"kind": "status-update", "status": {"state": "working", "message": {
        "role": "agent", "messageId": "complete", "metadata": {"kagent_adk_partial": None},
        "parts": [{"kind": "text", "text": "Hello, world"}]}}})
    assert chunks.reply == "Hello, world"
    denied = StreamView("agent")
    denied.consume({"kind": "status-update", "status": {"state": "working", "message": {
        "parts": [{"kind": "data", "metadata": {"kagent_type": "function_response"},
        "data": {"id": "x", "name": "post", "response": {"isError": True,
        "content": [{"text": "tool not permitted; approval request filed"}]}}}]}}})
    assert denied.denied
    print("interactive-chat: self-test passed")
    return 0


def main():
    if sys.argv[1:] == ["--selftest"]:
        return selftest()
    agent = os.environ.get("KAIMAHI_CHAT_AGENT", "hello-world")
    context = os.environ.get("KAIMAHI_CHAT_CONTEXT", "kind-kaimahi-p1")
    port = int(os.environ.get("KAIMAHI_CHAT_PORT", "8083"))
    kagent = os.environ.get("KAIMAHI_KAGENT", "bin/kagent")
    kubectl = os.environ.get("KAIMAHI_KUBECTL", "kubectl")
    session = os.environ.get("KAIMAHI_CHAT_SESSION") or None
    try:
        return run(agent, context, port, kagent, kubectl, session)
    except KeyboardInterrupt:
        print("\nChat ended.")
        return 130
    except ExitChat:
        print("Chat ended.")
        return 0
    except (RuntimeError, ValueError) as error:
        print(f"interactive chat: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
