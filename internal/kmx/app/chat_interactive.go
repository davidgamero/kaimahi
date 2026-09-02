package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

const maxControllerResponse = 10 << 20

var controllerClient = &http.Client{Timeout: 30 * time.Second}
var agentNameRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func safeTerminal(s string) string {
	var out strings.Builder
	const (
		normal = iota
		escape
		csi
		osc
		oscEscape
	)
	state := normal
	for _, r := range s {
		switch state {
		case escape:
			switch r {
			case '[':
				state = csi
			case ']':
				state = osc
			default:
				state = normal
			}
			continue
		case csi:
			if r >= '@' && r <= '~' {
				state = normal
			}
			continue
		case osc:
			if r == '\a' {
				state = normal
			} else if r == '\x1b' {
				state = oscEscape
			}
			continue
		case oscEscape:
			if r == '\\' {
				state = normal
			} else {
				state = osc
			}
			continue
		}
		if r == '\x1b' {
			state = escape
			continue
		}
		if r == '\n' || r == '\t' || (!unicode.IsControl(r) && r != '\u007f') {
			out.WriteRune(r)
		}
	}
	return out.String()
}

type streamView struct {
	agent, context, taskID, state, reply string
	toolCalls                            map[string]string
	denied                               bool
	approval                             *hitlRequest
	partials                             string
	approvalErr                          error
}

type hitlRequest struct {
	TaskID, ContextID string
	Calls             []hitlCall
}

type hitlCall struct {
	ID, Name string
	Args     json.RawMessage
}

type askUserQuestion struct {
	Question string   `json:"question"`
	Choices  []string `json:"choices"`
}

type streamEvent struct {
	Kind      string          `json:"kind"`
	ContextID string          `json:"contextId"`
	TaskID    string          `json:"taskId"`
	Final     bool            `json:"final"`
	LastChunk bool            `json:"lastChunk"`
	Status    json.RawMessage `json:"status"`
	Artifact  struct {
		Parts []struct {
			Kind, Text string
		} `json:"parts"`
	} `json:"artifact"`
}

