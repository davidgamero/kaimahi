package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/run"
)

func TestConsoleInferenceOverlaySourcesFormsAndDemo(t *testing.T) {
	for _, kind := range []string{"foundry", "ollama", "copilot", "apikey"} {
		m := newAgentTUIModel(AgentTUIOptions{Demo: true})
		m = tuiKey(m, tea.KeyEnter, "")
		m = tuiKey(m, 'f', "f")
		if m.inference == nil || m.action != nil {
			t.Fatal("inference left console")
		}
		view := ansi.Strip(m.View().Content)
		if strings.Index(view, "Add inference source") < strings.Index(view, "demo-provider") {
			t.Fatal("add source is not last")
		}
		m = tuiKey(m, tea.KeyDown, "")
		m = tuiKey(m, tea.KeyEnter, "")
		if m.inference.stage != "kinds" {
			t.Fatal("add source did not open connector choices")
		}
		for i, k := range m.inference.sourceKinds() {
			if k == kind {
				m.inference.selection = i
			}
		}
		m = tuiKey(m, tea.KeyEnter, "")
		values := map[string][]string{
			"foundry": {"https://example.openai.azure.com", "chat-model", ""},
			"ollama":  {"test-ollama", "http://ollama.ollama.svc.cluster.local:11434", "qwen2.5:3b"},
			"copilot": {"gpt-4.1"},
			"apikey":  {"test-api", "openai", "https://api.example.com/v1", "test-model", "provider-key", "api-key"},
		}[kind]
		for _, value := range values {
			m.inference.fields[m.inference.field].input.SetValue(value)
			m = tuiKey(m, tea.KeyEnter, "")
		}
		if m.inference.stage != "review" {
			t.Fatalf("%s form: %v", kind, m.inference.err)
		}
		for _, size := range [][2]int{{64, 18}, {80, 24}, {120, 40}} {
			m.width, m.height = size[0], size[1]
			view := m.View().Content
			if lipgloss.Width(view) > m.width || lipgloss.Height(view) > m.height {
				t.Fatalf("%s overlay overflow at %v", kind, size)
			}
		}
		m = tuiKey(m, tea.KeyDown, "")
		m = tuiKey(m, tea.KeyEnter, "")
		if m.inference.stage != "done" || m.action != nil {
			t.Fatal("demo saved a source")
		}
		m = tuiKey(m, tea.KeyEnter, "")
		if m.inference != nil {
			t.Fatal("result did not close")
		}
	}
}

func TestConsoleInferenceOverlaySaveCancelAndStaleLoad(t *testing.T) {
	m := newAgentTUIModel(AgentTUIOptions{Demo: true})
	m = tuiKey(m, tea.KeyEnter, "")
	m = tuiKey(m, 'f', "f")
	p := m.inference
	updated, _ := m.Update(consoleInferenceLoaded{pane: &consoleInferencePane{}, err: context.Canceled})
	m = updated.(agentTUIModel)
	if p.err != nil {
		t.Fatal("stale lookup replaced current pane")
	}
	p.source = consoleInferenceSource{Kind: "cluster", Name: "selected"}
	p.stage = "review"
	p.selection = 1
	m.opt.Demo = false
	cancelled := false
	m.startInference = func(env agentTUIEnvironment, agent agentTUIAgent, snapshot consoleInferenceSnapshot, source consoleInferenceSource, model string) (<-chan consoleInferenceSaved, context.CancelFunc) {
		if source.Name != "selected" || env.Name != "kind-local-demo" || agent.Name != "assistant" {
			t.Fatal("save routed incorrectly")
		}
		return make(chan consoleInferenceSaved, 1), func() { cancelled = true }
	}
	m.loadInventory = func(agentTUIEnvironment) ([]agentTUIAgent, error) { return nil, nil }
	m = tuiKey(m, tea.KeyEnter, "")
	m = tuiKey(m, tea.KeyEsc, "")
	if !cancelled || p.stage != "saving" {
		t.Fatal("cancellation closed before worker joined")
	}
	updated, _ = m.Update(consoleInferenceSaved{err: context.Canceled})
	m = updated.(agentTUIModel)
	if p.stage != "done" {
		t.Fatal("save result lost")
	}
}

func TestConsoleInferenceSourcePersistenceAndValidation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	env := agentTUIEnvironment{Name: "kind-test"}
	agent := agentTUIAgent{Name: "demo", Namespace: "agents", Runtime: "orka"}
	source := consoleInferenceSource{Kind: "foundry", Name: "test", Endpoint: "https://example.openai.azure.com", Model: "test-model"}
	if err := saveConsoleInference(env, agent, &source); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadConsoleInference(env, agent)
	if err != nil || loaded.Model != source.Model {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	path, _ := consoleInferencePath(env, agent)
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("metadata permissions incorrect")
	}
	other, err := loadConsoleInference(agentTUIEnvironment{Name: "remote"}, agent)
	if err != nil || other != nil {
		t.Fatal("source leaked across environments")
	}
	if err := saveConsoleInference(env, agent, nil); err != nil {
		t.Fatal(err)
	}
	if loaded, err := loadConsoleInference(env, agent); err != nil || loaded != nil {
		t.Fatal("cluster selection did not clear override")
	}
	for _, s := range []consoleInferenceSource{
		{Kind: "foundry", Endpoint: "https://untrusted.example.com", Model: "x"},
		{Kind: "apikey", Name: "demo", Provider: "openai", Endpoint: "https://user:pass@example.com", Model: "x", Secret: "key", SecretKey: "api-key"},
		{Kind: "ollama", Name: "demo", Endpoint: "http://example.com?token=x", Model: "x"},
	} {
		if s.validate("orka") == nil {
			t.Fatalf("invalid source accepted: %s", s.Kind)
		}
	}
	if source.validate("kagent") == nil {
		t.Fatal("host inference exposed for unsupported runtime")
	}
}

func TestConsoleInferenceCreatesConnectorWithoutRewritingAgentTools(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "body")
	fakeTool(t, dir, "kubectl", `case "$*" in
 *'create --validate=strict -f -'*) cat > "$BODY_LOG"; printf '%s' '{"kind":"ModelConfig"}' ;;
 *) exit 1 ;;
esac`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	a := &App{Cfg: &config.Config{KubeContext: "remote"}, Run: &run.Runner{Env: []string{"BODY_LOG=" + log}}}
	s := consoleInferenceSource{Kind: "ollama", Name: "local-model", Endpoint: "http://ollama:11434", Model: "qwen2.5:3b"}
	if err := a.consoleCreateConnector(t.Context(), agentTUIAgent{Runtime: "kagent", Namespace: "kagent"}, s); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	spec := doc["spec"].(map[string]any)
	if doc["kind"] != "ModelConfig" || spec["provider"] != "Ollama" || spec["ollama"].(map[string]any)["host"] != s.Endpoint {
		t.Fatalf("connector=%s", raw)
	}
}
