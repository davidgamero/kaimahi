package app

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

type staticChatBackend struct {
	messages []string
}

func (b *staticChatBackend) Agent() string { return "shared-agent" }
func (b *staticChatBackend) Connect(_ context.Context, renderer *chatRenderer) error {
	renderer.statusStart(b.Agent(), "kind-test")
	renderer.statusSection("Runtime", "test backend")
	renderer.statusEnd()
	return nil
}
func (b *staticChatBackend) Send(_ context.Context, message string, renderer *chatRenderer) error {
	b.messages = append(b.messages, message)
	renderer.beginAssistant(b.Agent())
	renderer.assistant(b.Agent(), "reply to "+message, true)
	return nil
}

func TestSharedInteractiveChatShellDispatchesBackendRetryAndExit(t *testing.T) {
	input, err := os.CreateTemp(t.TempDir(), "chat-input")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if _, err := input.WriteString("hello\n/retry\n/help\n/exit\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := input.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	backend := &staticChatBackend{}
	a := &App{Stdin: input, Out: &out, Err: &out}
	if err := a.runInteractiveChatBackend(backend); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(backend.messages, ","); got != "hello,hello" {
		t.Fatalf("backend messages=%q", got)
	}
	text := out.String()
	for _, want := range []string{"shared-agent", "test backend", "reply to hello", "[CHAT HELP]", "Commands: /help /retry /exit", "Status: ended"} {
		if !strings.Contains(text, want) {
			t.Fatalf("shared chat output missing %q:\n%s", want, text)
		}
	}
}