func (a *App) interactiveChat(kagent, agent, initialTask, session string) error {
	if len(agent) > 63 || !agentNameRE.MatchString(agent) {
		return fmt.Errorf("agent name %q is not a valid Kubernetes name", agent)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := a.waitServable(agent); err != nil {
		return err
	}
	stop, err := a.portForward()
	if err != nil {
		return err
	}
	defer stop()
	base := "http://127.0.0.1:" + a.Cfg.ChatPort
	fmt.Fprintf(a.Out, "Active agent: %s\n/exit or Ctrl-C to exit\n", safeTerminal(agent))
	a.showActiveTools(agent)
	if session != "" {
		if err := a.showSessionHistory(base, session, agent); err != nil {
			return err
		}
	}
	fmt.Fprintln(a.Out)

	reader := bufio.NewScanner(a.Stdin)
	last := ""
	for {
		if err := ctx.Err(); err != nil {
			fmt.Fprintln(a.Out, "Chat ended.")
			return nil
		}
		message := ""
		if initialTask != "" {
			message, initialTask = initialTask, ""
			fmt.Fprintf(a.Out, "You: %s\n", safeTerminal(message))
		} else {
			fmt.Fprint(a.Out, "You: ")
			line, err := scanLine(ctx, reader)
			if err != nil {
				if err == io.EOF || ctx.Err() != nil {
					fmt.Fprintln(a.Out, "Chat ended.")
					return nil
				}
				return err
			}
			message = strings.TrimSpace(line)
		}
		switch {
		case message == "/exit" || message == "/quit" || message == "\x1b":
			fmt.Fprintln(a.Out, "Chat ended.")
			return nil
		case message == "/session":
			fmt.Fprintln(a.Out, map[bool]string{true: session, false: "No active session."}[session != ""])
			continue
		case message == "/new":
			session, last = "", ""
			fmt.Fprintln(a.Out, "Started a new session.")
			continue
		case message == "/sessions":
			if err := a.showSessions(base); err != nil {
				fmt.Fprintf(a.Err, "sessions: %v\n", err)
			}
			continue
		case strings.HasPrefix(message, "/resume "):
			candidate := strings.TrimSpace(strings.TrimPrefix(message, "/resume "))
			if err := a.showSessionHistory(base, candidate, agent); err != nil {
				fmt.Fprintf(a.Err, "cannot resume: %v\n", err)
				continue
			}
			session = candidate
			continue
		case message == "/retry":
			if last == "" {
				fmt.Fprintln(a.Out, "Nothing to retry.")
				continue
			}
			message = last
		case message == "":
			continue
		case strings.HasPrefix(message, "/"):
			fmt.Fprintln(a.Out, "Commands: /session /sessions /resume <id> /new /retry /exit")
			continue
		default:
			last = message
		}
		view, err := a.invokeStream(ctx, kagent, base, agent, message, session)
		if err != nil {
			fmt.Fprintf(a.Err, "chat: %v\n", err)
			continue
		}
		session = view.context
		for view.approval != nil {
			if view.approvalErr != nil {
				return view.approvalErr
			}
			if len(view.approval.Calls) > 1 {
				for _, call := range view.approval.Calls {
					if call.Name == "ask_user" {
						return fmt.Errorf("ask_user arrived with other pending approvals — refusing a decision that could approve an unseen tool")
					}
				}
			}
			decision, err := a.promptHITL(ctx, reader, view.approval)
			if err != nil {
				return err
			}
			view, err = a.sendHITL(ctx, base, agent, view.approval, decision)
			if err != nil {
				return err
			}
			session = view.context
		}
		if view.denied {
			fmt.Fprintln(a.Out, "Kaimahi denied a tool call. Approve separately, then type /retry.")
		}
		fmt.Fprintln(a.Out)
	}
}

func scanLine(ctx context.Context, reader *bufio.Scanner) (string, error) {
	type result struct {
		line string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		if reader.Scan() {
			done <- result{line: reader.Text()}
			return
		}
		err := reader.Err()
		if err == nil {
			err = io.EOF
		}
		done <- result{err: err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case value := <-done:
		return value.line, value.err
	}
}

func (a *App) invokeStream(ctx context.Context, kagent, base, agent, task, session string) (*streamView, error) {
	args := []string{"--kagent-url", base, "invoke", "--stream", "--agent", agent, "--task", task}
	if session != "" {
		args = append(args, "--session", session)
	}
	cmd := exec.CommandContext(ctx, kagent, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := os.CreateTemp("", "kmx-chat-stderr-*.log")
	if err != nil {
		return nil, err
	}
	defer func() { stderr.Close(); os.Remove(stderr.Name()) }()
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	view := &streamView{agent: agent, toolCalls: map[string]string{}}
	done := make(chan struct{})
	spinner := isTerminal(a.Err)
	if spinner {
		go func() {
			frames := []string{"|", "/", "-", "\\"}
			for i := 0; ; i++ {
				select {
				case <-done:
					return
				case <-time.After(250 * time.Millisecond):
					fmt.Fprintf(a.Err, "\r%s: thinking %s", agent, frames[i%len(frames)])
				}
			}
		}()
	}
	decodeErr := a.consumeStream(stdout, view)
	close(done)
	if spinner {
		fmt.Fprint(a.Err, "\r\033[2K")
	}
	if decodeErr != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	_, _ = stderr.Seek(0, io.SeekStart)
	errText, _ := io.ReadAll(io.LimitReader(stderr, 64<<10))
	if decodeErr != nil {
		return nil, decodeErr
	}
	if waitErr != nil {
		return nil, fmt.Errorf("kagent invoke: %v: %s", waitErr, safeTerminal(strings.TrimSpace(string(errText))))
	}
	if view.state == "working" || view.state == "submitted" {
		if err := a.waitExistingTask(ctx, base, view); err != nil {
			return nil, err
		}
	}
	if view.state == "input-required" && view.approval != nil {
		if view.approvalErr != nil {
			return nil, view.approvalErr
		}
		return view, nil
	}
	if view.state != "completed" || view.reply == "" {
		return nil, fmt.Errorf("task did not complete with a reply (state %q)", view.state)
	}
	return view, nil
}

func isTerminal(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func (a *App) consumeStream(r io.Reader, view *streamView) error {
	dec := json.NewDecoder(r)
	for {
		var event streamEvent
		if err := dec.Decode(&event); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("invalid kagent stream: %w", err)
		}
		view.consume(event, a.Out)
	}
}

func (v *streamView) consume(event streamEvent, out io.Writer) {
	if event.ContextID != "" {
		v.context = event.ContextID
	}
	if event.TaskID != "" {
		v.taskID = event.TaskID
	}
	if len(event.Status) > 0 {
		var status struct {
			State   string `json:"state"`
			Message struct {
				Role     string `json:"role"`
				Metadata struct {
					Partial bool `json:"kagent_adk_partial"`
				} `json:"metadata"`
				Parts []struct {
					Kind     string          `json:"kind"`
					Text     string          `json:"text"`
					Data     json.RawMessage `json:"data"`
					Metadata struct {
						Type        string `json:"kagent_type"`
						LongRunning bool   `json:"kagent_is_long_running"`
					} `json:"metadata"`
				} `json:"parts"`
			} `json:"message"`
		}
		if json.Unmarshal(event.Status, &status) == nil {
			v.state = status.State
			for _, part := range status.Message.Parts {
				if part.Kind == "text" && status.Message.Role == "agent" && part.Text != "" {
					if v.reply == "" {
						fmt.Fprintf(out, "%s: ", safeTerminal(v.agent))
					}
					addition := part.Text
					if v.partials != "" && strings.HasPrefix(part.Text, v.partials) {
						addition = strings.TrimPrefix(part.Text, v.partials)
					}
					fmt.Fprint(out, safeTerminal(addition))
					v.reply += addition
					if status.Message.Metadata.Partial {
						v.partials += addition
					} else {
						v.partials = ""
					}
				}
				if part.Kind == "data" {
					v.consumeTool(part.Metadata.Type, part.Metadata.LongRunning, part.Data, out)
				}
			}
		}
	}
	var artifactText strings.Builder
	for _, part := range event.Artifact.Parts {
		if part.Kind != "text" || part.Text == "" {
			continue
		}
		artifactText.WriteString(part.Text)
	}
	text := artifactText.String()
	addition := text
	if strings.HasPrefix(text, v.reply) {
		addition = strings.TrimPrefix(text, v.reply)
	} else if event.LastChunk && strings.HasSuffix(v.reply, text) {
		addition = ""
	}
	if addition != "" {
		if v.reply == "" {
			fmt.Fprintf(out, "%s: ", safeTerminal(v.agent))
		}
		fmt.Fprint(out, safeTerminal(addition))
		v.reply += addition
	}
	if event.LastChunk && len(text) >= len(v.reply) {
		v.reply = text
	}
	if event.Final && v.reply != "" {
		fmt.Fprintln(out)
	}
}

func (v *streamView) consumeTool(kind string, longRunning bool, raw json.RawMessage, out io.Writer) {
	var data struct {
		ID       string          `json:"id"`
		Name     string          `json:"name"`
		Args     json.RawMessage `json:"args"`
		Response struct {
			IsError bool            `json:"isError"`
			Content json.RawMessage `json:"content"`
		} `json:"response"`
	}
	if json.Unmarshal(raw, &data) != nil {
		return
	}
	if kind == "function_call" && longRunning && data.Name == "adk_request_confirmation" {
		var args struct {
			Original     hitlCall `json:"originalFunctionCall"`
			Confirmation struct {
				Payload struct {
					Parts []struct {
						Original hitlCall `json:"originalFunctionCall"`
					} `json:"hitl_parts"`
				} `json:"payload"`
			} `json:"toolConfirmation"`
		}
		if json.Unmarshal(data.Args, &args) == nil {
			if v.approval == nil {
				v.approval = &hitlRequest{TaskID: v.taskID, ContextID: v.context}
			}
			if len(args.Confirmation.Payload.Parts) > 0 {
				for _, part := range args.Confirmation.Payload.Parts {
					if part.Original.ID != "" {
						v.approval.Calls = append(v.approval.Calls, part.Original)
					}
				}
			} else if args.Original.ID != "" {
				v.approval.Calls = append(v.approval.Calls, args.Original)
			} else {
				v.approvalErr = fmt.Errorf("HITL request contained a confirmation without a tool-call ID")
			}
			seen := map[string]bool{}
			for _, call := range v.approval.Calls {
				if seen[call.ID] {
					v.approvalErr = fmt.Errorf("HITL request repeated tool-call ID %q", call.ID)
				}
				seen[call.ID] = true
			}
		}
		return
	}
	if data.Name == "adk_request_confirmation" {
		return
	}
	switch kind {
	case "function_call":
		v.toolCalls[data.ID] = data.Name
		fmt.Fprintf(out, "Tool: %s %s\n", safeTerminal(data.Name), safeTerminal(strings.TrimSpace(string(data.Args))))
	case "function_response":
		name := v.toolCalls[data.ID]
		if name == "" {
			name = data.Name
		}
		state := "completed"
		if data.Response.IsError {
			state = "failed"
		}
		fmt.Fprintf(out, "Tool: %s %s\n", safeTerminal(name), state)
		body := string(data.Response.Content)
		if strings.Contains(body, "approval request filed") || strings.Contains(body, "not permitted") {
			v.denied = true
		}
	}
}

func (a *App) waitExistingTask(ctx context.Context, base string, view *streamView) error {
	for i := 0; i < 300; i++ {
		task, err := getTask(ctx, base, view.agent, view.taskID)
		if err != nil {
			return err
		}
		state := task.Status.State
		if state != "working" && state != "submitted" {
			status, _ := json.Marshal(task.Status)
			view.consume(streamEvent{ContextID: task.ContextID, TaskID: task.ID, Status: status}, a.Out)
			var text strings.Builder
			for _, artifact := range task.Artifacts {
				for _, part := range artifact.Parts {
					if part.Kind == "text" {
						text.WriteString(part.Text)
					}
				}
			}
			if value := text.String(); value != "" {
				addition := value
				if strings.HasPrefix(value, view.reply) {
					addition = strings.TrimPrefix(value, view.reply)
				}
				if addition != "" {
					if view.reply == "" {
						fmt.Fprintf(a.Out, "%s: ", safeTerminal(view.agent))
					}
					fmt.Fprintln(a.Out, safeTerminal(addition))
					view.reply += addition
				}
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("task %s remained working for 5m", view.taskID)
}

type a2aTask struct {
	ID        string `json:"id"`
	ContextID string `json:"contextId"`
	Status    struct {
		State   string          `json:"state"`
		Message json.RawMessage `json:"message"`
	} `json:"status"`
	Artifacts []struct {
		Parts []struct{ Kind, Text string } `json:"parts"`
	} `json:"artifacts"`
}

func getTask(ctx context.Context, base, agent, id string) (*a2aTask, error) {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "kmx", "method": "tasks/get", "params": map[string]string{"id": id}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/a2a/kagent/"+url.PathEscape(agent)+"/", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-user-id", "admin@kagent.dev")
	resp, err := controllerClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tasks/get answered HTTP %d", resp.StatusCode)
	}
	var envelope struct {
		Result a2aTask        `json:"result"`
		Error  map[string]any `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxControllerResponse)).Decode(&envelope); err != nil {
		return nil, err
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("tasks/get: %s", safeTerminal(fmt.Sprint(envelope.Error)))
	}
	return &envelope.Result, nil
}

func (a *App) promptHITL(ctx context.Context, reader *bufio.Scanner, request *hitlRequest) (map[string]any, error) {
	decisions := map[string]string{}
	for _, call := range request.Calls {
		if call.Name == "ask_user" {
			var request struct {
				Questions []askUserQuestion `json:"questions"`
			}
			if err := json.Unmarshal(call.Args, &request); err != nil || len(request.Questions) == 0 {
				return nil, fmt.Errorf("ask_user request has no valid questions")
			}
			answers := make([]map[string][]string, 0, len(request.Questions))
			fmt.Fprintln(a.Out, "Agent needs your input")
			for _, question := range request.Questions {
				fmt.Fprintf(a.Out, "  %s\n", safeTerminal(question.Question))
				if len(question.Choices) > 0 {
					fmt.Fprintf(a.Out, "  Choices: %s\n", safeTerminal(strings.Join(question.Choices, ", ")))
				}
				fmt.Fprint(a.Out, "  Answer: ")
				answer, err := scanLine(ctx, reader)
				if err != nil {
					return nil, err
				}
				var values []string
				for _, value := range strings.Split(answer, ",") {
					if value = strings.TrimSpace(value); value != "" {
						values = append(values, value)
					}
				}
				answers = append(answers, map[string][]string{"answer": values})
			}
			return map[string]any{"decision_type": "approve", "ask_user_answers": answers}, nil
		}
		fmt.Fprintf(a.Out, "Human approval required\n  Tool: %s\n  Args: %s\n  Approve? [y/N]: ", safeTerminal(call.Name), safeTerminal(strings.TrimSpace(string(call.Args))))
		answerLine, err := scanLine(ctx, reader)
		if err != nil {
			return nil, err
		}
		answer := strings.ToLower(strings.TrimSpace(answerLine))
		if answer == "y" || answer == "yes" {
			decisions[call.ID] = "approve"
		} else {
			decisions[call.ID] = "reject"
		}
	}
	if len(decisions) == 1 {
		for _, decision := range decisions {
			return map[string]any{"decision_type": decision}, nil
		}
	}
	return map[string]any{"decision_type": "batch", "decisions": decisions}, nil
}

func (a *App) sendHITL(ctx context.Context, base, agent string, approval *hitlRequest, decision map[string]any) (*streamView, error) {
	messageID := fmt.Sprintf("kmx-%d", time.Now().UnixNano())
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": messageID, "method": "message/stream", "params": map[string]any{"message": map[string]any{
		"kind": "message", "role": "user", "messageId": messageID, "taskId": approval.TaskID, "contextId": approval.ContextID,
		"parts": []map[string]any{{"kind": "data", "data": decision, "metadata": map[string]any{}}, {"kind": "text", "text": "HITL decision submitted"}},
	}}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/a2a/kagent/"+url.PathEscape(agent)+"/", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("x-user-id", "admin@kagent.dev")
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HITL decision answered HTTP %d", resp.StatusCode)
	}
	view := &streamView{agent: agent, toolCalls: map[string]string{}}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var envelope struct {
			Result *streamEvent   `json:"result"`
			Error  map[string]any `json:"error"`
		}
		if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
			return nil, fmt.Errorf("invalid HITL stream: %w", err)
		}
		if envelope.Error != nil {
			return nil, fmt.Errorf("HITL decision: %v", envelope.Error)
		}
		if envelope.Result != nil {
			view.consume(*envelope.Result, a.Out)
			continue
		}
		var event streamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return nil, fmt.Errorf("invalid HITL event: %w", err)
		}
		view.consume(event, a.Out)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if view.state == "working" || view.state == "submitted" {
		if err := a.waitExistingTask(ctx, base, view); err != nil {
			return nil, err
		}
	}
	if view.state != "completed" && view.approval == nil {
		return nil, fmt.Errorf("HITL task ended in state %q", view.state)
	}
	return view, nil
}

func (a *App) showSessions(base string) error {
	resp, err := controllerClient.Get(base + "/api/sessions")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sessions answered HTTP %d", resp.StatusCode)
	}
	var envelope struct {
		Data []struct{ ID, Name, AgentID string } `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxControllerResponse)).Decode(&envelope); err != nil {
		return err
	}
	fmt.Fprintln(a.Out, "Recent sessions")
	for _, item := range envelope.Data {
		fmt.Fprintf(a.Out, "  %s  %s  %s\n", safeTerminal(item.ID), safeTerminal(strings.ReplaceAll(strings.TrimPrefix(item.AgentID, "kagent__NS__"), "_", "-")), safeTerminal(item.Name))
	}
	return nil
}

func (a *App) showSessionHistory(base, id, agent string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("session ID is empty")
	}
	resp, err := controllerClient.Get(base + "/api/sessions/" + url.PathEscape(id))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("session %q answered HTTP %d", id, resp.StatusCode)
	}
	var envelope struct {
		Data struct {
			Session struct {
				AgentID string `json:"agent_id"`
			} `json:"session"`
			Events []struct {
				CreatedAt string `json:"created_at"`
				Data      string `json:"data"`
			} `json:"events"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxControllerResponse)).Decode(&envelope); err != nil {
		return err
	}
	want := "kagent__NS__" + strings.ReplaceAll(agent, "-", "_")
	if envelope.Data.Session.AgentID != want {
		return fmt.Errorf("session belongs to %s, not %s", safeTerminal(envelope.Data.Session.AgentID), safeTerminal(agent))
	}
	sort.SliceStable(envelope.Data.Events, func(i, j int) bool { return envelope.Data.Events[i].CreatedAt < envelope.Data.Events[j].CreatedAt })
	fmt.Fprintf(a.Out, "History for %s (%s)\n", agent, id)
	for _, wrapper := range envelope.Data.Events {
		var event struct {
			Author  string `json:"author"`
			Content struct {
				Role  string `json:"role"`
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		}
		if json.Unmarshal([]byte(wrapper.Data), &event) != nil {
			continue
		}
		for _, part := range event.Content.Parts {
			if part.Text == "" {
				continue
			}
			if event.Content.Role == "user" && event.Author == "user" {
				fmt.Fprintf(a.Out, "You: %s\n", safeTerminal(part.Text))
			} else if event.Content.Role == "model" {
				fmt.Fprintf(a.Out, "%s: %s\n", safeTerminal(agent), safeTerminal(part.Text))
			}
		}
	}
	return nil
}

func (a *App) showActiveTools(agent string) {
	raw, err := a.kubectlCapture("-n", "kagent", "get", "agent", agent, "-o", "json")
	if err != nil {
		fmt.Fprintf(a.Err, "Tools: unavailable: %s\n", safeTerminal(err.Error()))
		return
	}
	var resource struct {
		Spec struct {
			Declarative struct {
				Tools []struct {
					MCPServer struct {
						Name      string
						ToolNames []string
					} `json:"mcpServer"`
				}
			} `json:"declarative"`
		} `json:"spec"`
	}
	if json.Unmarshal([]byte(raw), &resource) != nil || len(resource.Spec.Declarative.Tools) == 0 {
		fmt.Fprintln(a.Out, "Tools: none")
		return
	}
	fmt.Fprintln(a.Out, "Tools:")
	for _, tool := range resource.Spec.Declarative.Tools {
		serverRaw, err := a.kubectlCapture("-n", "kagent", "get", "remotemcpserver", tool.MCPServer.Name, "-o", "json")
		if err != nil {
			fmt.Fprintf(a.Out, "  %s: unavailable (%s)\n", safeTerminal(tool.MCPServer.Name), safeTerminal(err.Error()))
			continue
		}
		var server struct {
			Spec struct {
				URL string `json:"url"`
			} `json:"spec"`
			Status struct {
				Discovered []struct{ Name, Description string } `json:"discoveredTools"`
			} `json:"status"`
		}
		if json.Unmarshal([]byte(serverRaw), &server) != nil {
			fmt.Fprintf(a.Out, "  %s: invalid discovery status\n", safeTerminal(tool.MCPServer.Name))
			continue
		}
		discovered := map[string]string{}
		for _, item := range server.Status.Discovered {
			discovered[item.Name] = item.Description
		}
		posture := "direct, not Kaimahi-governed"
		parsed, _ := url.Parse(server.Spec.URL)
		if parsed != nil && parsed.Scheme == "http" && parsed.Host == "kaimahi-mcp-gateway.kaimahi:8081" && strings.HasPrefix(parsed.Path, "/upstream/") {
			posture = "governed by Kaimahi"
		}
		fmt.Fprintf(a.Out, "  %s (%s)\n", safeTerminal(tool.MCPServer.Name), posture)
		for _, name := range tool.MCPServer.ToolNames {
			if description, ok := discovered[name]; ok {
				fmt.Fprintf(a.Out, "    %s - %s\n", safeTerminal(name), safeTerminal(description))
			} else {
				fmt.Fprintf(a.Out, "    %s - selected but not currently discovered\n", safeTerminal(name))
			}
		}
	}
}
