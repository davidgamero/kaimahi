package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

func consoleFinish(t *testing.T, m *suiteConsole, c tea.Cmd) {
	t.Helper()
	if c == nil {
		t.Fatal("no operation")
	}
	msg := c()
	if batch, ok := msg.(tea.BatchMsg); ok {
		msg = batch[0]()
	}
	r, ok := msg.(suiteConsoleResult)
	if !ok {
		t.Fatalf("wrong message %T", msg)
	}
	m.Update(r)
	if r.err != nil {
		t.Fatal(r.err)
	}
}

func TestSuiteConsolePublishPlanApplyRestartAndFreshAnswer(t *testing.T) {
	a, p, f := quickstartSuiteApplyFixture(t)
	r, err := a.reviewSuiteWorkspace(t.Context(), p.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(p.Workspace, "kmx.yaml"), "version: 1\ntargets:\n  dev:\n    context: kind-test\n    namespace: orka-system\n    runtime: orka\n    inference: provider:inference\n")
	var workers sync.WaitGroup
	m := &suiteConsole{a: a, ctx: t.Context(), workers: &workers, input: textinput.New(), events: make(chan suiteConsoleProgress, 256), width: 100, height: 32, entries: []suiteConsoleEntry{{Name: r.suite.Name, Origin: "workspace", Root: r.root, Review: r, Reviewed: true, Evidence: p.Evidence}}}
	d, err := consoleDestination(p.Workspace, "dev")
	if err != nil {
		t.Fatal(err)
	}
	server, err := a.consoleCreateTarget(t.Context(), agentTUIEnvironment{Name: d.Context})
	if err != nil {
		t.Fatal(err)
	}
	_ = server // Evaluation now uses disposable candidates, qualified by shared pipeline tests.
	layout := filepath.Join(t.TempDir(), "published")
	m.submit(suiteConsoleForm{action: "push", values: []string{"workspace:tested", layout, m.entry().Evidence, ""}})
	consoleFinish(t, m, m.submit(suiteConsoleForm{action: "publish-confirm", values: []string{"publish"}}))
	if !strings.Contains(m.entry().Reference, "@sha256:") {
		t.Fatal("publication lost digest")
	}
	e := *m.entry()
	consoleFinish(t, m, m.submit(suiteConsoleForm{action: "lift", values: []string{e.Reference, e.Evidence, "dev", layout, "", ""}}))
	if m.plan == nil {
		t.Fatal("no reviewed plan")
	}
	before := f.taskCreates(t)
	consoleFinish(t, m, m.submit(suiteConsoleForm{action: "apply", values: []string{"kind-test"}}))
	if f.taskCreates(t) != before {
		t.Fatal("Lift sent model request")
	}
	id := m.entry().Deployment
	if id == "" {
		t.Fatal("placement not retained")
	}
	loaded := loadSuiteConsole(t.Context(), a, AgentTUIOptions{Deployment: id})
	if loaded.err != nil {
		t.Fatal(loaded.err)
	}
	found := false
	for _, e := range loaded.entries {
		if e.Deployment == id {
			found = true
		}
	}
	if !found {
		t.Fatal("restart lost placement")
	}
	answer, err := a.RunSuiteMember(context.Background(), id, "hello-world-agent", "A separate first question")
	if err != nil || answer != "A fresh answer" || f.taskCreates(t) != before+1 {
		t.Fatalf("answer=%q err=%v", answer, err)
	}
	if _, err := os.Stat(p.Workspace); err != nil {
		t.Fatal("source deleted")
	}
	workers.Wait()
}

func TestSuiteConsoleImageAuthorReviewAndCancellation(t *testing.T) {
	t.Setenv("KMX_HOME", t.TempDir())
	a, _, _, _ := extractionFixture(t)
	var workers sync.WaitGroup
	m := &suiteConsole{a: a, ctx: t.Context(), workers: &workers, input: textinput.New(), events: make(chan suiteConsoleProgress, 256), width: 100, height: 32}
	root := filepath.Join(t.TempDir(), "image-source")
	consoleFinish(t, m, m.submit(suiteConsoleForm{action: "create", execution: "image-http", values: []string{root, "image-member", "", "Answer briefly."}}))
	e := m.entry()
	if e == nil || e.Review == nil || e.Review.image == nil {
		t.Fatal("no image review")
	}
	if !strings.Contains(m.View().Content, "image-member") {
		t.Fatal("missing rendered image member")
	}
	if c := m.submit(suiteConsoleForm{action: "image-eval", values: []string{"unreviewed", "absent", "kind-test"}}); c != nil || m.running {
		t.Fatal("unreviewed image evaluation started")
	}
	if c := m.submit(suiteConsoleForm{action: "image-push", values: []string{"example.invalid/suite:v1", "", "cancel"}}); c != nil || m.running {
		t.Fatal("cancelled publication started")
	}
	workers.Wait()
}

func TestSuiteConsoleCreateAndExtractActionsAuthorNewWorkspaces(t *testing.T) {
	t.Setenv("KMX_HOME", t.TempDir())
	a, opts, _, _ := extractionFixture(t)
	var workers sync.WaitGroup
	m := &suiteConsole{a: a, ctx: t.Context(), workers: &workers, input: textinput.New(), events: make(chan suiteConsoleProgress, 256), width: 100, height: 32, entries: []suiteConsoleEntry{{Name: opts.Agent, Origin: "standalone", Context: a.Cfg.KubeContext, Live: &agentTUIAgent{Name: opts.Agent, Namespace: opts.Namespace}}}}
	consoleFinish(t, m, m.submit(suiteConsoleForm{action: "extract", values: []string{opts.Workspace, opts.Name}}))
	if m.extraction == nil {
		t.Fatal("extraction skipped review")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	consoleFinish(t, m, cmd)
	if len(m.entries) != 2 || m.entries[0].Live == nil || m.entries[1].Review == nil {
		t.Fatal("original disappeared or no suite created")
	}
	root := filepath.Join(t.TempDir(), "authored")
	consoleFinish(t, m, m.submit(suiteConsoleForm{action: "create", values: []string{root, "new-member", "test-model", "Answer briefly."}}))
	if m.entry().Review.suite.MemberID != "new-member" {
		t.Fatal("create did not author suite member")
	}
	workers.Wait()
}
