package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestQuickstartWizardViewKeepsInfrastructureAboveNumberedAgentStep(t *testing.T) {
	create, err := newCreateWizardModel(CreateOptions{Model: "qwen2.5:3b"})
	if err != nil {
		t.Fatal(err)
	}
	m := quickstartWizardModel{create: create, setup: [4]string{"done", "active", "pending", "pending"}, frame: 3}
	view := m.View().Content
	for _, want := range []string{"Infrastructure", "Kind cluster", "Ollama image", "Model qwen2.5:3b", "Orka runtime", "Agent setup  [2/8]", "Description"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if strings.Index(view, "Infrastructure") > strings.Index(view, "Agent setup") {
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
	got := quickstartModelLabel(localModel{Provider: "ollama", Model: "qwen3:8b", Size: 8 << 30})
	for _, want := range []string{"qwen3:8b", "ollama on this host", "8.0 GB"} {
		if !strings.Contains(got, want) {
			t.Fatalf("host label %q missing %q", got, want)
		}
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
	view := m.View().Content
	for _, want := range []string{"Agent setup  [1/8]", "Create a new agent", `Use existing Agent "existing" (orka-system)`} {
		if !strings.Contains(view, want) {
			t.Fatalf("start choice missing %q:\n%s", want, view)
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
