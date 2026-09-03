package scaffold

import (
	"os"
	"strings"
	"testing"
)

// The gateway URL is the string this whole lane exists to stop anyone
// retyping. If it drifts from the committed seam, kmx would scaffold
// upstreams that point somewhere the gateway does not serve — and a
// wrong URL does not announce itself: it is a 404, or worse, a different
// upstream.
func TestTheDerivedGatewayURLMatchesTheCommittedSeam(t *testing.T) {
	raw, err := os.ReadFile("../../../k8s/kaimahi-tools.yaml")
	if err != nil {
		t.Fatalf("the committed seam is the premise of this test: %v", err)
	}
	want := "url: " + GatewayURL("kagent-tools")
	if !strings.Contains(string(raw), want) {
		t.Fatalf("k8s/kaimahi-tools.yaml does not carry %q — the derived URL has drifted", want)
	}
}

// The overlay ConfigMap kmx writes must be the one the proxy mounts.
func TestTheOverlayConfigMapIsTheOneTheProxyMounts(t *testing.T) {
	raw, err := os.ReadFile("../../../k8s/plane/proxy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "name: "+OverlayConfigMap) {
		t.Fatalf("k8s/plane/proxy.yaml does not mount the ConfigMap %q kmx writes", OverlayConfigMap)
	}
}

// The proxy selector every scaffolded policy pins the plane side to is
// the one the committed boundary uses. A drift here would produce a pair
// that opens nothing while reading as correct.
func TestTheProxySelectorMatchesTheCommittedBoundary(t *testing.T) {
	raw, err := os.ReadFile("../../../k8s/plane/network-policy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), ProxySelectorKey+": "+ProxySelectorValue) {
		t.Fatalf("k8s/plane/network-policy.yaml does not select the proxy by %s: %s",
			ProxySelectorKey, ProxySelectorValue)
	}
	// And the generated pair is emitted FROM those constants, so this
	// test guards the generator and not just a string nobody reads.
	doc, err := GenerateUpstream(warehouse())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(doc, ProxySelectorKey+": "+ProxySelectorValue) != 2 {
		t.Fatalf("both scaffolded policies must pin the proxy by the committed label:\n%s", doc)
	}
}
