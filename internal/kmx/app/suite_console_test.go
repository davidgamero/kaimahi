package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestSuiteConsoleStandaloneDoesNotBecomeOwned(t *testing.T) {
	m := &suiteConsole{ctx: context.Background(), input: textinput.New(), width: 90, height: 25, entries: []suiteConsoleEntry{{Name: "original", Origin: "standalone · not extracted", Live: &agentTUIAgent{Name: "original"}}}}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'x'})
	if cmd == nil || m.form == nil || m.form.action != "extract" {
		t.Fatal("no explicit extraction action")
	}
	if m.entries[0].Root != "" || m.entries[0].Reference != "" || m.entries[0].Deployment != "" {
		t.Fatal("wrapper fabricated suite identity")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m.entries[0].Live.SuiteOwned = true
	m.Update(tea.KeyPressMsg{Code: 'x'})
	if m.form != nil {
		t.Fatal("reserved suite resource offered extraction")
	}
}

func TestSuiteConsoleSmallAndUntrustedRenderingIsBounded(t *testing.T) {
	m := &suiteConsole{input: textinput.New(), width: 12, height: 5, status: "bad\x1b[2Jstatus", entries: []suiteConsoleEntry{{Name: "member\x1b[H", Origin: "standalone"}}}
	text := ansi.Strip(m.View().Content)
	if strings.Contains(text, "\x1b") {
		t.Fatal("untrusted escape escaped")
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 5 {
		t.Fatal("height overflow")
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > 12 {
			t.Fatal("width overflow", line)
		}
	}
}

func TestSuiteConsoleCannotEvaluateBeforeCompleteReview(t *testing.T) {
	m := &suiteConsole{input: textinput.New(), entries: []suiteConsoleEntry{{Name: "suite", Root: "unused"}}}
	if cmd := m.submit(suiteConsoleForm{action: "eval", values: []string{"dev", "context", "", ""}}); cmd != nil {
		t.Fatal("unreviewed evaluation started")
	}
	if !strings.Contains(m.status, "Review all") {
		t.Fatal(m.status)
	}
}

func TestConsoleNamedDestinationSnapshotDoesNotRereadConfig(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "kmx.yaml")
	if err := os.WriteFile(file, []byte("version: 1\ntargets:\n  dev:\n    context: kind-first\n    inference: provider:model\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := consoleDestination(root, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("version: 1\ntargets:\n  dev:\n    context: kind-other\n    inference: provider:model\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f := suiteConsoleForm{destination: &d}
	if f.destination.Context != "kind-first" {
		t.Fatal("selection changed")
	}
}

func TestSuiteConsoleProgressRejectsStaleOperationAndStaysBounded(t *testing.T) {
	m := &suiteConsole{op: 2, events: make(chan suiteConsoleProgress, 256)}
	m.events <- suiteConsoleProgress{1, SuiteProgress{Stage: "stale"}}
	for i := 0; i < 200; i++ {
		m.events <- suiteConsoleProgress{2, SuiteProgress{Stage: "case", State: "pass"}}
	}
	m.Update(suiteConsoleTick{})
	if len(m.progress) != 128 {
		t.Fatal(len(m.progress))
	}
	for _, e := range m.progress {
		if e.Stage == "stale" {
			t.Fatal("stale event displayed")
		}
	}
}

func consoleViewEntries() []suiteConsoleEntry {
	return []suiteConsoleEntry{
		{Name: "support-assistant", Origin: "live", Context: "kind-test", Side: 0, Live: &agentTUIAgent{Name: "support-assistant", Ready: "yes"}},
		{Name: "helper", Origin: "standalone · not extracted", Context: "remote-context", Side: 1, Live: &agentTUIAgent{Name: "helper", Namespace: "agents", Runtime: "orka"}},
	}
}

func TestSuiteConsoleFullPaneLayoutAndViews(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, size := range [][2]int{{120, 40}, {100, 32}, {86, 24}, {72, 28}, {54, 20}, {54, 16}, {12, 5}} {
		m := &suiteConsole{input: textinput.New(), width: size[0], height: size[1], entries: consoleViewEntries(), status: "Live view"}
		m.columns[0].Env = agentTUIEnvironment{Name: "kind-test", Local: true}
		for tab := range suiteConsoleTabs {
			m.tab = tab
			v := m.View()
			if !v.AltScreen {
				t.Fatal("console must own alternate screen")
			}
			text := ansi.Strip(v.Content)
			lines := strings.Split(text, "\n")
			if len(lines) > size[1] {
				t.Fatalf("%v tab %d height %d", size, tab, len(lines))
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("%v width overflow: %q", size, line)
				}
			}
			if size[0] >= 86 {
				for _, want := range []string{"LOCAL", "REMOTE", "OPERATION PROGRESS", "support-assistant", "╭", "╰"} {
					if !strings.Contains(text, want) {
						t.Fatalf("%v missing %q:\n%s", size, want, text)
					}
				}
				if ansi.StringWidth(lines[2]) != size[0] || !strings.HasSuffix(lines[2], "╮") {
					t.Fatalf("panes did not fill width: %q", lines[2])
				}
			}
			if strings.Contains(v.Content, "\x1b") {
				t.Fatal("NO_COLOR contains ANSI")
			}
		}
	}
}

func TestSuiteConsoleFocusActionsAndCommandCompletion(t *testing.T) {
	m := &suiteConsole{input: textinput.New(), width: 100, height: 24, entries: consoleViewEntries()}
	m.columns[0].Env = agentTUIEnvironment{Name: "kind-test", Local: true}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.focus != 1 || m.selected != 1 {
		t.Fatal("right pane did not select remote agent")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.selected != 0 || m.detailScroll != 0 {
		t.Fatal("suite navigation failed")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.actionsOpen || !strings.Contains(ansi.Strip(m.View().Content), "Extract AgentSuite") {
		t.Fatal("missing standalone actions menu")
	}
	m.Update(tea.KeyPressMsg{Code: 'x'})
	if m.actionsOpen || m.form == nil || m.form.action != "extract" {
		t.Fatal("menu did not open extraction review")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m.Update(tea.KeyPressMsg{Code: '/'})
	m.input.SetValue("suite ")
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != "suite show" {
		t.Fatalf("completion selected %q", m.input.Value())
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "COMMAND PALETTE") {
		t.Fatal("completion overlay missing")
	}
}

func TestSuiteConsoleModalBounds(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, key := range []rune{'n', 'o', 't', '?', '/'} {
		m := &suiteConsole{input: textinput.New(), width: 100, height: 28, entries: consoleViewEntries()}
		if key == 't' {
			m.contexts = &suiteContextPicker{side: 0, choices: []suiteContextChoice{{label: "Initialize local environment", action: "init"}}}
		} else {
			m.Update(tea.KeyPressMsg{Code: key})
		}
		v := m.View()
		text := ansi.Strip(v.Content)
		if !v.AltScreen || !strings.Contains(text, "LOCAL") || !strings.Contains(text, "REMOTE") {
			t.Fatalf("%c replaced the persistent dashboard:\n%s", key, text)
		}
		if m.running || m.chatID != "" || m.standaloneName != "" {
			t.Fatalf("%c form executed without submission", key)
		}
		m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
		if m.form != nil || m.help {
			t.Fatalf("%c modal did not close", key)
		}
	}
}

func TestSuiteConsolePaletteOpensRealCreateForm(t *testing.T) {
	m := &suiteConsole{input: textinput.New(), width: 100, height: 32, entries: consoleViewEntries()}
	m.Update(tea.KeyPressMsg{Code: '/'})
	m.input.SetValue("create")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.creation == nil || m.creation.step().kind != "name" || m.running {
		t.Fatal("palette did not open create form")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "What’s this new agent called?") {
		t.Fatal("missing create inputs")
	}
}

func TestSuiteConsoleReviewPagingUsesVisiblePane(t *testing.T) {
	m := &suiteConsole{width: 86, height: 24, extraction: &PreparedSuiteExtraction{Summary: SuiteExtractSummary{Agent: "original", Member: "copy", Instructions: strings.Repeat("review every line of instructions\n", 50)}}}
	_, rows := m.reviewText()
	_, _, page := m.reviewGeometry()
	if len(rows) <= page {
		t.Fatal("fixture must scroll")
	}
	if !m.advanceReview() || m.scroll != page {
		t.Fatal("Enter did not page visible review")
	}
	m.scrollReview("end")
	if m.advanceReview() {
		t.Fatal("last page should permit explicit confirmation")
	}
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 18})
	if !m.advanceReview() {
		t.Fatal("smaller resized viewport must expose remaining review before consent")
	}
}

func TestSuiteConsoleStyleAndPaneFocusRemainSemantic(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	m := &suiteConsole{input: textinput.New(), width: 100, height: 32, entries: consoleViewEntries()}
	m.columns[0].Env = agentTUIEnvironment{Name: "kind-test", Local: true}
	v := m.View()
	if !strings.Contains(v.Content, "\x1b[") {
		t.Fatal("styled view has no emphasis")
	}
	before := v.Content
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if before == m.View().Content {
		t.Fatal("focus has no visual state")
	}
	t.Setenv("NO_COLOR", "1")
	plain := m.View().Content
	for _, want := range []string{"support-assistant", "LOCAL", "REMOTE", "OPERATION PROGRESS"} {
		if !strings.Contains(plain, want) {
			t.Fatal("color carried semantics", want)
		}
	}
}
