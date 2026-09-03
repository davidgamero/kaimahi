package app

import (
	"strings"
	"testing"
)

// The Podman recovery #53 added to `make cluster` moved into kmx when that
// recipe became a delegation. Its fail-closed rule is the part that can be
// tested without a Podman machine: kind listing a cluster that Podman has no
// containers for is a disagreement, not an empty start list.
func TestPodmanNodesFailsClosedOnAnEmptyListing(t *testing.T) {
	for _, listing := range []string{"", "\n", "   \n\t\n"} {
		if _, err := podmanNodes(listing, "kaimahi-p1"); err == nil {
			t.Errorf("an empty node listing (%q) must refuse, not start nothing and carry on", listing)
		} else if !strings.Contains(err.Error(), "kaimahi-p1") {
			t.Errorf("the refusal must name the cluster: %v", err)
		}
	}
}

func TestPodmanNodesReturnsEveryNode(t *testing.T) {
	// A multi-node cluster lists one name per line; all of them get started.
	names, err := podmanNodes("p1-control-plane\np1-worker\np1-worker2\n", "p1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"p1-control-plane", "p1-worker", "p1-worker2"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", names, want)
	}
}
