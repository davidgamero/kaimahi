package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSharedManagerSessionsAreIndependent(t *testing.T) {
	a := mdFixture(t)
	root := filepath.Dir(filepath.Dir(filepath.Dir(a.Path)))
	if err := InitMarkdownAgent(root, "second"); err != nil {
		t.Fatal(err)
	}
	b, err := ReadMarkdownAgent(filepath.Join(root, ".github", "agents", "second.agent.md"))
	if err != nil {
		t.Fatal(err)
	}
	var executors []*mdFakeExecutor
	m := &localAgentManager{agents: map[string]*managedAgent{}, socket: "shared", lastActive: time.Now(), newExecutor: func() markdownExecutor {
		e := &mdFakeExecutor{answer: "hello"}
		executors = append(executors, e)
		return e
	}}
	defer m.close()
	start := func(a *MarkdownAgent) AgentInstance {
		t.Helper()
		reply := m.handle(t.Context(), agentWorkerRequest{Operation: "start", Source: a.Path, Version: a.Revision})
		if reply.Error != "" || reply.Instance == nil {
			t.Fatalf("%+v", reply)
		}
		return *reply.Instance
	}
	ar, br := start(a), start(b)
	if ar.PID != br.PID || ar.Socket != br.Socket || ar.ID == br.ID || len(executors) != 2 {
		t.Fatal("instances did not share manager with isolated executors")
	}
	call := func(r AgentInstance, op string) agentWorkerReply {
		return m.handle(context.Background(), agentWorkerRequest{AgentID: r.ID, Version: r.Version, Generation: r.Generation, Operation: op, Prompt: "hello"})
	}
	if reply := call(ar, "send"); reply.Error != "" {
		t.Fatal(reply.Error)
	}
	if executors[0].calls != 1 || executors[1].calls != 0 {
		t.Fatal("conversation crossed agents")
	}
	if reply := call(ar, "stop"); reply.Error != "" {
		t.Fatal(reply.Error)
	}
	if reply := call(br, "send"); reply.Error != "" {
		t.Fatal("stopping first agent affected second:", reply.Error)
	}
	if reply := call(ar, "send"); reply.Error == "" {
		t.Fatal("stopped session accepted prompt")
	}
	restarted := start(a)
	if restarted.Generation != 2 {
		t.Fatal("restart generation not incremented")
	}
	if reply := call(ar, "send"); reply.Error == "" {
		t.Fatal("old generation reached restarted session")
	}
}

func TestBorrowedCopilotCloseDoesNotDeleteSharedDirectory(t *testing.T) {
	dir := t.TempDir()
	borrowed := &markdownCopilot{dir: dir, borrowed: true}
	borrowed.Close()
	// A new source can still be created under the shared directory after closing
	// one logical executor. The owning provider is responsible for cleanup.
	if err := InitMarkdownAgent(dir, "still-here"); err != nil {
		t.Fatal(err)
	}
}
