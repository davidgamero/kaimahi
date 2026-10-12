package main

import (
	"bytes"
	"context"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite/workspace"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseLiftRejectsAmbiguousRoutesBeforeConfiguration(t *testing.T) {
	ref := "registry.example/team@sha256:" + strings.Repeat("a", 64)
	for _, args := range [][]string{
		{"agent", "lift", "--release", "registry.example/team:latest", "--environment", "target.json"},
		{"agent", "lift", "bundle", "--release", ref, "--environment", "target.json"},
		{"agent", "lift", "--release", ref},
		{"agent", "lift", "--release", ref, "--environment", "target.json", "--runtime", "orka"},
		{"agent", "lift", "--release", ref, "--environment", "target.json", "--require-evaluated", "local"},
		{"agent", "lift", "--release", ref, "--environment", "target.json", "--context", "other"},
		{"agent", "lift", "bundle", "--environment", "target.json"},
	} {
		var out, diagnostics bytes.Buffer
		deps, loads := testDependencies(&out, &diagnostics)
		if err := execute(args, deps); err == nil {
			t.Fatalf("accepted %v", args)
		}
		if *loads != 0 {
			t.Fatalf("invalid release route loaded configuration: %v", args)
		}
	}
}

func TestSuitePushDiscoversWorkspaceFromMemberDirectory(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(t.TempDir(), "instructions.md")
	if err := os.WriteFile(input, []byte("Hello."), 0600); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Create(context.Background(), root, workspace.CreateOptions{Name: "hello", Model: "model-a", InstructionsFile: input}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(root, "agents", "hello"))
	t.Setenv("KMX_HOME", filepath.Join(t.TempDir(), "state"))
	var out, diagnostics bytes.Buffer
	deps, loads := testDependencies(&out, &diagnostics)
	layout := filepath.Join(t.TempDir(), "layout")
	if err := execute([]string{"suite", "push", "--to-layout", layout, "suite:v1"}, deps); err != nil {
		t.Fatal(err)
	}
	if *loads != 0 {
		t.Fatal("offline workspace push loaded cluster configuration")
	}
	if !strings.Contains(out.String(), "Required member images: []") {
		t.Fatal(out.String())
	}
}
