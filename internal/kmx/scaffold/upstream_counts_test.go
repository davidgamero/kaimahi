package scaffold

import (
	"strings"
	"testing"
)

// The CI step asserts on counts over the whole emitted file. Pin those
// counts here, so a generator change that breaks the CI grep fails in
// `go test` first rather than eight minutes into a cluster job.
func TestTheCountsTheCIStepGrepsForHold(t *testing.T) {
	for _, tc := range []struct {
		name         string
		spec         UpstreamSpec
		egressBlocks int
	}{
		{"default posture", warehouse(), 1},
		{"dns posture", func() UpstreamSpec { s := warehouse(); s.ServerDNS = true; return s }(), 2},
		{"keep posture", func() UpstreamSpec { s := warehouse(); s.ServerEgressKeep = true; return s }(), 1},
	} {
		doc, err := GenerateUpstream(tc.spec)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Count(doc, "\n  egress:"); got != tc.egressBlocks {
			t.Fatalf("%s: %d egress blocks, want %d:\n%s", tc.name, got, tc.egressBlocks, doc)
		}
		// The proxy is named exactly twice: it is the subject of the
		// egress policy and the sole peer of the ingress policy.
		if got := strings.Count(doc, "app: kaimahi-proxy"); got != 2 {
			t.Fatalf("%s: the proxy is named %d times, want 2:\n%s", tc.name, got, doc)
		}
	}
}
