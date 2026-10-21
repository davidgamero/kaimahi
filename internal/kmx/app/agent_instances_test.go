package app

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentInstancesOnlyAppearAfterRun(t *testing.T) {
	a := mdFixture(t)
	var out bytes.Buffer
	if err := AgentInstanceStatus(&out, "", "table"); err != nil || !strings.Contains(out.String(), "No agent instances") {
		t.Fatalf("%s %v", &out, err)
	}
	s := NewMarkdownSession(io.Discard, io.Discard)
	s.Provider = "copilot"
	s.executor = &mdFakeExecutor{answer: "Hello"}
	if err := s.StartAgent(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	rows, err := ReadAgentInstances("")
	if err != nil || len(rows) != 1 || rows[0].State != "running" || rows[0].Version != a.Revision {
		t.Fatalf("%+v %v", rows, err)
	}
	if _, _, err := beginAgentInstance(a, ""); err == nil {
		t.Fatal("concurrent instance replaced live owner")
	}
	s.Close()
	rows, err = ReadAgentInstances("")
	if err != nil || rows[0].State != "stopped" {
		t.Fatalf("%+v %v", rows, err)
	}
	if err := os.Remove(a.Path); err != nil {
		t.Fatal(err)
	}
	rows, err = ReadAgentInstances("")
	if err != nil || len(rows) != 1 {
		t.Fatal("status depends on original source file")
	}
}

func TestAgentVersionsAreImmutableAndRerunsUpdate(t *testing.T) {
	a := mdFixture(t)
	lease, frozen, err := beginAgentInstance(a, "")
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Instructions != a.Instructions {
		t.Fatal("wrong snapshot")
	}
	if err = lease.finish(nil); err != nil {
		t.Fatal(err)
	}
	root, _ := agentStateRoot()
	oldPath := filepath.Join(root, "versions", strings.TrimPrefix(a.Revision, "sha256:"), "agent.md")
	info, _ := os.Stat(oldPath)
	lease, _, err = beginAgentInstance(a, "")
	if err != nil {
		t.Fatal(err)
	}
	if lease.record.Generation != 2 {
		t.Fatal("same source did not retrigger")
	}
	_ = lease.finish(nil)
	after, _ := os.Stat(oldPath)
	if !after.ModTime().Equal(info.ModTime()) {
		t.Fatal("rewrote cached version")
	}
	if err = os.WriteFile(a.Path, []byte(strings.Replace(string(a.Source), "Greet the user.", "Say goodbye.", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := ReadMarkdownAgent(a.Path)
	if err != nil {
		t.Fatal(err)
	}
	lease, frozen, err = beginAgentInstance(b, "")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.finish(nil)
	if lease.record.Generation != 3 || lease.record.PreviousVersion != a.Revision || lease.record.Version == a.Revision || frozen.Instructions != "Say goodbye." {
		t.Fatalf("%+v", lease.record)
	}
	old, err := os.ReadFile(oldPath)
	if err != nil || !bytes.Equal(old, a.Source) {
		t.Fatal("old version lost")
	}
}

func TestAgentInstanceAbandonedLeaseIsStopped(t *testing.T) {
	a := mdFixture(t)
	lease, _, err := beginAgentInstance(a, "")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate process death: the kernel releases the lock, but running remains on disk.
	lease.file.Close()
	lease.file = nil
	rows, err := ReadAgentInstances("")
	if err != nil || rows[0].State != "stopped" || rows[0].Finished != nil || !strings.Contains(rows[0].Reason, "owner exited") {
		t.Fatalf("%+v %v", rows, err)
	}
	var persisted AgentInstance
	if err = mdStateRead(lease.path, &persisted); err != nil || persisted.State != "running" {
		t.Fatal("read-only status rewrote instance")
	}
}
