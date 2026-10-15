package app

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestSuiteCreationPickerBackNavigationAndImageAuthoring(t *testing.T) {
	t.Setenv("KMX_HOME", t.TempDir())
	a, _, _, _ := extractionFixture(t)
	var workers sync.WaitGroup
	m := &suiteConsole{a: a, ctx: t.Context(), workers: &workers, input: textinput.New(), events: make(chan suiteConsoleProgress, 256), width: 100, height: 32}
	m.startCreation()
	m.input.SetValue("saved-name")
	m.input.CursorEnd()
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.creation.step().kind != "name" || m.input.Value() != "saved-nam" {
		t.Fatal("Backspace failed normal text deletion")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.creation.step().kind != "source" || m.creation.profile != "native" || m.creation.root != "saved-nam" {
		t.Fatal("name did not advance directly to execution with default location")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt})
	m.input.SetValue("renamed-agent")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.creation.root != "renamed-agent" {
		t.Fatal("renaming did not update suggested location")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.input.SetValue("model-a")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.creation.step().kind != "instructions" {
		t.Fatal("image requested exact native model")
	}
	m.input.SetValue("Answer briefly.")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.input.Value() != "Answer briefly." {
		t.Fatal("back lost instructions")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	// Alternative execution is available only from final review.
	m.creation.step().selection = 2
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.creation.step().kind != "profile" {
		t.Fatal("Change execution not available")
	}
	m.creation.step().selection = 1
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.creation.model != "" || m.creation.instructions != "Answer briefly." {
		t.Fatal("profile change retained incompatible model or lost instructions")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	// Location is optional and only offered on the final confirmation screen.
	if m.creation.step().kind != "confirm" || !strings.Contains(ansi.Strip(m.View().Content), "Save to: renamed-agent") {
		t.Fatal("missing final location preview")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.creation.step().kind != "root" {
		t.Fatal("Change location did not open location editor")
	}
	root := filepath.Join(t.TempDir(), "image")
	m.input.SetValue(root)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.creation.step().kind != "confirm" || m.creation.root != root || m.creation.step().selection != 0 {
		t.Fatal("location editor did not return to Create agent")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	consoleFinish(t, m, cmd)
	if m.entry().Review.image == nil || m.entry().Root != root {
		t.Fatal("wrong execution profile")
	}
	workers.Wait()
}

func TestSuiteCreateProviderDiscoveryFiltersAndNeverReadsSecrets(t *testing.T) {
	a, _, _, _ := extractionFixture(t)
	a.captureCommand = func(_ context.Context, c *exec.Cmd) error {
		args := strings.Join(c.Args, " ")
		if !strings.Contains(args, "get providers.core.orka.ai") || strings.Contains(args, "secret") {
			t.Fatalf("unexpected discovery: %s", args)
		}
		_, err := io.WriteString(c.Stdout, `{"items":[{"metadata":{"name":"ready"},"spec":{"type":"openai","defaultModel":"model-a","baseURL":"https://model.example/v1"},"status":{"conditions":[{"type":"Ready","status":"True"}]}},{"metadata":{"name":"unready"},"spec":{"type":"openai","defaultModel":"model-b"}},{"metadata":{"name":"unsupported"},"spec":{"type":"anthropic","defaultModel":"model-c"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}`)
		return err
	}
	choices, err := a.suiteCreateProviders(t.Context(), agentTUIEnvironment{Name: "kind-test"}, "orka-system")
	if err != nil || len(choices) != 1 || choices[0].value != "model-a" {
		t.Fatalf("choices=%+v err=%v", choices, err)
	}
}

func TestSuiteCreationDefaultLocationNeedsNoWorkspaceStep(t *testing.T) {
	t.Setenv("KMX_HOME", t.TempDir())
	t.Chdir(t.TempDir())
	a, _, _, _ := extractionFixture(t)
	var workers sync.WaitGroup
	m := &suiteConsole{a: a, ctx: t.Context(), workers: &workers, input: textinput.New(), events: make(chan suiteConsoleProgress, 256), width: 100, height: 32}
	m.startCreation()
	m.input.SetValue("new-helper")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.creation.step().kind != "source" {
		t.Fatal("name did not advance to execution")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.input.SetValue("model-a")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.input.SetValue("Answer briefly.")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	for _, step := range m.creation.steps {
		if step.kind == "root" || step.kind == "profile" {
			t.Fatal("normal flow requested a workspace")
		}
	}
	if _, err := os.Stat("new-helper"); !os.IsNotExist(err) {
		t.Fatal("source written before Create agent")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	consoleFinish(t, m, cmd)
	if _, err := os.Stat("new-helper/workspace.yaml"); err != nil {
		t.Fatal(err)
	}
	if m.entry().Name != "new-helper" {
		t.Fatal("agent name lost")
	}
	workers.Wait()
}

func TestSuiteCreateNativeSelectionAndNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := &suiteConsole{input: textinput.New(), width: 100, height: 32}
	m.startCreation()
	m.creation.step().value = "member"
	m.advanceCreation()
	if m.creation.step().kind != "source" {
		t.Fatal("missing inference choices")
	}
	m.creation.step().selection = 1
	m.advanceCreation() // explicit advanced manual fallback
	m.creation.step().value = "image-http"
	m.advanceCreation()
	if m.creation.profile != "native" || m.creation.model != "image-http" {
		t.Fatal("model string silently changed execution")
	}
	view := m.View().Content
	if view != ansi.Strip(view) || !strings.Contains(view, "Backspace") {
		t.Fatal("non-color navigation unreadable")
	}
}

func TestSuiteProviderChoiceWrapUsesHangingIndent(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	label := "Existing Providers · aks-long-resource-group-and-cluster-name"
	for _, selected := range []bool{false, true} {
		lines := strings.Split(suiteChoiceWrapped(label, 24, selected, newSuiteConsoleTheme()), "\n")
		if len(lines) < 2 {
			t.Fatal("fixture did not wrap")
		}
		for i, line := range lines {
			if ansi.StringWidth(line) > 24 {
				t.Fatal("overflow", line)
			}
			if i > 0 && !strings.HasPrefix(line, "  ") {
				t.Fatal("continuation not indented", line)
			}
		}
	}
}

func TestSuiteSubscriptionSearchNavigationAndRememberedIdentity(t *testing.T) {
	t.Setenv("KMX_HOME", t.TempDir())
	choices := []consoleAzureChoice{{Label: "Alpha Dev", ID: "11111111-1111-1111-1111-111111111111"}, {Label: "Production West", ID: "22222222-2222-2222-2222-222222222222"}, {Label: "Production East", ID: "33333333-3333-3333-3333-333333333333"}}
	if err := saveLiftPreference(suiteSubscriptionPreference, choices[2].ID); err != nil {
		t.Fatal(err)
	}
	prefs, err := loadLiftPreferences()
	if err != nil {
		t.Fatal(err)
	}
	ordered := recentSuiteSubscriptions(choices, prefs[suiteSubscriptionPreference])
	if ordered[0].ID != choices[2].ID || choices[0].Label != "Alpha Dev" {
		t.Fatal("last identity not preferred or source mutated")
	}
	m := &suiteConsole{input: textinput.New(), width: 120, height: 40, creation: &suiteCreateWizard{}}
	step := suiteCreateStep{kind: "azure-subscriptions", title: "Choose Azure subscription"}
	for _, c := range ordered {
		step.choices = append(step.choices, suiteCreateChoice{label: c.Label + " · " + c.ID, azure: c})
	}
	m.createStep(step)
	if !strings.Contains(ansi.Strip(m.View().Content), choices[2].ID) {
		t.Fatal("GUID missing from row")
	}
	m.Update(tea.KeyPressMsg{Code: 'p', Text: "prd"})
	s := m.creation.step()
	matches := s.matches()
	if len(matches) != 2 || s.query != "prd" {
		t.Fatalf("fuzzy matches=%v query=%q", matches, s.query)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if s.choices[s.matches()[s.selection]].azure.ID != choices[1].ID {
		t.Fatal("arrow did not navigate filtered results")
	}
	m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if len(s.matches()) != 0 {
		t.Fatal("j was navigation rather than search input")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // no selection: must not start a job
	if m.running {
		t.Fatal("empty match dispatched discovery")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if s.query != "prd" || s.selection != 0 {
		t.Fatal("search deletion lost query or selection reset")
	}
	missing := recentSuiteSubscriptions(choices, "absent")
	if missing[0].ID != choices[0].ID {
		t.Fatal("unavailable remembered subscription was invented")
	}
}

func TestSuiteResourcePickersSearchNamesAndDetails(t *testing.T) {
	for _, kind := range []string{"azure-accounts", "azure-deployments", "providers", "source"} {
		t.Run(kind, func(t *testing.T) {
			m := &suiteConsole{input: textinput.New(), width: 100, height: 32, creation: &suiteCreateWizard{}}
			m.createStep(suiteCreateStep{kind: kind, choices: []suiteCreateChoice{{label: "unrelated", detail: "east"}, {label: "resource-one", detail: "Japan west"}, {label: "resource-two", detail: "Japan west"}}})
			m.Update(tea.KeyPressMsg{Code: 'j', Text: "jpnw"})
			s := m.creation.step()
			if len(s.matches()) != 2 || s.query != "jpnw" {
				t.Fatalf("matches=%v query=%s", s.matches(), s.query)
			}
			m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			if s.matches()[s.selection] != 2 {
				t.Fatal("arrow selected wrong underlying resource")
			}
			m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
			if s.query != "jpn" || m.creation.step() != s {
				t.Fatal("Backspace navigated instead of editing")
			}
		})
	}
}

func TestSuiteAzureFormEnterSelectsFilteredResource(t *testing.T) {
	m := &suiteConsole{input: textinput.New(), width: 120, height: 40, azure: []suiteAzureSelection{
		{Context: "context-a", Cluster: suiteAzureCluster{Name: "production-japan", ResourceGroup: "team-west"}},
		{Context: "context-b", Cluster: suiteAzureCluster{Name: "production-japan-two", ResourceGroup: "team-west"}},
	}}
	m.startForm("azure-select", []string{"Cluster", "Registry"}, []string{"", ""})
	m.input.SetValue("prdjpn")
	if len(m.formCompletions()) != 2 {
		t.Fatal("AKS name was not searchable through context row")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.form.field != 1 || m.form.values[0] != "context-b" {
		t.Fatal("Enter did not select highlighted filtered context")
	}
	m.input.SetValue("nothing")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.form == nil || m.running {
		t.Fatal("empty registry match submitted form")
	}
}
