package app

import (
	"os"
	"path/filepath"
	"testing"
)

// kmx applies manifests from inside the binary, because it is installed with
// `go install` and run outside a clone. Two things can silently break that: a
// mistyped embed pattern (the file is simply absent at run time, on the
// operator's machine, half way through `kmx up`), and an edit to k8s/ that
// nobody rebuilt against. Assert both here, where it costs nothing.
func TestEmbeddedManifestsAreTheOnesInTheTree(t *testing.T) {
	for _, name := range []string{"ollama.yaml", "kagent-values.yaml", "hello-world.yaml", "tools-agent.yaml"} {
		embedded, err := manifest(name)
		if err != nil {
			t.Errorf("k8s/%s is not embedded in the binary: %v", name, err)
			continue
		}
		onDisk, err := os.ReadFile(filepath.Join("..", "..", "..", "k8s", name))
		if err != nil {
			t.Fatalf("k8s/%s: %v", name, err)
		}
		if string(embedded) != string(onDisk) {
			t.Errorf("k8s/%s differs from the embedded copy", name)
		}
	}
}

// Milestone 2 puts the plane's manifests and the two governed presets in the
// binary, for the same reason as the runtime ones: `kmx plane` and
// `kmx govern` run outside a clone, with no k8s/ on disk to point kubectl at.
func TestThePlanesManifestsTravelInTheBinary(t *testing.T) {
	for _, name := range []string{
		"plane/namespace.yaml", "plane/postgres.yaml", "plane/proxy.yaml",
		"plane/upstreams.yaml", "plane/network-policy.yaml",
		"models/governed-ollama.yaml", "models/governed-copilot.yaml",
	} {
		embedded, err := manifest(name)
		if err != nil {
			t.Errorf("k8s/%s is not embedded in the binary: %v", name, err)
			continue
		}
		onDisk, err := os.ReadFile(filepath.Join("..", "..", "..", "k8s", filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("k8s/%s: %v", name, err)
		}
		if string(embedded) != string(onDisk) {
			t.Errorf("k8s/%s differs from the embedded copy", name)
		}
	}
}

// What kmx carries is a decision, not a directory listing. Milestone 3's
// families must NOT ride along: their targets are still the Makefile's, and
// a manifest in the binary that no kmx command applies is a claim kmx cannot
// honour. The hosted-model presets are excluded for a second reason — they
// need a captured key, and kmx accepts a credential in no form at all (D27).
func TestMilestoneThreesManifestsAreNotEmbedded(t *testing.T) {
	for _, name := range []string{
		"kaimahi-slack.yaml", "slack-agent.yaml", "slack-mcp.yaml",
		"kaimahi-github.yaml", "github-agent.yaml",
		"inbound-edge.yaml", "egress-copilot.yaml", "egress-hosted.yaml",
		"kaimahi-tools.yaml",
		"models/openai.yaml", "models/anthropic.yaml", "models/github-copilot.yaml",
	} {
		// The premise first: this asserts an EXCLUSION, and an exclusion
		// passes for free once the thing it excludes stops existing. If a
		// manifest moves, this test must fail and be rewritten against
		// wherever it went — not quietly keep passing.
		if _, err := os.Stat(filepath.Join("..", "..", "..", "k8s", filepath.FromSlash(name))); err != nil {
			t.Fatalf("k8s/%s is gone, so this exclusion no longer proves anything: %v", name, err)
		}
		if _, err := manifest(name); err == nil {
			t.Errorf("k8s/%s is embedded in kmx, but no kmx command applies it (milestone 3)", name)
		}
	}
}
