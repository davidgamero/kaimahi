package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestSuiteMemberInputsRefuseBeforeOperationalConfig(t *testing.T) {
	for _, args := range [][]string{
		{"suite", "extract", "original", "--name", "original", "--namespace", "agents", "--workspace", "new"},
		{"suite", "extract", "original", "--name", "copy", "--workspace", "new"},
		{"suite", "status"},
		{"run", "--deployment", "id", "--agent", "agent", "--prompt", "hello"},
		{"chat", "agent", "--deployment", "id"},
		{"chat", "--member", "member", "agent"},
	} {
		var out, diag bytes.Buffer
		deps, loads := testDependencies(&out, &diag)
		if err := execute(args, deps); err == nil || *loads != 0 {
			t.Fatalf("%v: err=%v loads=%d", args, err, *loads)
		}
	}
}
func TestSuiteMemberHelpDoesNotLoadConfig(t *testing.T) {
	for _, args := range [][]string{{"suite", "extract", "--help"}, {"suite", "status", "--help"}, {"suite", "show", "--help"}, {"chat", "--deployment", "id", "--help"}} {
		var out, diag bytes.Buffer
		deps, loads := testDependencies(&out, &diag)
		if err := execute(args, deps); err != nil || *loads != 0 {
			t.Fatalf("%v: %v loads %d", args, err, *loads)
		}
	}
}

func TestRuntimeCheckModesRefuseBeforeLoadingConfig(t *testing.T) {
	for _, flags := range [][]string{{"--plan"}, {"--retain-placement"}, {"--promote"}} {
		args := append([]string{"lift", "suite@sha256:" + strings.Repeat("a", 64), "--check-runtime", "--to-context", "ctx"}, flags...)
		var out, diag bytes.Buffer
		deps, loads := testDependencies(&out, &diag)
		if err := execute(args, deps); err == nil || *loads != 0 {
			t.Fatalf("%v: %v loads=%d", args, err, *loads)
		}
	}
}
