package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarkdownCommandsOfflineAndBeforeConfig(t *testing.T) {
	path, err := filepath.Abs("../../.github/agents/hello.agent.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"console", "--help"}, {"list", "--type", "md", "--directory", "../.."}, {"inspect", path}, {"test", "--help"},
		{"__complete", "chat", "--type", ""}, {"validate", path},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv("PATH", t.TempDir())
			var out, diagnostics bytes.Buffer
			deps, loads := testDependencies(&out, &diagnostics)
			if err := execute(args, deps); err != nil {
				t.Fatal(err)
			}
			if *loads != 0 {
				t.Fatal("loaded operational config")
			}
		})
	}
	for _, args := range [][]string{
		{"run", path, "--provider", "invalid"}, {"run", path, "--prompt", "hello", "--prompt-file", "-"},
		{"test", path, "--case", "missing"}, {"test", path, "--case="},
		{"inspect", path, "--context", "poison"}, {"inspect", path, "--container-engine", "docker"}, {"md", "list"},
	} {
		var out, diagnostics bytes.Buffer
		deps, loads := testDependencies(&out, &diagnostics)
		if err := execute(args, deps); err == nil {
			t.Fatalf("accepted %v", args)
		}
		if *loads != 0 {
			t.Fatal("loaded operational config")
		}
	}
}

func TestRegisteredAgentNameRoutesLocallyBeforeClusterDiscovery(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KMX_HOME", home)
	path := filepath.Join(t.TempDir(), "frogagent.agent.md")
	id := fmt.Sprintf("md-%x", sha256.Sum256([]byte(path)))
	dir := filepath.Join(home, "agents", "instances", id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	record := map[string]any{"schemaVersion": 1, "id": id, "name": "frogagent", "type": "md", "source": path, "version": "sha256:" + strings.Repeat("a", 64), "generation": 1, "state": "stopped"}
	raw, _ := json.Marshal(record)
	if err := os.WriteFile(filepath.Join(dir, "instance.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "active.lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, selector := range []string{"frogagent", id} {
		var out, diag bytes.Buffer
		deps, loads := testDependencies(&out, &diag)
		err := execute([]string{"chat", selector}, deps)
		if err == nil || !strings.Contains(err.Error(), "is stopped") || *loads != 0 {
			t.Fatalf("selector=%s err=%v loads=%d", selector, err, *loads)
		}
	}
	var out, diag bytes.Buffer
	deps, loads := testDependencies(&out, &diag)
	deps.loadConfig = func(string, string) (*config.Config, error) {
		*loads++
		return nil, fmt.Errorf("explicit Orka selection")
	}
	err := execute([]string{"chat", "frogagent", "--type", "orka"}, deps)
	if err == nil || !strings.Contains(err.Error(), "explicit Orka") || *loads != 1 {
		t.Fatalf("err=%v loads=%d", err, *loads)
	}
}

func TestMarkdownRedirectedBrowserListsWithoutCopilot(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	in, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	var out, diagnostics bytes.Buffer
	deps, loads := testDependencies(&out, &diagnostics)
	deps.stdin = in
	if err := execute([]string{"list", "--type", "md", "--definitions", "--directory", "../.."}, deps); err != nil {
		t.Fatal(err)
	}
	if *loads != 0 || !strings.Contains(out.String(), "hello.agent.md") || strings.Contains(diagnostics.String(), "Copilot CLI found") {
		t.Fatalf("out=%s diagnostics=%s loads=%d", &out, &diagnostics, *loads)
	}
}

func TestTypedAgentNamesAndCreationStayOffline(t *testing.T) {
	var out, diagnostics bytes.Buffer
	deps, loads := testDependencies(&out, &diagnostics)
	for _, args := range [][]string{{"inspect", "hello", "--type", "md", "--directory", "../.."}, {"validate", "hello", "--directory", "../.."}} {
		if err := execute(args, deps); err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	if err := execute([]string{"create", "greeting", "--type", "md", "--directory", root}, deps); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".github", "agents", "greeting.agent.md")); err != nil {
		t.Fatal(err)
	}
	if *loads != 0 {
		t.Fatal("local typed route loaded cluster settings")
	}
}
