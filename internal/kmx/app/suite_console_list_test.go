package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestSuiteListSortSearchCountsAndRefresh(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := &suiteConsole{input: textinput.New(), width: 140, height: 48, entries: []suiteConsoleEntry{
		{Name: "alpha-agent", Root: "/alpha", Modified: time.Unix(10, 0)},
		{Name: "beta-agent", Root: "/beta", Modified: time.Unix(20, 0)},
		{Name: "unknown-agent", Root: "/unknown"},
		{Name: "remote-agent", Root: "/remote", Side: 1},
	}}
	m.columns[0].Env = agentTUIEnvironment{Name: "kind-test", Local: true}
	for i := range m.entries {
		if m.entries[i].Side == 0 {
			m.entries[i].Context = "kind-test"
			m.entries[i].ObservedReady = true
		}
	}
	m.selectSide(0)
	if got := m.sideIndices(0); !reflect.DeepEqual(got, []int{1, 0, 2}) {
		t.Fatalf("recent order %v", got)
	}
	m.Update(tea.KeyPressMsg{Code: 'S'})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.sideIndices(0); !reflect.DeepEqual(got, []int{0, 1, 2}) || m.entry().Name != "beta-agent" {
		t.Fatalf("oldest sort lost selection %v", got)
	}
	m.setListSort(2)
	if got := m.sideIndices(0); !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Fatal(got)
	}
	m.setListSort(3)
	if got := m.sideIndices(0); !reflect.DeepEqual(got, []int{2, 1, 0}) {
		t.Fatal(got)
	}
	m.Update(tea.KeyPressMsg{Code: 'f'})
	m.Update(tea.KeyPressMsg{Code: 'b', Text: "btagt"})
	if m.entry() == nil || m.entry().Name != "beta-agent" {
		t.Fatal("fuzzy filter failed")
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "1 / 3 matching") || !strings.Contains(view, "1. beta-agent") || !strings.Contains(view, "1 total") {
		t.Fatalf("counts/numbering missing: %s", view)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.acceptRefresh(suiteConsoleResult{entries: []suiteConsoleEntry{m.entries[2], m.entries[0], m.entries[1], m.entries[3]}})
	if m.entry() == nil || m.entry().Name != "beta-agent" || m.listQuery[0] != "btagt" {
		t.Fatal("refresh lost search or selection")
	}
	m.Update(tea.KeyPressMsg{Code: 'f'})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if len(m.sideIndices(0)) != 3 {
		t.Fatal("Escape did not clear search")
	}
}

func TestSuiteListNoMatchesCannotCreateAccidentally(t *testing.T) {
	m := &suiteConsole{input: textinput.New(), entries: []suiteConsoleEntry{{Name: "agent", Root: "/agent"}}}
	m.columns[0].Env = agentTUIEnvironment{Name: "kind-test", Local: true}
	m.entries[0].Context = "kind-test"
	m.entries[0].ObservedReady = true
	m.listQuery[0] = "nothing"
	m.selectSide(0)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.creation != nil {
		t.Fatal("no-match Enter created an agent")
	}
}

func TestSuiteSourceRecencyUsesInstructionsNotCache(t *testing.T) {
	a, p, _ := quickstartSuiteApplyFixture(t)
	instructions := filepath.Join(p.Workspace, "agents", "hello-world-agent", "instructions.md")
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(instructions, future, future); err != nil {
		t.Fatal(err)
	}
	r, err := a.reviewSuiteWorkspace(t.Context(), p.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	if !r.modified.Equal(future) {
		t.Fatalf("source timestamp %v want %v", r.modified, future)
	}
}

func TestSuiteLiveRecencyIgnoresStatusUpdates(t *testing.T) {
	m := agentTUIMetadata{CreationTimestamp: time.Unix(10, 0)}
	m.ManagedFields = append(m.ManagedFields, struct {
		Time        time.Time
		Subresource string
	}{time.Unix(20, 0), ""}, struct {
		Time        time.Time
		Subresource string
	}{time.Unix(100, 0), "status"})
	if !m.modifiedTime().Equal(time.Unix(20, 0)) {
		t.Fatal("status churn reordered live agents")
	}
}

func TestSuiteActionRowBackgroundAndNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	row := newSuiteConsoleTheme().actionRow("c", "Chat with agent", 35)
	if ansi.StringWidth(row) != 35 || !strings.Contains(row, "48;2;36;70;90") {
		t.Fatalf("background/width missing %q", row)
	}
	if !strings.Contains(row, "255;157;61") {
		t.Fatal("shortcut lost orange")
	}
	t.Setenv("NO_COLOR", "1")
	plain := newSuiteConsoleTheme().actionRow("c", "Chat with agent", 35)
	if plain != ansi.Strip(plain) || !strings.HasPrefix(plain, "› c  Chat with agent") {
		t.Fatal("plain action unreadable")
	}
}

func TestSuiteUnavailableCatalogSourcesAreNotAgents(t *testing.T) {
	t.Setenv("KMX_HOME", t.TempDir())
	root := filepath.Join(t.TempDir(), "authored")
	if err := rememberSuiteConsoleEntry(suiteConsoleEntry{Root: root, Name: "authored"}); err != nil {
		t.Fatal(err)
	}
	a, _, _, _ := extractionFixture(t)
	r := loadSuiteConsoleSources(t.Context(), a, AgentTUIOptions{})
	if len(r.entries) != 0 || r.err == nil || !strings.Contains(r.err.Error(), "1 saved source folders unavailable") {
		t.Fatalf("phantom agents: %+v %v", r.entries, r.err)
	}
	hints, err := readSuiteCatalog()
	if err != nil || len(hints) != 1 {
		t.Fatal("unavailable source hint deleted")
	}
}

func TestSuiteAgentRowIncludesInference(t *testing.T) {
	m := &suiteConsole{width: 180, height: 40, entries: []suiteConsoleEntry{{Name: "football", Live: &agentTUIAgent{Name: "football", Provider: "foundry", Model: "DeepSeek-V3.2"}}}}
	m.columns[0].Env = agentTUIEnvironment{Name: "kind-test", Local: true}
	m.entries[0].Context = "kind-test"
	m.entries[0].Live.Ready = "yes"
	m.selectSide(0)
	if !strings.Contains(ansi.Strip(m.View().Content), "1. football · foundry / DeepSeek-V3.2") {
		t.Fatal("inference missing from agent row")
	}
}
