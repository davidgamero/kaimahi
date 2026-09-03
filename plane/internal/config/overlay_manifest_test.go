package config

import (
	"os"
	"strings"
	"testing"
)

// The overlay only works if the proxy's Deployment mounts it where the
// proxy looks. Those two facts live in different files, in different
// modules, and nothing else would notice them drifting apart: the
// directory would simply be absent, `Read` would treat that as an empty
// overlay — which is correct behaviour for a cluster with no overlay —
// and every onboarded upstream would silently stop being enforced.
func TestTheOverlayMountMatchesWhereTheProxyLooks(t *testing.T) {
	raw, err := os.ReadFile("../../../k8s/plane/proxy.yaml")
	if err != nil {
		t.Fatalf("the committed proxy manifest is the premise of this test: %v", err)
	}
	manifest := string(raw)
	if !strings.Contains(manifest, "mountPath: "+DefaultConfigDir) {
		t.Fatalf("k8s/plane/proxy.yaml does not mount %s", DefaultConfigDir)
	}
	// A whole-directory mount, not subPath: the ConfigMap grows a key
	// per onboarded server, and subPath would pin a running pod to the
	// keys that existed when it started.
	i := strings.Index(manifest, "mountPath: "+DefaultConfigDir)
	if strings.Contains(manifest[i:i+200], "subPath") {
		t.Fatal("the overlay must be a whole-directory mount, not subPath")
	}
	// Optional: on every cluster where nobody has onboarded anything the
	// ConfigMap does not exist, and the plane must still boot.
	j := strings.Index(manifest, "name: kaimahi-upstreams-extra")
	if j < 0 {
		t.Fatal("k8s/plane/proxy.yaml does not reference the overlay ConfigMap")
	}
	if !strings.Contains(manifest[j:j+120], "optional: true") {
		t.Fatal("the overlay ConfigMap must be optional, or a fresh plane cannot start")
	}
}
