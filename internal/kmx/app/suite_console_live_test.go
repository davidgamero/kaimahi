package app

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
)

func TestSuiteConsoleLiveSplitPreservesSuccessfulSide(t *testing.T) {
	t.Setenv("KMX_HOME", t.TempDir())
	a := New(&config.Config{KubeContext: "kind-local"})
	var mu sync.Mutex
	var calls []string
	a.captureCommand = func(_ context.Context, cmd *exec.Cmd) error {
		args := strings.Join(cmd.Args, " ")
		mu.Lock()
		calls = append(calls, args)
		mu.Unlock()
		raw := ""
		switch {
		case strings.Contains(args, "config view"):
			raw = `{"contexts":[{"name":"kind-local","context":{"cluster":"local"}},{"name":"remote","context":{"cluster":"remote"}}],"clusters":[{"name":"local","cluster":{"server":"https://127.0.0.1:6443"}},{"name":"remote","cluster":{"server":"https://remote.example.invalid"}}]}`
		case strings.Contains(args, "--context remote"):
			return errors.New("permission denied")
		case strings.Contains(args, "api-resources"):
			raw = "agents.core.orka.ai\nproviders.core.orka.ai\n"
		case strings.Contains(args, "get agents.core.orka.ai"):
			raw = `{"items":[{"metadata":{"name":"original","namespace":"orka-system","uid":"agent-uid"},"spec":{"systemPrompt":{"inline":"Hello"}},"status":{"ready":true}}]}`
		case strings.Contains(args, "get providers.core.orka.ai"):
			raw = `{"items":[]}`
		default:
			t.Errorf("unexpected read: %s", args)
			return errors.New("unexpected read")
		}
		_, err := io.WriteString(cmd.Stdout, raw)
		return err
	}
	r := loadSuiteConsole(t.Context(), a, AgentTUIOptions{LocalContext: "kind-local", RemoteContext: "remote", Namespace: OrkaNamespace})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if r.columns == nil || r.columns[0].Env.Name != "kind-local" || r.columns[1].Env.Name != "remote" || r.columns[1].Error == "" {
		t.Fatalf("wrong column state: %+v", r.columns)
	}
	if len(r.entries) != 1 || r.entries[0].Side != 0 || r.entries[0].Live.Name != "original" {
		t.Fatalf("successful side lost: %+v", r.entries)
	}
	for _, call := range calls {
		for _, mutate := range []string{" create ", " patch ", " delete ", " apply "} {
			if strings.Contains(call, mutate) {
				t.Fatal("refresh mutated", call)
			}
		}
	}
}

func TestSuiteConsoleRefreshKeepsSideSelectionAndRejectsStaleGeneration(t *testing.T) {
	m := &suiteConsole{entries: consoleViewEntries(), selected: 1, focus: 1, columnSelection: [2]int{0, 0}, refreshGeneration: 2, detail: "answer", detailScroll: 2}
	m.Update(suiteConsoleRefresh{generation: 1, result: suiteConsoleResult{entries: []suiteConsoleEntry{}}})
	if len(m.entries) != 2 {
		t.Fatal("stale refresh erased current selection")
	}
	m.acceptRefresh(suiteConsoleResult{entries: []suiteConsoleEntry{m.entries[1], m.entries[0]}})
	if e := m.entry(); e == nil || e.Name != "helper" || e.Side != 1 || m.detail != "answer" {
		t.Fatal("refresh lost selected side/identity")
	}
}

func TestSuiteConsoleBootLoadingAnimationAndCompletion(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := &suiteConsole{width: 120, height: 40, bootLoading: true, refreshing: true, refreshGeneration: 1}
	first := m.View().Content
	if strings.Count(first, "· LOADING") != 2 || strings.Contains(first, "No agents or suites") || strings.Contains(first, "No context selected") {
		t.Fatal("startup showed empty state instead of loading")
	}
	_, cmd := m.Update(suiteConsoleBootTick{})
	if cmd == nil || m.View().Content == first {
		t.Fatal("loading bar did not animate")
	}
	m.Update(suiteConsoleRefresh{generation: 0})
	if !m.bootLoading {
		t.Fatal("stale refresh stopped startup animation")
	}
	m.Update(suiteConsoleRefresh{generation: 1, result: suiteConsoleResult{entries: []suiteConsoleEntry{}, err: errors.New("discovery unavailable")}})
	if m.bootLoading || strings.Contains(m.View().Content, "· LOADING") || m.discoveryError == "" {
		t.Fatal("loading did not resolve to discovery outcome")
	}
	_, cmd = m.Update(suiteConsoleBootTick{})
	if cmd != nil {
		t.Fatal("animation kept ticking after discovery")
	}
}

