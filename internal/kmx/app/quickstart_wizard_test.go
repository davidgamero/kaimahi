package app

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestQuickstartWizardViewKeepsInfrastructureAboveNumberedAgentStep(t *testing.T) {
	create, err := newCreateWizardModel(CreateOptions{Model: "qwen2.5:3b"})
	if err != nil {
		t.Fatal(err)
	}
	m := quickstartWizardModel{create: create, setup: [4]string{"done", "active", "pending", "pending"}, frame: 3}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"INFRASTRUCTURE", "Kind cluster", "Ollama image", "Model qwen2.5:3b", "Orka runtime", "AGENT SETUP", "STEP 2 OF 8", "Description"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if strings.Index(view, "INFRASTRUCTURE") > strings.Index(view, "AGENT SETUP") {
		t.Fatalf("infrastructure progress is not at the top:\n%s", view)
	}
}

func TestQuickstartWizardWaitsForSetupAfterReview(t *testing.T) {
	create, err := newCreateWizardModel(CreateOptions{
		Name: "demo", Description: "Demo", Namespace: OrkaNamespace,
		ProviderType: "openai", Model: "qwen2.5:3b", Secret: "kickstart-provider-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan quickstartSetupEvent)
	m := quickstartWizardModel{create: create, events: events}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := updated.(quickstartWizardModel)
	if cmd != nil || !got.formDone || got.setupDone {
		t.Fatalf("review did not wait for setup: formDone=%v setupDone=%v cmd=%v", got.formDone, got.setupDone, cmd)
	}
	if !strings.Contains(got.View().Content, `Agent "demo" is queued. Waiting for the model and runtime`) {
		t.Fatalf("waiting state is not visible:\n%s", got.View().Content)
	}
	_, cmd = got.Update(quickstartSetupEvent{step: -1, status: "complete"})
	if cmd == nil {
		t.Fatal("setup completion did not release the wizard")
	}
}

func TestQuickstartProgressBarsDistinguishEveryState(t *testing.T) {
	states := []string{"pending", "active", "done", "failed"}
	seen := map[string]bool{}
	for _, state := range states {
		bar := quickstartProgressBar(state, 4)
		if len(bar) != 20 || seen[bar] {
			t.Fatalf("state %q has invalid or duplicate bar %q", state, bar)
		}
		seen[bar] = true
	}
}

func TestQuickstartWizardRefusesNoninteractiveInputBeforeSetup(t *testing.T) {
	a := &App{}
	err := a.QuickstartWizard(QuickstartWizardOptions{})
	if err == nil || !strings.Contains(err.Error(), "requires an interactive terminal") {
		t.Fatalf("error=%v", err)
	}
}

func TestQuickstartModelChoicesShowSourceAndReportedSize(t *testing.T) {
	if got := quickstartModelLabel(localModel{Provider: "bundled", Model: "qwen2.5:3b"}); !strings.Contains(got, "bundled") || !strings.Contains(got, "download") {
		t.Fatalf("bundled label=%q", got)
	}
	got := quickstartModelLabel(localModel{Provider: "ollama", Model: "qwen3:8b", Size: 8_000_000_000})
	for _, want := range []string{"qwen3:8b", "ollama on this host", "8.0 GB"} {
		if !strings.Contains(got, want) {
			t.Fatalf("host label %q missing %q", got, want)
		}
	}
}

func TestBundledModelChoiceSaysWhenDownloadAlreadyFinished(t *testing.T) {
	model := localModel{Provider: "bundled", Model: "qwen2.5:3b"}
	m := quickstartWizardModel{}
	if got := m.quickstartModelChoiceLabel(model); !strings.Contains(got, "download during setup") {
		t.Fatalf("pending label=%q", got)
	}
	m.setup[2] = "done"
	if got := m.quickstartModelChoiceLabel(model); got != "qwen2.5:3b (bundled, already downloaded)" {
		t.Fatalf("completed label=%q", got)
	}
}

func TestQuickstartInfrastructureRowsExplainEstimatedSetupSize(t *testing.T) {
	m := quickstartWizardModel{models: []localModel{{Provider: "bundled", Model: "qwen2.5:3b"}}}
	want := []string{"~1.3 GB node image", "~1.1 GB image", "~1.9 GB model", "~860 MB images"}
	for step, detail := range want {
		if got := m.infrastructureSize(step); got != detail {
			t.Fatalf("step %d detail=%q, want %q", step, got, detail)
		}
	}
	m.chosen = &localModel{Provider: "ollama", Model: "qwen3:8b", Size: 8_000_000_000}
	if got := m.infrastructureSize(1); got != "skipped; host runtime" {
		t.Fatalf("host runtime detail=%q", got)
	}
	if got := m.infrastructureSize(2); got != "8.0 GB already installed" {
		t.Fatalf("host model detail=%q", got)
	}
}

func TestQuickstartStartsWithExistingAgentOrNewChoice(t *testing.T) {
	create, err := newCreateWizardModel(CreateOptions{descriptionDefault: "Hello world agent"})
	if err != nil {
		t.Fatal(err)
	}
	m := quickstartWizardModel{create: create, agentStep: true, modelStep: true,
		existing: []quickstartExistingAgent{{Name: "existing", Namespace: OrkaNamespace}},
		models:   []localModel{{Provider: "bundled", Model: "qwen2.5:3b"}}}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"AGENT SETUP", "STEP 1 OF 8", "Create a new agent", `Use existing Agent "existing" (orka-system)`} {
		if !strings.Contains(view, want) {
			t.Fatalf("start choice missing %q:\n%s", want, view)
		}
	}
}

func TestQuickstartWizardPanelsFitNarrowTerminal(t *testing.T) {
	create, err := newCreateWizardModel(CreateOptions{Model: "qwen2.5:3b"})
	if err != nil {
		t.Fatal(err)
	}
	m := quickstartWizardModel{create: create, width: 52, setup: [4]string{"done", "active"}}
	for _, line := range strings.Split(m.View().Content, "\n") {
		if got := lipgloss.Width(line); got > 52 {
			t.Fatalf("narrow view line is %d cells wide: %q", got, ansi.Strip(line))
		}
	}
}

func TestQuickstartDescriptionStartsWithHelloWorldAgent(t *testing.T) {
	m, err := newCreateWizardModel(CreateOptions{descriptionDefault: "Hello world agent"})
	if err != nil {
		t.Fatal(err)
	}
	if m.step != createDescription || m.input.Value() != "Hello world agent" {
		t.Fatalf("step=%d value=%q", m.step, m.input.Value())
	}
}

func TestQuickstartReadyScreenDefaultsToChat(t *testing.T) {
	m := quickstartReadyModel{name: "hello-world-agent"}
	view := m.View().Content
	if !strings.Contains(view, "> Chat with agent") || !strings.Contains(view, `Agent "hello-world-agent" is ready`) {
		t.Fatalf("ready screen does not continue into chat by default:\n%s", view)
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || updated.(quickstartReadyModel).selection != 0 {
		t.Fatal("default ready action did not select chat")
	}
}

func TestQuickstartChatIntroducesTaskSemanticsWithoutAuthorizationWarningAsReply(t *testing.T) {
	var out bytes.Buffer
	fmt.Fprintln(&out, "Chatting with demo. Type /exit to finish.")
	fmt.Fprintln(&out, "Each message runs as a fresh local Orka Task.")
	text := out.String()
	if !strings.Contains(text, "fresh local Orka Task") || strings.Contains(text, "full effective authority") {
		t.Fatalf("chat introduction=%q", text)
	}
}
