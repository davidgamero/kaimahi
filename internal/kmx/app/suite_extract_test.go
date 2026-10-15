package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite/workspace"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
)

func extractionFixture(t *testing.T) (*App, SuiteExtractOptions, map[string]any, *[]string) {
	t.Helper()
	a := New(&config.Config{KubeContext: "kind-extract"})
	calls := []string{}
	doc := map[string]any{"apiVersion": "core.orka.ai/v1alpha1", "kind": "Agent", "metadata": map[string]any{"name": "original", "namespace": "agents", "uid": "original-uid", "resourceVersion": "1"}, "spec": map[string]any{"providerRef": map[string]any{"name": "model"}, "systemPrompt": map[string]any{"inline": "Greet users briefly."}}}
	a.captureCommand = func(_ context.Context, c *exec.Cmd) error {
		args := strings.Join(c.Args, " ")
		calls = append(calls, args)
		var value any
		switch {
		case strings.Contains(args, "kube-system"):
			value = map[string]any{"kind": "Namespace", "metadata": map[string]any{"name": "kube-system", "uid": "cluster-uid"}}
		case strings.Contains(args, "agents.core.orka.ai"):
			value = doc
		case strings.Contains(args, "providers.core.orka.ai"):
			value = map[string]any{"apiVersion": "core.orka.ai/v1alpha1", "kind": "Provider", "metadata": map[string]any{"name": "model", "namespace": "agents", "uid": "provider-uid", "resourceVersion": "1"}, "spec": map[string]any{"type": "openai", "defaultModel": "test-model"}}
		default:
			t.Fatalf("unexpected extraction request: %s", args)
		}
		return json.NewEncoder(c.Stdout).Encode(value)
	}
	return a, SuiteExtractOptions{Agent: "original", Namespace: "agents", Name: "copy", Workspace: filepath.Join(t.TempDir(), "new")}, doc, &calls
}

func TestExtractionSuggestsAvailableAbsoluteDirectory(t *testing.T) {
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "helper-copy"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "helper-copy-2"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	path, name, err := suggestExtractionWorkspace(parent, "helper")
	if err != nil || name != "helper-copy-3" || path != filepath.Join(parent, name) || !filepath.IsAbs(path) {
		t.Fatalf("suggestion %q %q %v", path, name, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("suggestion wrote source")
	}
	if err := extractionWorkspaceAvailable(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := extractionWorkspaceAvailable(path); err == nil {
		t.Fatal("race-created destination accepted")
	}
}

func TestExtractionOccupiedDestinationRefusesBeforeClusterReads(t *testing.T) {
	a, opt, _, calls := extractionFixture(t)
	if err := os.Mkdir(opt.Workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PrepareSuiteExtraction(t.Context(), opt); err == nil || len(*calls) != 0 {
		t.Fatalf("occupied destination read cluster: %v %v", err, *calls)
	}
}
func TestSuiteExtractionKeepsOriginalAndUsesWorkspaceWriter(t *testing.T) {
	a, o, doc, calls := extractionFixture(t)
	before, _ := json.Marshal(doc)
	p, err := a.PrepareSuiteExtraction(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(o.Workspace); !os.IsNotExist(err) {
		t.Fatal("plan wrote workspace")
	}
	if err = a.ExtractSuite(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	cache := t.TempDir()
	if err := os.Chmod(cache, 0700); err != nil {
		t.Fatal(err)
	}
	prepared, err := workspace.Prepare(t.Context(), o.Workspace, cache)
	if err != nil {
		t.Fatal(err)
	}
	s, err := workspace.Load(t.Context(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	if s.MemberID != "copy" || string(s.Instructions) != "Greet users briefly." || s.Model != "test-model" {
		t.Fatalf("wrong source: %+v", s)
	}
	after, _ := json.Marshal(doc)
	if string(before) != string(after) {
		t.Fatal("original changed")
	}
	for _, c := range *calls {
		if strings.Contains(c, "secret") || strings.Contains(c, " create ") || strings.Contains(c, " patch ") {
			t.Fatal(c)
		}
	}
	if err = a.ExtractSuite(t.Context(), p); err == nil {
		t.Fatal("overwrote output")
	}
}
func TestSuiteExtractionRefusesLossyOwnedAndDriftedSources(t *testing.T) {
	for _, scenario := range []string{"tools", "owned", "drift"} {
		t.Run(scenario, func(t *testing.T) {
			a, o, d, _ := extractionFixture(t)
			if scenario == "tools" {
				d["spec"].(map[string]any)["tools"] = []any{map[string]any{"name": "tool"}}
			}
			if scenario == "owned" {
				d["metadata"].(map[string]any)["annotations"] = map[string]any{orkaSuiteMarkers[0]: nil}
			}
			p, err := a.PrepareSuiteExtraction(t.Context(), o)
			if scenario == "drift" {
				if err != nil {
					t.Fatal(err)
				}
				d["metadata"].(map[string]any)["resourceVersion"] = "2"
				err = a.ExtractSuite(t.Context(), p)
			}
			if err == nil {
				t.Fatal("unsafe extraction accepted")
			}
			if _, err = os.Stat(o.Workspace); !os.IsNotExist(err) {
				t.Fatal("refusal wrote output")
			}
		})
	}
}

func TestSuiteExtractionRefusesEveryReservationAndSecretShapedInstructions(t *testing.T) {
	for _, key := range orkaSuiteMarkers {
		t.Run(key, func(t *testing.T) {
			a, o, d, _ := extractionFixture(t)
			d["metadata"].(map[string]any)["annotations"] = map[string]any{key: ""}
			if _, err := a.PrepareSuiteExtraction(t.Context(), o); err == nil {
				t.Fatal("reserved input accepted")
			}
		})
	}
	a, o, d, _ := extractionFixture(t)
	d["spec"].(map[string]any)["systemPrompt"] = map[string]any{"inline": "\x1b[2J"}
	if _, err := a.PrepareSuiteExtraction(t.Context(), o); err == nil {
		t.Fatal("control-bearing instructions accepted")
	}
}
