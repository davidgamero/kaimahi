// Package delegation holds the test that make and kmx are ONE
// implementation of the journey (D27, condition 1).
//
// The claim is not "the Makefile mentions kmx somewhere". It is that each
// delegating target hands kmx the right work with the right arguments — and
// the only way to know that is to ask make itself, with the same variable
// expansion a developer's invocation gets. `make -n` runs nothing, so this
// stays a unit test: no cluster, no network, no Go toolchain beyond the one
// already running it.
package delegation

import (
	"os/exec"
	"strings"
	"testing"
)

func dryRun(t *testing.T, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}
	cmd := exec.Command("make", append([]string{"-n"}, args...)...)
	cmd.Dir = "../../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make -n %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// Every target the Makefile delegates, and the kmx command it must produce.
func TestMakeTargetsDelegateToKmx(t *testing.T) {
	for _, tc := range []struct {
		target string
		want   string
	}{
		{"up", "bin/kmx up"},
		{"cluster", "bin/kmx up --step cluster"},
		{"ollama", "bin/kmx up --step ollama"},
		{"model", "bin/kmx up --step model"},
		{"kagent", "bin/kmx up --step kagent"},
		{"agent", "bin/kmx up --step agent"},
		{"tools-agent", "bin/kmx up --step tools-agent"},
		{"status", "bin/kmx status"},
		{"down", "bin/kmx down"},
		{"chat", `bin/kmx agent chat hello-world "Hello! Who are you and where are you running?"`},
	} {
		t.Run(tc.target, func(t *testing.T) {
			out := dryRun(t, tc.target)
			if !strings.Contains(out, tc.want) {
				t.Errorf("`make %s` does not invoke `%s`:\n%s", tc.target, tc.want, out)
			}
		})
	}
}

// The delegation has to carry the operator's settings, or `KIND_CLUSTER=mine
// make up` would build one cluster and `KIND_CLUSTER=mine kmx up` another.
func TestDelegationPassesTheOperatorsSettings(t *testing.T) {
	out := dryRun(t, "up", "KIND_CLUSTER=mine", "CONTAINER_ENGINE=podman", "MODEL=llama3", "CHAT_PORT=9999")
	for _, want := range []string{
		"KIND_CLUSTER='mine'",
		"KUBE_CTX='kind-mine'",
		"CONTAINER_ENGINE='podman'",
		"MODEL='llama3'",
		"CHAT_PORT='9999'",
		"KAGENT_VERSION='0.9.12'",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("delegation does not pass %s:\n%s", want, out)
		}
	}
}

// A confirmation given to make must not be asked for again by kmx.
func TestConfirmationRidesThrough(t *testing.T) {
	out := dryRun(t, "down", "KIND_CLUSTER=mine", "KAIMAHI_CONFIRM=kind-mine")
	if !strings.Contains(out, "KAIMAHI_CONFIRM='kind-mine'") {
		t.Errorf("KAIMAHI_CONFIRM is not passed to kmx:\n%s", out)
	}
}

// In a checkout there is one kagent binary, not two: make fetches it, kmx is
// told where it is.
func TestChatHandsKmxTheCheckoutsKagentBinary(t *testing.T) {
	out := dryRun(t, "chat")
	if !strings.Contains(out, "KAGENT='bin/kagent'") {
		t.Errorf("chat does not hand kmx bin/kagent:\n%s", out)
	}
}

// The managed path is NOT kmx's in milestone 1 (D27: no AKS). Its bring-up
// must still be the Makefile's own recipes.
func TestTheManagedPathDoesNotDelegate(t *testing.T) {
	out := dryRun(t, "cluster", "TARGET=aks", "AKS_RESOURCE_GROUP=rg", "ACR_NAME=acr")
	if strings.Contains(out, "bin/kmx up") {
		t.Errorf("TARGET=aks must not route cluster bring-up through kmx:\n%s", out)
	}
	if !strings.Contains(out, "aks-up.sh") {
		t.Errorf("TARGET=aks cluster should still run scripts/aks-up.sh:\n%s", out)
	}
}
