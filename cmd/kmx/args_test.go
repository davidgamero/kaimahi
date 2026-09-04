package main

import (
	"reflect"
	"testing"
)

func TestContextIsExtractedFromAnywhere(t *testing.T) {
	for _, tc := range []struct {
		argv     []string
		wantKept []string
		wantCtx  string
	}{
		{[]string{"--context", "kind-x", "up"}, []string{"up"}, "kind-x"},
		{[]string{"up", "--context", "kind-x"}, []string{"up"}, "kind-x"},
		{[]string{"agent", "chat", "hello", "hi", "-context=kind-x"}, []string{"agent", "chat", "hello", "hi"}, "kind-x"},
	} {
		kept, context, err := extractContext(tc.argv)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(kept, tc.wantKept) || context != tc.wantCtx {
			t.Fatalf("%v -> %v %q", tc.argv, kept, context)
		}
	}
	for _, argv := range [][]string{{"up", "--context"}, {"up", "--context="}, {"up", "--context", " "}} {
		if _, _, err := extractContext(argv); err == nil {
			t.Errorf("empty context accepted: %v", argv)
		}
	}
}

func TestChatMessageIsJoined(t *testing.T) {
	if got := joinArgs([]string{"what", "pods", "run?"}); got != "what pods run?" {
		t.Fatalf("message=%q", got)
	}
}

func TestUsageNamesEveryCommand(t *testing.T) {
	root := newRootCommand(&commandState{deps: productionDependencies()})
	for _, command := range []string{"ctx", "up", "agent", "status", "down", "version"} {
		child, _, err := root.Find([]string{command})
		if err != nil || child == root {
			t.Errorf("command tree does not contain %q", command)
		}
	}
}
