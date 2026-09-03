package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestStaticCompletionCandidates(t *testing.T) {
	for _, tc := range []struct {
		name  string
		words []string
		want  []string
	}{
		{"top level", []string{"ag"}, []string{"agent"}},
		{"agent verbs", []string{"agent", ""}, []string{"--context", "chat", "create", "list"}},
		{"up step", []string{"up", "--step", ""}, []string{"agent", "cluster", "kagent", "model", "ollama", "tools-agent"}},
		{"inline up step", []string{"up", "--step=o"}, []string{"--step=ollama"}},
		{"plane step", []string{"plane", "--step", "s"}, []string{"secrets"}},
		{"status format", []string{"status", "-o", "j"}, []string{"json"}},
		{"agent list format", []string{"agent", "list", "--output=y"}, []string{"--output=yaml"}},
		{"audit kind", []string{"audit", "a"}, []string{"approval"}},
		{"completion shell", []string{"completion", "f"}, []string{"fish"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := completeWords(tc.words); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("completeWords(%v)=%v, want %v", tc.words, got, tc.want)
			}
		})
	}
}

func TestContextCompletionPreservesSlashes(t *testing.T) {
	got := filterCompletions([]string{"team/prod", "kind-local"}, "team/")
	if !reflect.DeepEqual(got, []string{"team/prod"}) {
		t.Fatalf("slash-containing context changed: %v", got)
	}
}

func TestChatAgentPositionIgnoresFlagValues(t *testing.T) {
	flags := map[string][]string{"--json": {}, "--interactive": {}, "--session": nil}
	for _, args := range [][]string{
		{"--interactive"},
		{"--session", "session-1"},
		{"--session=session-1", "--json"},
	} {
		if got := nonFlagWordsWithValues(args, flags); len(got) != 0 {
			t.Fatalf("flags %v produced positional words %v", args, got)
		}
	}
	if got := nonFlagWordsWithValues([]string{"--session", "session-1", "hello-tools"}, flags); !reflect.DeepEqual(got, []string{"hello-tools"}) {
		t.Fatalf("agent positional lost: %v", got)
	}
}

func TestCompletionContextCanAppearAnywhere(t *testing.T) {
	contextName, words := completionContext([]string{"agent", "--context", "kind-other", "chat"})
	if contextName != "kind-other" || !reflect.DeepEqual(words, []string{"agent", "chat"}) {
		t.Fatalf("completion context=%q words=%v", contextName, words)
	}
}

func TestCompletionFilteringIsSortedAndUnique(t *testing.T) {
	got := filterCompletions([]string{"zeta", "alpha", "alpha", "beta"}, "a")
	if !reflect.DeepEqual(got, []string{"alpha"}) {
		t.Fatalf("filtered candidates: %v", got)
	}
}

func TestCompletionScriptsUseTheSideEffectFreeEndpoint(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			var out bytes.Buffer
			original := completionOutput
			completionOutput = &out
			t.Cleanup(func() { completionOutput = original })
			if err := printCompletionScript(shell); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "kmx __complete") {
				t.Fatalf("%s script does not use completion endpoint:\n%s", shell, out.String())
			}
		})
	}
}
