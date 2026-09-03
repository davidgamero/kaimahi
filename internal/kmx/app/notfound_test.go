package app

import (
	"errors"
	"testing"
)

// The one distinction the capture-then-restore dance rests on: "the object is
// not there" versus "I could not ask". Only the first may be read as "nothing
// to preserve" — the second, read as absence, is how a re-apply silently
// un-governs a live agent, and how `agent create` would scaffold onto the
// keyless preset on a cluster that does have a plane.
//
// These are kubectl's real messages.
func TestOnlyAGenuineNotFoundReadsAsAbsent(t *testing.T) {
	absent := []string{
		`exit status 1: Error from server (NotFound): agents.kagent.dev "hello-world" not found`,
		`exit status 1: Error from server (NotFound): modelconfigs.kagent.dev "governed-ollama" not found`,
	}
	present := []string{
		// The cluster is down. Nothing may be concluded about the object.
		`exit status 1: The connection to the server 127.0.0.1:6443 was refused - did you specify the right host or port?`,
		`exit status 1: Unable to connect to the server: dial tcp: lookup nowhere.invalid: no such host`,
		// Read denied is not absence.
		`exit status 1: Error from server (Forbidden): agents.kagent.dev is forbidden: User "x" cannot get resource "agents"`,
		// The CRD is not installed yet — kagent has not been deployed.
		`exit status 1: error: the server doesn't have a resource type "agent"`,
		// kubectl itself is missing. "executable file not found" must not be
		// mistaken for "the agent is not found".
		`exec: "kubectl": executable file not found in $PATH`,
		`exit status 1: error: You must be logged in to the server (Unauthorized)`,
	}
	for _, message := range absent {
		if !isNotFound(errors.New(message)) {
			t.Errorf("should read as absent: %s", message)
		}
	}
	for _, message := range present {
		if isNotFound(errors.New(message)) {
			t.Errorf("must NOT read as absent: %s", message)
		}
	}
	if isNotFound(nil) {
		t.Error("no error is not an absence")
	}
}
