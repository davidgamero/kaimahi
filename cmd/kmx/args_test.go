package main

import (
	"reflect"
	"strings"
	"testing"
)

// The name comes first and the flags come after — `kmx agent create billing
// --tools server:tool`. The flag package stops at the first non-flag word, so
// without parseInterspersed every flag after the name is silently dropped and
// the command reports a usage error about arguments it refused to read. That
// is not hypothetical: it is what the first end-to-end run of `kmx agent
// create` did.
func TestFlagsAreParsedOnEitherSideOfTheName(t *testing.T) {
	for _, tc := range []struct {
		name      string
		argv      []string
		wantNames []string
		wantTools string
		wantDry   bool
	}{
		{"flags after the name", []string{"billing", "--tools", "server:one,two"}, []string{"billing"}, "server:one,two", false},
		{"flags before the name", []string{"--tools", "server:one", "billing"}, []string{"billing"}, "server:one", false},
		{"flags on both sides", []string{"--dry-run", "billing", "--tools", "server:one"}, []string{"billing"}, "server:one", true},
		{"inline value", []string{"billing", "--tools=server:one"}, []string{"billing"}, "server:one", false},
		{"a bool flag after the name", []string{"billing", "--dry-run"}, []string{"billing"}, "", true},
		{"no flags", []string{"billing"}, []string{"billing"}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := newFlagSet("agent create")
			tools := fs.String("tools", "", "")
			dry := fs.Bool("dry-run", false, "")
			names, err := parseInterspersed(fs, tc.argv)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if !reflect.DeepEqual(names, tc.wantNames) {
				t.Errorf("positional args %v, want %v", names, tc.wantNames)
			}
			if *tools != tc.wantTools {
				t.Errorf("--tools %q, want %q", *tools, tc.wantTools)
			}
			if *dry != tc.wantDry {
				t.Errorf("--dry-run %v, want %v", *dry, tc.wantDry)
			}
		})
	}
}

// Two names is a mistake worth catching, not a name plus a stray word to
// ignore.
func TestASecondPositionalIsKept(t *testing.T) {
	fs := newFlagSet("agent create")
	fs.String("tools", "", "")
	names, err := parseInterspersed(fs, []string{"billing", "investigator"})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Errorf("got %v, want both positionals so the caller can refuse them", names)
	}
}

// --context is global: it may appear before or after the command, because an
// operator who has just been shown a banner naming the wrong cluster should
// be able to append it to what they already typed.
func TestContextIsExtractedFromAnywhere(t *testing.T) {
	for _, tc := range []struct {
		argv     []string
		wantKept []string
		wantCtx  string
	}{
		{[]string{"--context", "kind-x", "up"}, []string{"up"}, "kind-x"},
		{[]string{"up", "--context", "kind-x"}, []string{"up"}, "kind-x"},
		{[]string{"up", "--context=kind-x"}, []string{"up"}, "kind-x"},
		{[]string{"agent", "chat", "hello-world", "hi"}, []string{"agent", "chat", "hello-world", "hi"}, ""},
	} {
		kept, ctx, err := extractContext(tc.argv)
		if err != nil {
			t.Fatalf("%v: %v", tc.argv, err)
		}
		if !reflect.DeepEqual(kept, tc.wantKept) || ctx != tc.wantCtx {
			t.Errorf("%v -> %v, %q; want %v, %q", tc.argv, kept, ctx, tc.wantKept, tc.wantCtx)
		}
	}
	// An empty value must be refused rather than falling through to KUBE_CTX
	// or the default: `--context=$CTX` with an unset CTX is someone aiming at
	// a cluster they cannot name.
	for _, argv := range [][]string{
		{"up", "--context"},
		{"up", "--context="},
		{"up", "--context", "   "},
	} {
		if _, _, err := extractContext(argv); err == nil {
			t.Errorf("%v: an empty --context must be refused, not treated as unset", argv)
		}
	}
}

// The message that is joined back together is the operator's question.
func TestChatMessageIsJoined(t *testing.T) {
	if got := joinArgs([]string{"what", "pods", "are", "running?"}); got != "what pods are running?" {
		t.Errorf("joined %q", got)
	}
}

// `--json` must work where an operator actually types it: at the END, after
// the agent and the question. flag.Parse stops at the first non-flag
// argument, so the naive form appended "--json" to the QUESTION and printed
// the readable view anyway — the flag looked ignored and the agent was asked
// something slightly different. Caught on a live cluster, not in review.
func TestChatJSONFlagIsHonouredAfterThePositionals(t *testing.T) {
	for _, args := range [][]string{
		{"--json", "hello-world", "Who are you?"},
		{"hello-world", "Who are you?", "--json"},
		{"hello-world", "--json", "Who are you?"},
	} {
		fs := newFlagSet("agent chat")
		asJSON := fs.Bool("json", false, "")
		rest, err := parseInterspersed(fs, args)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !*asJSON {
			t.Errorf("%v: --json was not honoured", args)
		}
		if len(rest) == 0 || rest[0] != "hello-world" {
			t.Errorf("%v: agent name lost, got %v", args, rest)
		}
		if got := joinArgs(rest[1:]); got != "Who are you?" {
			t.Errorf("%v: the question must not absorb the flag, got %q", args, got)
		}
	}
}

func TestUsageNamesEveryCommand(t *testing.T) {
	for _, command := range []string{"ctx", "up", "agent create", "agent chat", "status", "down", "version"} {
		if !strings.Contains(usage, command) {
			t.Errorf("usage does not mention %q", command)
		}
	}
}
