package app

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func suiteMouseClick(m *suiteConsole, x, y int) (tea.Model, tea.Cmd) {
	return m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
}

func suiteMouseHit(t *testing.T, m *suiteConsole, action string, index int) suiteMouseRegion {
	t.Helper()
	var regions []suiteMouseRegion
	copy := *m
	copy.mouseRegions = &regions
	view := copy.View()
	if view.MouseMode != tea.MouseModeCellMotion {
		t.Fatal("mouse reporting disabled")
	}
	for _, r := range regions {
		if r.action == action && r.index == index {
			return r
		}
	}
	t.Fatalf("missing region %s/%d: %+v", action, index, regions)
	return suiteMouseRegion{}
}

func TestSuiteConsoleMouseTabsEmptyActionAndOverlayIsolation(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := &suiteConsole{input: textinput.New(), width: 120, height: 40, selected: -1}
	suiteMouseClick(m, 12, 1)
	if m.tab != 1 {
		t.Fatal("click did not select Members tab")
	}
	// Rendered empty-pane action is row 8 (zero-based), after heading/context/gap.
	rnew := suiteMouseHit(t, m, "new", 1)
	suiteMouseClick(m, rnew.x, rnew.y)
	if m.focus != 1 || m.creation == nil || m.creation.step().kind != "name" {
		t.Fatal("empty remote New agent did not start creation")
	}
	suiteMouseClick(m, 3, 1)
	if m.tab != 1 {
		t.Fatal("overlay click reached underlying tabs")
	}
	m.input.SetValue("mouse-agent")
	r := suiteMouseHit(t, m, "next", 0)
	suiteMouseClick(m, r.x, r.y)
	if m.creation.step().kind != "source" || m.creation.profile != "native" {
		t.Fatal("Continue did not accept agent name")
	}
	r = suiteMouseHit(t, m, "creation", 1)
	suiteMouseClick(m, r.x, r.y)
	if m.creation.step().kind != "manual" {
		t.Fatal("mouse inference mapping wrong")
	}
	r = suiteMouseHit(t, m, "back", 0)
	suiteMouseClick(m, r.x, r.y)
	if m.creation.step().kind != "source" {
		t.Fatal("Back did not navigate")
	}
	r = suiteMouseHit(t, m, "cancel", 0)
	suiteMouseClick(m, r.x, r.y)
	if m.creation != nil {
		t.Fatal("Cancel did not close creation")
	}
}

func TestSuiteConsoleMouseEntryActionsAndWheel(t *testing.T) {
	m := &suiteConsole{input: textinput.New(), width: 120, height: 40, entries: consoleViewEntries()}
	r := suiteMouseHit(t, m, "entry", 1)
	suiteMouseClick(m, r.x, r.y)
	if m.focus != 1 || m.selected != 1 || !m.actionsOpen {
		t.Fatal("entry click did not select/open actions")
	}
	suiteMouseClick(m, 2, 1)
	if !m.actionsOpen {
		t.Fatal("outside click dismissed overlay")
	}
	m.Update(tea.MouseWheelMsg{X: r.x, Y: r.y, Button: tea.MouseWheelDown})
	if m.actionSelection != 1 {
		t.Fatal("wheel did not navigate menu")
	}
	m.running = true
	before := m.actionSelection
	m.Update(tea.MouseWheelMsg{X: 4, Y: 4, Button: tea.MouseWheelDown})
	if m.actionSelection != before {
		t.Fatal("mouse changed selection during operation")
	}
}

func TestSuiteConsoleMouseFilteredChoiceAndWrappedRows(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := &suiteConsole{input: textinput.New(), width: 64, height: 32, creation: &suiteCreateWizard{}}
	m.createStep(suiteCreateStep{kind: "providers", query: "chosen", choices: []suiteCreateChoice{
		{label: "hidden", value: "wrong"},
		{label: "chosen " + strings.Repeat("long name ", 8), value: "first"},
		{label: "chosen second", value: "second"},
	}})
	r := suiteMouseHit(t, m, "creation", 1)
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	if !strings.Contains(lines[r.y], "chosen second") {
		t.Fatalf("hitbox not on rendered row: %q", lines[r.y])
	}
	suiteMouseClick(m, r.x, r.y)
	if m.creation.model != "second" {
		t.Fatal("filtered click used original list offset")
	}
}

func TestSuiteConsoleMouseReviewCannotSkipClippedSource(t *testing.T) {
	m := &suiteConsole{input: textinput.New(), width: 86, height: 24, extraction: &PreparedSuiteExtraction{Summary: SuiteExtractSummary{Instructions: strings.Repeat("review this line\n", 60)}}}
	r := suiteMouseHit(t, m, "next", 0)
	suiteMouseClick(m, r.x, r.y)
	if m.running || m.extraction == nil || m.scroll == 0 {
		t.Fatal("review click skipped unseen source")
	}
	m.width = 54
	m.height = 16
	r = suiteMouseHit(t, m, "next", 0)
	suiteMouseClick(m, r.x, r.y)
	if m.running || m.extraction == nil {
		t.Fatal("resized review click executed extraction")
	}
}
