package app

import (
	"reflect"
	"testing"
)

func commandNames(commands []slashCommand) []string {
	names := make([]string, 0, len(commands))
	for _, command := range commands {
		names = append(names, command.name)
	}
	return names
}

func TestSlashTrieMatchesPrefixes(t *testing.T) {
	for _, tc := range []struct {
		prefix string
		want   []string
	}{
		{"/", []string{"/exit", "/govern", "/history", "/new", "/resume", "/retry", "/session", "/sessions", "/tools", "/ungovern"}},
		{"/s", []string{"/session", "/sessions"}},
		{"/sess", []string{"/session", "/sessions"}},
		{"/hist", []string{"/history"}},
		{"/unknown", []string{}},
	} {
		if got := commandNames(slashMatches(tc.prefix)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("slashMatches(%q)=%v, want %v", tc.prefix, got, tc.want)
		}
	}
}

func TestSlashCompletionUsesLongestCommonPrefix(t *testing.T) {
	for _, tc := range []struct{ line, want string }{
		{"/hi", "/history"},
		{"/s", "/session"},
		{"/session", "/session"},
		{"/res", "/resume"},
		{"/resume", "/resume "},
		{"/tools", "/tools "},
		{"/unknown", "/unknown"},
	} {
		if got := completeSlash(tc.line, slashMatches(tc.line)); got != tc.want {
			t.Errorf("completeSlash(%q)=%q, want %q", tc.line, got, tc.want)
		}
	}
}

func TestSlashHintsStopAtArguments(t *testing.T) {
	if got := slashMatches("/tools "); got != nil {
		t.Fatalf("argument input produced command hints: %v", got)
	}
	if got := slashHint(slashMatches("/hist")); got != "/history" {
		t.Fatalf("unexpected hint %q", got)
	}
}

func TestSlashHintFitsTerminalWidth(t *testing.T) {
	hint := fitSlashHint(slashHint(slashMatches("/")), 40)
	if len([]rune(hint)) > 38 || hint[len(hint)-3:] != "..." {
		t.Fatalf("hint was not bounded to terminal width: %q", hint)
	}
	if got := fitSlashHint("/history", 40); got != "/history" {
		t.Fatalf("short hint changed: %q", got)
	}
}

func TestSlashRegistryCoversDispatchedCommands(t *testing.T) {
	for _, command := range []string{"/exit", "/govern", "/history", "/new", "/resume", "/retry", "/session", "/sessions", "/tools", "/ungovern"} {
		matches := slashMatches(command)
		found := false
		for _, match := range matches {
			if match.name == command {
				found = true
			}
		}
		if !found {
			t.Errorf("dispatched command %s is absent from slash registry", command)
		}
	}
}
