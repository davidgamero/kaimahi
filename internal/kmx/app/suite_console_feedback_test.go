package app

import (
	"context"
	"io"
	"net"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestConsoleInspectionSuiteOwnedWithoutRetainedPlacement(t *testing.T) {
	a, _, _, _ := extractionFixture(t)
	reads := 0
	a.captureCommand = func(_ context.Context, c *exec.Cmd) error {
		if !strings.Contains(strings.Join(c.Args, " "), "get agents.core.orka.ai live") {
			t.Fatalf("unexpected read/write %v", c.Args)
		}
		reads++
		_, err := io.WriteString(c.Stdout, `{"metadata":{"name":"live","namespace":"agents","uid":"uid","resourceVersion":"1","annotations":{"kaimahi.dev/suite":"suite"}},"spec":{"providerRef":{"name":"inference"},"systemPrompt":{"inline":"Helpful instructions"}},"status":{"ready":true}}`)
		return err
	}
	e := suiteConsoleEntry{Name: "suite", Context: "remote", Environment: agentTUIEnvironment{Name: "remote"}, Live: &agentTUIAgent{Name: "live", Namespace: "agents", UID: "uid", SuiteOwned: true}}
	text, err := a.inspectConsoleAgent(t.Context(), e)
	if err != nil || reads != 2 || !strings.Contains(text, "No retained placement:") || !strings.Contains(text, "Helpful instructions") {
		t.Fatalf("inspection %s %v", text, err)
	}
	var workers sync.WaitGroup
	m := &suiteConsole{a: a, ctx: t.Context(), workers: &workers, events: make(chan suiteConsoleProgress, 256), input: textinput.New(), entries: []suiteConsoleEntry{e}, width: 100, height: 32}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'i'})
	consoleFinish(t, m, cmd)
	if m.feedback == nil || !strings.Contains(ansi.Strip(m.View().Content), "AGENT INSPECTION") {
		t.Fatal("Inspect wrote to invisible detail instead of modal")
	}
	workers.Wait()
}

func TestConsoleFeedbackLoadingAndRuntimeConfirmation(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := &suiteConsole{width: 100, height: 32, running: true, loadingLabel: "Loading Lift destinations", events: make(chan suiteConsoleProgress, 8)}
	before := m.View().Content
	m.Update(suiteConsoleTick{})
	if before == m.View().Content || !strings.Contains(before, "Loading Lift destinations") {
		t.Fatal("operation is not animated/labeled")
	}
	m.running = false
	m.input = textinput.New()
	m.liftGuide = &suiteLiftGuide{}
	m.guideInput("runtime-install", "Install pinned Orka? Type context: an-extremely-long-target-context", "", "an-extremely-long-target-context")
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Type to confirm:") || !strings.Contains(view, "an-extremely-long-target-context") {
		t.Fatal("confirmation hidden in truncated title")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || m.running {
		t.Fatal("blank confirmation installed runtime")
	}
}

func TestConsoleFindFocusMovesHighlightToInput(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := &suiteConsole{width: 100, height: 32, entries: []suiteConsoleEntry{{Name: "agent", Context: "kind-test", Live: &agentTUIAgent{Name: "agent", Ready: "yes"}}}}
	m.columns[0].Env = agentTUIEnvironment{Name: "kind-test", Local: true}
	m.selectSide(0)
	m.Update(tea.KeyPressMsg{Code: 'f'})
	view := m.View().Content
	if !strings.Contains(view, "› Find agents:") {
		t.Fatal("search field not visibly focused")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if strings.Contains(m.View().Content, "› Find agents:") {
		t.Fatal("search retained focus after Enter")
	}
}

func TestInteractiveChatSelectsAvailableLoopbackPort(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	opt, err := interactiveResultOptions("agents")
	if err != nil {
		t.Fatal(err)
	}
	if opt.Namespace != "agents" || opt.ResultPort == "" || "127.0.0.1:"+opt.ResultPort == l.Addr().String() {
		t.Fatalf("invalid chat port %+v", opt)
	}
}

func TestConsoleJobReceivesInstallerPhaseEvents(t *testing.T) {
	t.Setenv("KMX_HOME", t.TempDir())
	a, _, _, _ := extractionFixture(t)
	var workers sync.WaitGroup
	m := &suiteConsole{a: a, ctx: t.Context(), workers: &workers, events: make(chan suiteConsoleProgress, 16)}
	cmd := m.jobNamed("Installing Orka", func(_ context.Context, w *App) suiteConsoleResult {
		return suiteConsoleResult{err: w.runPhase(phase{current: 1, total: 1, name: "Verify pinned chart"}, func() error { return nil })}
	})
	consoleFinish(t, m, cmd)
	m.Update(suiteConsoleTick{})
	if len(m.progress) < 2 || m.progress[0].Stage != "Verify pinned chart" || m.progress[1].State != "done" {
		t.Fatalf("phase events missing %+v", m.progress)
	}
	workers.Wait()
}