func TestKindPaneOnlyObservedReadyAgents(t *testing.T) {
	t.Setenv("KMX_HOME", t.TempDir())
	a := New(&config.Config{KubeContext: "kind-test"})
	fail := false
	a.captureCommand = func(_ context.Context, cmd *exec.Cmd) error {
		args := strings.Join(cmd.Args, " ")
		raw := ""
		switch {
		case strings.Contains(args, "config view"):
			raw = `{"contexts":[{"name":"kind-test","context":{"cluster":"local"}}],"clusters":[{"name":"local","cluster":{"server":"https://127.0.0.1:6443"}}]}`
		case strings.Contains(args, "api-resources"):
			raw = "agents.core.orka.ai\nproviders.core.orka.ai\n"
		case strings.Contains(args, "get agents.core.orka.ai"):
			if fail {
				return errors.New("offline")
			}
			raw = `{"items":[{"metadata":{"name":"ready","uid":"one"},"status":{"ready":true}},{"metadata":{"name":"stopped","uid":"two"},"status":{"ready":false}},{"metadata":{"name":"unknown","uid":"three"}},{"metadata":{"name":"deleting","uid":"four","deletionTimestamp":"2026-10-15T01:00:00Z"},"status":{"ready":true}}]}`
		default:
			raw = `{"items":[]}`
		}
		_, err := io.WriteString(cmd.Stdout, raw)
		return err
	}
	result := loadSuiteConsole(t.Context(), a, AgentTUIOptions{LocalContext: "kind-test"})
	m := &suiteConsole{}
	m.acceptRefresh(result)
	if m.sideTotal(0) != 1 || m.entry() == nil || m.entry().Name != "ready" {
		t.Fatalf("not strictly Ready inventory: %+v", m.entries)
	}
	// Saved source/placement hints cannot manufacture an observed agent.
	m.entries = append(m.entries, suiteConsoleEntry{Name: "source", Root: "/source"}, suiteConsoleEntry{Name: "stale", Context: "kind-test", Deployment: strings.Repeat("a", 32)}, suiteConsoleEntry{Name: "other-cluster", Context: "kind-other", ObservedReady: true})
	if m.sideTotal(0) != 1 {
		t.Fatal("unobserved hints visible")
	}
	fail = true
	m.acceptRefresh(loadSuiteConsole(t.Context(), a, AgentTUIOptions{LocalContext: "kind-test"}))
	if m.sideTotal(0) != 0 || m.columns[0].Error == "" {
		t.Fatal("failed observation retained running claim")
	}
}

func TestKindPaneSeparatesAuthoredFiles(t *testing.T) {
	t.Setenv("KMX_HOME", t.TempDir())
	a, p, _ := quickstartSuiteApplyFixture(t)
	r, err := a.reviewSuiteWorkspace(t.Context(), p.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	m := &suiteConsole{entries: []suiteConsoleEntry{{Name: r.name(), Root: r.root, Review: r}, {Name: "running", Context: "kind-test", Live: &agentTUIAgent{Name: "running", Ready: "yes"}}}}
	m.columns[0].Env = agentTUIEnvironment{Name: "kind-test", Local: true}
	m.selectSide(0)
	if m.sideTotal(0) != 1 || m.entry().Name != "running" {
		t.Fatal("source appeared as running")
	}
	m.authoringView = true
	m.selectSide(0)
	if m.sideTotal(0) != 1 || m.entry().Root != r.root {
		t.Fatal("source not accessible separately")
	}
	if _, err := os.Stat(filepath.Join(r.root, "workspace.yaml")); err != nil {
		t.Fatal("source removed")
	}
}
