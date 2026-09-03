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

// The plane's manifests are milestone 2's job (D27). They must not ride
// along in the binary: kmx applying a plane manifest is precisely the
// scope this milestone said it would not have.
func TestThePlaneIsNotEmbedded(t *testing.T) {
	// The premise first: this asserts an EXCLUSION, and an exclusion test
	// passes for free once the thing it excludes stops existing. If the
	// plane's manifests move, this test must fail and be rewritten against
	// wherever they went — not quietly keep passing.
	if _, err := os.Stat(filepath.Join("..", "..", "..", "k8s", "plane", "proxy.yaml")); err != nil {
		t.Fatalf("k8s/plane/proxy.yaml is gone, so this exclusion test no longer proves anything: %v", err)
	}
	if _, err := manifest("plane/proxy.yaml"); err == nil {
		t.Error("the plane's manifests must not be embedded in kmx (D27: runtime only)")
	}
}
