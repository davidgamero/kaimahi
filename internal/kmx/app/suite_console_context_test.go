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
	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
)

func TestSuiteConsoleBrokenPlacementDoesNotHideContexts(t *testing.T) {
	t.Setenv("KMX_HOME", t.TempDir())
	dir, err := deploymentDirectory()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, strings.Repeat("a", 32)+".json"), []byte("invalid"), 0644); err != nil {
		t.Fatal(err)
	}
	a := New(&config.Config{KubeContext: "kind-local"})
	a.captureCommand = func(_ context.Context, c *exec.Cmd) error {
		raw := `{"items":[]}`
		if strings.Contains(strings.Join(c.Args, " "), "config view") {
			raw = `{"contexts":[{"name":"kind-local","context":{"cluster":"local"}},{"name":"remote","context":{"cluster":"remote"}}],"clusters":[{"name":"local","cluster":{"server":"https://127.0.0.1:6443"}},{"name":"remote","cluster":{"server":"https://remote.example"}}]}`
		}
		if strings.Contains(strings.Join(c.Args, " "), "api-resources") {
			raw = "agents.core.orka.ai\nproviders.core.orka.ai\n"
		}
		_, err := io.WriteString(c.Stdout, raw)
		return err
	}
	r := loadSuiteConsole(t.Context(), a, AgentTUIOptions{})
	if r.err == nil || r.columns == nil || r.columns[0].Env.Name != "kind-local" || r.columns[1].Env.Name != "remote" {
		t.Fatalf("contexts lost on invalid placement: %+v %v", r.columns, r.err)
	}
}

func TestSuiteConsoleEmptyPaneNewAgentAndNoMarketingText(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := &suiteConsole{input: textinput.New(), width: 120, height: 40, selected: -1}
	view := ansi.Strip(m.View().Content)
	for _, old := range []string{"Create → Prove → Publish → Lift", "A suite is the lifecycle object", "Agents stay directly accessible"} {
		if strings.Contains(view, old) {
			t.Fatal("empty pane still has", old)
		}
	}
	if !strings.Contains(view, "No agents or suites") || !strings.Contains(view, "› New agent") {
		t.Fatal("missing empty action")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.creation == nil || m.creation.step().kind != "name" {
		t.Fatal("empty Enter did not launch creation")
	}
}

func TestSuiteConsoleContextsOfferExistingKindAndInitialization(t *testing.T) {
	t.Setenv("KMX_HOME", t.TempDir())
	dir := t.TempDir()
	fakeTool(t, dir, "kind", `printf '%s\n' existing-local`)
	t.Setenv("PATH", dir)
	a := New(&config.Config{KindCluster: "kaimahi"})
	a.captureCommand = func(_ context.Context, c *exec.Cmd) error {
		_, err := io.WriteString(c.Stdout, `{"contexts":[],"clusters":[]}`)
		return err
	}
	var workers sync.WaitGroup
	m := &suiteConsole{a: a, ctx: t.Context(), workers: &workers, events: make(chan suiteConsoleProgress, 256), input: textinput.New()}
	consoleFinish(t, m, m.openContexts(0))
	if m.contexts == nil || len(m.contexts.choices) != 2 || m.contexts.choices[0].action != "kind" || m.contexts.choices[1].action != "init" {
		t.Fatalf("choices %+v", m.contexts)
	}
	m.Update(tea.KeyPressMsg{Code: 'e', Text: "exloc"})
	if len(m.contexts.matches()) != 1 {
		t.Fatal("no fuzzy kind match")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.form == nil || m.form.action != "kind-connect" || m.running {
		t.Fatal("kind access not confirmed before action")
	}
	workers.Wait()
}

func TestSuiteConsoleAKSDiscoveryDoesNotRequireKubeconfigOrWrite(t *testing.T) {
	t.Setenv("KMX_HOME", t.TempDir())
	dir := t.TempDir()
	fakeTool(t, dir, "az", `if [ "$1 $2" != "aks list" ]; then exit 99; fi; printf '%s' '[{"name":"aks-new","resourceGroup":"test-west","id":"/subscriptions/test/resourceGroups/test-west/providers/Microsoft.ContainerService/managedClusters/aks-new","fqdn":"aks.example"}]'`)
	t.Setenv("PATH", dir)
	a := New(&config.Config{})
	var workers sync.WaitGroup
	m := &suiteConsole{a: a, ctx: t.Context(), workers: &workers, events: make(chan suiteConsoleProgress, 256), input: textinput.New(), contexts: &suiteContextPicker{side: 1}}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	consoleFinish(t, m, cmd)
	if m.contexts == nil || !m.contexts.aks || len(m.contexts.choices) != 1 {
		t.Fatal("AKS missing without kubeconfig")
	}
	m.Update(tea.KeyPressMsg{Code: 'w', Text: "tstw"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.form == nil || m.form.action != "aks-connect" || m.pendingAKS.Name != "aks-new" {
		t.Fatal("filtered selection lost AKS identity")
	}
	if cmd := m.submit(suiteConsoleForm{action: "aks-connect", values: []string{"wrong"}}); cmd != nil {
		t.Fatal("credential write without exact confirmation")
	}
	workers.Wait()
}
