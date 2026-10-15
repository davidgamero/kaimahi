package app

import (
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

func TestSuiteLiftPublicationLatestFuzzyAndHistory(t *testing.T) {
	t.Setenv("KMX_HOME", t.TempDir())
	root := filepath.Join(t.TempDir(), "workspace")
	old := suitePublicationChoice{Reference: "registry.example/alpha@sha256:" + strings.Repeat("a", 64), Evidence: "old-proof", Layout: "old-layout", PublishedAt: time.Unix(1, 0)}
	latest := suitePublicationChoice{Reference: "registry.example/beta@sha256:" + strings.Repeat("b", 64), Evidence: "new-proof", PublishedAt: time.Unix(2, 0), Image: true, Environment: "image-bindings"}
	e := suiteConsoleEntry{Root: root, Name: "agent", Reference: old.Reference, Evidence: old.Evidence, Layout: old.Layout, Published: &old}
	if err := rememberSuiteConsoleEntry(e); err != nil {
		t.Fatal(err)
	}
	e.Reference, e.Evidence, e.Layout, e.Published = latest.Reference, latest.Evidence, "", &latest
	if err := rememberSuiteConsoleEntry(e); err != nil {
		t.Fatal(err)
	}
	// A later observation of an older deployment must not promote it to latest.
	e.Reference, e.Evidence, e.Layout, e.Published = old.Reference, old.Evidence, old.Layout, nil
	if err := rememberSuiteConsoleEntry(e); err != nil {
		t.Fatal(err)
	}
	a, _, _, _ := extractionFixture(t)
	a.captureCommand = func(_ context.Context, c *exec.Cmd) error {
		_, err := io.WriteString(c.Stdout, `{"contexts":[],"clusters":[]}`)
		return err
	}
	var workers sync.WaitGroup
	m := &suiteConsole{a: a, ctx: t.Context(), workers: &workers, events: make(chan suiteConsoleProgress, 256), input: textinput.New(), entries: []suiteConsoleEntry{e}, width: 120, height: 40}
	m.openPublications(e)
	if m.publications.versions[0].Reference != latest.Reference || m.publications.selection != 0 {
		t.Fatal("latest was not default after restart")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	consoleFinish(t, m, cmd)
	if m.liftGuide == nil || m.liftGuide.version.Reference != latest.Reference || m.liftGuide.version.Environment != "image-bindings" {
		t.Fatalf("latest selection did not bypass reference prompt: %+v", m.form)
	}
	m.form = nil
	m.liftGuide = nil
	m.openPublications(e)
	m.Update(tea.KeyPressMsg{Code: 'a', Text: "alpa"})
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	consoleFinish(t, m, cmd)
	if m.liftGuide == nil || m.liftGuide.version.Reference != old.Reference || m.liftGuide.version.Evidence != old.Evidence || m.liftGuide.version.Layout != old.Layout {
		t.Fatal("fuzzy old version lost retrieval/evidence hints")
	}
	workers.Wait()
}

func TestSuiteLiftPublicationEmptyManualAndMouse(t *testing.T) {
	t.Setenv("KMX_HOME", t.TempDir())
	e := suiteConsoleEntry{Root: t.TempDir(), Name: "agent"}
	m := &suiteConsole{input: textinput.New(), entries: []suiteConsoleEntry{e}, width: 120, height: 40}
	m.openPublications(e)
	if !strings.Contains(m.View().Content, "No published versions known") {
		t.Fatal("missing Publish first guidance")
	}
	m.Update(tea.KeyPressMsg{Code: 'z', Text: "zzz"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.publications == nil || m.form != nil || m.running {
		t.Fatal("empty query dispatched operation")
	}
	m.publications.query = "manual"
	m.publications.selection = 0
	r := suiteMouseHit(t, m, "publication", 0)
	suiteMouseClick(m, r.x, r.y)
	if m.publications != nil || m.form == nil || m.form.field != 0 || m.form.values[0] != "" {
		t.Fatal("manual fallback unavailable")
	}
}

func TestSuiteLiftUnpublishedStartsDestinationBeforePublication(t *testing.T) {
	t.Setenv("KMX_HOME", t.TempDir())
	a, p, _ := quickstartSuiteApplyFixture(t)
	r, err := a.reviewSuiteWorkspace(t.Context(), p.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	e := suiteConsoleEntry{Root: r.root, Name: r.name(), Review: r}
	var workers sync.WaitGroup
	m := &suiteConsole{a: a, ctx: t.Context(), workers: &workers, events: make(chan suiteConsoleProgress, 256), input: textinput.New(), entries: []suiteConsoleEntry{e}, authoringView: true}
	consoleFinish(t, m, m.openPublications(e))
	if m.publications != nil || m.form != nil || m.liftGuide == nil || m.liftGuide.stage != "destination" || m.liftGuide.version.Reference != "" {
		t.Fatal("unpublished Lift did not start destination-first guided publication")
	}
	workers.Wait()
}
