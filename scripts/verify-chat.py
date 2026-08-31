#!/usr/bin/env python3
"""Fail-closed check on `make chat` output: require an A2A task with
status.state == "completed" and a non-empty reply. Reads the captured chat
output from the file named in argv[1] (P1 shipped this inline in CI; P2
factors it out so the keyless preset-switch e2e can reuse it).

P3 adds optional positional args for the tool path:
  verify-chat.py FILE [TOOL [SUBSTRING]]
With TOOL, the task history must additionally contain a function_call for
that tool name AND a successful (isError == false) function_response for it
— a plausible-sounding reply without a real MCP invocation fails. With
SUBSTRING, the reply text must contain it (CI passes an unguessable
probe ConfigMap name, so the answer can only come from live cluster data).
"""
import json
import re
import sys

raw = open(sys.argv[1]).read()
tool = sys.argv[2] if len(sys.argv) > 2 else None
needle = sys.argv[3] if len(sys.argv) > 3 else None
# An empty arg (e.g. a $var that failed to expand) must not silently skip
# the check it was meant to enable.
if tool == "" or needle == "":
    sys.exit("empty TOOL/SUBSTRING argument — refusing to skip a check")

m = re.search(r"^\{.*\}$", raw, re.M | re.S)
if not m:
    sys.exit("no JSON task object found in chat output")
d = json.loads(m.group(0))
state = d.get("status", {}).get("state")
texts = [p.get("text", "") for a in d.get("artifacts", [])
         for p in a.get("parts", [])]
reply = "\n".join(t for t in texts if t.strip())
print(f"state={state}\nreply:\n{reply}")
ok = state == "completed" and bool(reply)

if tool:
    calls = responses = 0
    for msg in d.get("history", []):
        for p in msg.get("parts", []):
            if p.get("kind") != "data":
                continue
            data = p.get("data", {})
            kind = (p.get("metadata") or {}).get("kagent_type")
            if kind == "function_call" and data.get("name") == tool:
                calls += 1
            if (kind == "function_response" and data.get("name") == tool
                    and not data.get("response", {}).get("isError", True)):
                responses += 1
    print(f"tool={tool} function_calls={calls} ok_responses={responses}")
    ok = ok and calls > 0 and responses > 0

if needle:
    hit = needle in reply
    print(f"expect={needle!r} in_reply={hit}")
    ok = ok and hit

sys.exit(0 if ok else 1)
