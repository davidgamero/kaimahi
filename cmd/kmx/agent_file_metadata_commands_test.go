package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentfile"
)

type failThenWrite struct {
	calls int
}

func (w *failThenWrite) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == 1 {
		return 0, errors.New("write failed")
	}
	return len(p), nil
}

func metadataPairFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	agent := filepath.Join(root, "greeter.agent.md")
	tests := filepath.Join(root, "greeter.agent.test.md")
	if err := os.WriteFile(agent, []byte("---\nname: greeter\ndescription: Greeting agent\nmodel: fake\ntools: []\n---\n\nGreet the supplied name.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tests, []byte("---\nschemaVersion: 1\n---\n\n```yaml agent-test\nid: greeting\ninput: Ada\nassertions:\n  - id: text\n    type: contains\n    value: Hello\n```\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return agent, tests
}

func TestAgentFileValidateAndInspectUseCheckedPairOffline(t *testing.T) {
	agent, tests := metadataPairFixture(t)
	for _, operation := range []string{"validate", "inspect"} {
		t.Run(operation, func(t *testing.T) {
			var out, diagnostics bytes.Buffer
			deps, loads := testDependencies(&out, &diagnostics)
			if err := execute([]string{operation, agent, "--tests", tests, "-o", "json"}, deps); err != nil {
				t.Fatal(err)
			}
			if *loads != 0 {
				t.Fatal("offline metadata command loaded operational config")
			}
			var raw map[string]any
			if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
				t.Fatal(err)
			}
			if raw["valid"] != true || raw["name"] != "greeter" || raw["cases"] != float64(1) || raw["agentRevision"] == raw["snapshotRevision"] {
				t.Fatalf("result=%v", raw)
			}
			_, hasInstructions := raw["instructions"]
			_, hasExamples := raw["examples"]
			if operation == "validate" && (hasInstructions || hasExamples || strings.Contains(out.String(), "Ada") || strings.Contains(out.String(), "Hello")) {
				t.Fatal("validation leaked authored content")
			}
			if operation == "inspect" && (!hasInstructions || !hasExamples || !strings.Contains(out.String(), "Ada") || !strings.Contains(out.String(), "Hello")) {
				t.Fatal("inspection omitted authored content")
			}
		})
	}
}

func TestAgentFileValidateUsesDefaultAndGitHubPairs(t *testing.T) {
	agent, _ := metadataPairFixture(t)
	github := t.TempDir()
	agentDir := filepath.Join(github, ".github", "agents")
	testsDir := filepath.Join(github, ".github", "agent-tests")
	if err := os.MkdirAll(agentDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(testsDir, 0700); err != nil {
		t.Fatal(err)
	}
	githubAgent := filepath.Join(agentDir, "greeter.agent.md")
	githubTests := filepath.Join(testsDir, "greeter.agent.test.md")
	for source, target := range map[string]string{agent: githubAgent, strings.TrimSuffix(agent, ".md") + ".test.md": githubTests} {
		raw, err := os.ReadFile(source)
		if err != nil || os.WriteFile(target, raw, 0600) != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{agent, githubAgent} {
		want, err := agentfile.LoadPair(path, "")
		if err != nil {
			t.Fatal(err)
		}
		var out, diagnostics bytes.Buffer
		deps, _ := testDependencies(&out, &diagnostics)
		if err := execute([]string{"validate", path, "-o", "json"}, deps); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), `"state":"external"`) || !strings.Contains(out.String(), want.Tests.Selection.Key) || !strings.Contains(out.String(), want.SnapshotRevision) {
			t.Fatalf("pair not selected: %s", &out)
		}
		if strings.Contains(out.String(), filepath.Dir(path)) {
			t.Fatal("metadata disclosed physical source directory")
		}
	}
}

func TestAgentFileMetadataCommandsRefuseInvalidSelectionAndOperationalFlags(t *testing.T) {
	agent, _ := metadataPairFixture(t)
	for _, args := range [][]string{{"validate", agent, "--tests="}, {"inspect", agent, "-o", "yaml"}, {"validate", agent, "--context", "cluster"}, {"inspect", agent, "--container-engine", "docker"}} {
		var out, diagnostics bytes.Buffer
		deps, loads := testDependencies(&out, &diagnostics)
		if err := execute(args, deps); err == nil {
			t.Fatalf("accepted %v", args)
		}
		if *loads != 0 {
			t.Fatalf("invalid offline request loaded config: %v", args)
		}
	}
}

func TestAgentFileMetadataCommandsStayHiddenFromRootHelp(t *testing.T) {
	var out, diagnostics bytes.Buffer
	deps, _ := testDependencies(&out, &diagnostics)
	if err := execute(nil, deps); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "validate ") || strings.HasPrefix(strings.TrimSpace(line), "inspect ") {
			t.Fatal("offline metadata commands changed root front door")
		}
	}
}

func TestAgentFileMetadataTextStopsAfterFirstWriteFailure(t *testing.T) {
	agent, _ := metadataPairFixture(t)
	for _, operation := range []string{"validate", "inspect"} {
		t.Run(operation, func(t *testing.T) {
			var diagnostics bytes.Buffer
			deps, _ := testDependencies(&bytes.Buffer{}, &diagnostics)
			writer := &failThenWrite{}
			deps.stdout = writer
			if err := execute([]string{operation, agent}, deps); err == nil {
				t.Fatal("write failure accepted")
			}
			if writer.calls != 1 {
				t.Fatalf("continued after failed write: calls=%d", writer.calls)
			}
		})
	}
}

func TestAgentFileMetadataCommandsRemainOutsideNativeWindowsBoundary(t *testing.T) {
	agent, _ := metadataPairFixture(t)
	for _, operation := range []string{"validate", "inspect"} {
		var out, diagnostics bytes.Buffer
		deps, loads := testDependencies(&out, &diagnostics)
		deps.goos = "windows"
		err := execute([]string{operation, agent}, deps)
		if !errors.Is(err, errWindowsOperationUnsupported) || *loads != 0 || out.Len() != 0 {
			t.Fatalf("operation=%s err=%v loads=%d out=%q", operation, err, *loads, out.String())
		}
	}
}
