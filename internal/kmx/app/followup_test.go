package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/run"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/scaffold"
)

func fakeTool(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestInteractiveStreamShowsToolsAndFinalReply(t *testing.T) {
	var out bytes.Buffer
	view := &streamView{agent: "hello-tools", toolCalls: map[string]string{}, messageText: map[string]string{}, toolMode: "summary"}
	input := strings.NewReader(strings.Join([]string{
		`{"kind":"status-update","contextId":"session-1","taskId":"task-1","status":{"state":"working","message":{"role":"agent","parts":[{"kind":"data","metadata":{"kagent_type":"function_call"},"data":{"id":"call-1","name":"get_pods","args":{"namespace":"default"}}}]}}}`,
		`{"kind":"status-update","contextId":"session-1","taskId":"task-1","status":{"state":"working","message":{"role":"agent","parts":[{"kind":"data","metadata":{"kagent_type":"function_response"},"data":{"id":"call-1","name":"get_pods","response":{"isError":false}}}]}}}`,
		`{"kind":"artifact-update","contextId":"session-1","taskId":"task-1","artifact":{"parts":[{"kind":"text","text":"pod-a"}]}}`,
		`{"kind":"status-update","contextId":"session-1","taskId":"task-1","final":true,"status":{"state":"completed"}}`,
	}, "\n"))
	a := &App{Out: &out}
	if err := a.consumeStream(input, view); err != nil {
		t.Fatal(err)
	}
	if view.state != "completed" || view.context != "session-1" || view.reply != "pod-a" {
		t.Fatalf("unexpected view: %+v", view)
	}
	for _, want := range []string{"Tool: get_pods", "completed", "hello-tools: pod-a"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestToolDisplayModesStillDetectGovernanceDenials(t *testing.T) {
	raw := json.RawMessage(`{"id":"call-1","name":"post","response":{"isError":true,"content":[{"text":"not permitted; approval request filed"}]}}`)
	for _, mode := range []string{"off", "summary", "verbose"} {
		var out bytes.Buffer
		view := &streamView{toolCalls: map[string]string{"call-1": "post"}, messageText: map[string]string{}, toolMode: mode}
		view.consumeTool("function_response", false, raw, &out)
		if !view.denied {
			t.Errorf("mode %s hid governance denial", mode)
		}
		if mode == "off" && out.Len() != 0 {
			t.Errorf("off mode printed tool output: %s", out.String())
		}
		if mode == "verbose" && !strings.Contains(out.String(), "result:") {
			t.Errorf("verbose mode omitted result: %s", out.String())
		}
	}
}

func TestStreamLabelsDistinctAgentMessages(t *testing.T) {
	var out bytes.Buffer
	view := &streamView{agent: "agent", toolCalls: map[string]string{}, messageText: map[string]string{}, toolMode: "summary"}
	for _, id := range []string{"one", "two"} {
		status := fmt.Sprintf(`{"state":"working","message":{"role":"agent","messageId":%q,"parts":[{"kind":"text","text":%q}]}}`, id, id)
		view.consume(streamEvent{Status: json.RawMessage(status)}, &out)
	}
	if strings.Count(out.String(), "agent: ") != 2 {
		t.Fatalf("messages were not separately attributed: %s", out.String())
	}
}

func TestInteractiveStreamSurfacesNativeHITL(t *testing.T) {
	var out bytes.Buffer
	view := &streamView{agent: "agent", toolCalls: map[string]string{}, messageText: map[string]string{}, toolMode: "summary"}
	raw := `{"kind":"status-update","contextId":"session-1","taskId":"task-1","status":{"state":"input-required","message":{"role":"agent","parts":[{"kind":"data","metadata":{"kagent_type":"function_call","kagent_is_long_running":true},"data":{"name":"adk_request_confirmation","args":{"originalFunctionCall":{"id":"call-1","name":"delete_pod","args":{"name":"pod-a"}}}}}]}}}`
	a := &App{Out: &out}
	if err := a.consumeStream(strings.NewReader(raw), view); err != nil {
		t.Fatal(err)
	}
	if view.approval == nil || len(view.approval.Calls) != 1 || view.approval.Calls[0].Name != "delete_pod" {
		t.Fatalf("approval not parsed: %+v", view.approval)
	}
}

func TestInteractiveStreamAcceptsADKMetadataAliases(t *testing.T) {
	var out bytes.Buffer
	view := &streamView{agent: "agent", toolCalls: map[string]string{}, messageText: map[string]string{}, toolMode: "summary"}
	raw := `{"kind":"status-update","contextId":"session-1","taskId":"task-1","status":{"state":"input-required","message":{"role":"agent","parts":[{"kind":"data","metadata":{"adk_type":"function_call","adk_is_long_running":true},"data":{"name":"adk_request_confirmation","args":{"originalFunctionCall":{"id":"call-1","name":"delete_pod","args":{}}}}}]}}}`
	a := &App{Out: &out}
	if err := a.consumeStream(strings.NewReader(raw), view); err != nil {
		t.Fatal(err)
	}
	if view.approval == nil || view.approval.Calls[0].Name != "delete_pod" {
		t.Fatalf("ADK metadata alias was not parsed: %+v", view.approval)
	}
}

func TestTerminalOutputStripsControlSequences(t *testing.T) {
	got := safeTerminal("safe\x1b]52;c;secret\a\x1b[2Jtext\x00")
	if got != "safetext" {
		t.Fatalf("unsafe terminal text: %q", got)
	}
}

func TestGovernedModelRequiresExactProxyEndpoint(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"http://kaimahi-proxy.kaimahi:8080/upstream/ollama/v1", true},
		{"http://kaimahi-proxy.kaimahi.svc.cluster.local:8080/upstream/ollama/v1", true},
		{"https://kaimahi-proxy.kaimahi:8080/upstream/ollama/v1", false},
		{"http://example.invalid/kaimahi-proxy.kaimahi:8080/upstream/x", false},
		{"http://[::1", false},
	} {
		if got := usesKaimahiModelProxy(map[string]any{"openAI": map[string]any{"baseUrl": tc.url}}); got != tc.want {
			t.Errorf("usesKaimahiModelProxy(%q)=%v, want %v", tc.url, got, tc.want)
		}
	}
}

func TestUpPreflightReportsAllMissingDependencies(t *testing.T) {
	dir := t.TempDir()
	fakeTool(t, dir, "docker", "exit 0")
	t.Setenv("PATH", dir)
	a := &App{Cfg: &config.Config{ContainerEngine: "docker"}, Run: &run.Runner{}}
	err := a.preflightUp([]string{"cluster", "kagent"})
	if err == nil {
		t.Fatal("preflight unexpectedly passed")
	}
	for _, want := range []string{"3 missing or unusable dependencies", "kind is not on PATH", "kubectl is not on PATH", "helm is not on PATH"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("preflight error lacks %q:\n%s", want, err)
		}
	}
}

func TestValidateToolServer(t *testing.T) {
	wiring := &scaffold.ToolWiring{Server: "tools", Tools: []string{"get", "events"}}
	accepted := []serverCondition{{Type: "Accepted", Status: "True", ObservedGeneration: 2}}
	if err := validateToolServer(wiring, 2, 2, accepted, map[string]bool{"get": true, "events": true}); err != nil {
		t.Fatal(err)
	}
	if err := validateToolServer(wiring, 2, 2, accepted, map[string]bool{"get": true}); err == nil || !strings.Contains(err.Error(), "events") {
		t.Fatalf("missing tool was not reported: %v", err)
	}
	if err := validateToolServer(wiring, 2, 2, []serverCondition{{Type: "Accepted", Status: "False", Message: "dial failed", ObservedGeneration: 2}}, nil); err == nil || !strings.Contains(err.Error(), "dial failed") {
		t.Fatalf("unaccepted server was not reported: %v", err)
	}
	if err := validateToolServer(wiring, 2, 1, accepted, nil); err == nil || !strings.Contains(err.Error(), "still reconciling") {
		t.Fatalf("stale discovery was not refused: %v", err)
	}
}
