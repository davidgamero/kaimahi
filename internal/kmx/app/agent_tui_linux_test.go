package app

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
	"golang.org/x/sys/unix"
)

func TestSuiteConsoleLivePTYCommandsAndTerminalRestore(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("KMX_HOME", t.TempDir())
	master, slave := chatPTY(t, 100)
	before, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	a := New(&config.Config{KubeContext: "kind-test"})
	a.Stdin, a.Out, a.Err = slave, slave, slave
	a.captureCommand = func(_ context.Context, c *exec.Cmd) error {
		_, err := io.WriteString(c.Stdout, `{"contexts":[],"clusters":[]}`)
		return err
	}
	done := make(chan error, 1)
	go func() { done <- a.AgentTUI(AgentTUIOptions{}) }()
	var captured strings.Builder
	chatPTYReadUntil(t, master, &captured, func(s string) bool { return strings.Contains(s, "AGENTSUITES") })
	chatPTYReadUntil(t, master, &captured, func(s string) bool {
		return strings.Contains(s, "SUITES / MEMBERS") && strings.Contains(s, "OPERATION PROGRESS")
	})
	if !strings.Contains(captured.String(), "\x1b[?1049h") {
		t.Fatal("dashboard did not enter alternate screen")
	}
	if !strings.Contains(captured.String(), "\x1b[?1002h") {
		t.Fatal("console did not enable mouse reporting")
	}
	_, _ = io.WriteString(master, "n")
	chatPTYReadUntil(t, master, &captured, func(s string) bool { return strings.Contains(s, "What’s this new agent called?") })
	_, _ = io.WriteString(master, "pty-agent\r")
	chatPTYReadUntil(t, master, &captured, func(s string) bool { return strings.Contains(s, "Connect inference") })
	_, _ = io.WriteString(master, "\x1b")
	time.Sleep(100 * time.Millisecond)
	_, _ = io.WriteString(master, "/lift ")
	chatPTYReadUntil(t, master, &captured, func(s string) bool { return strings.Contains(s, "Command: open") })
	_, _ = io.WriteString(master, "\x03")
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("dashboard did not exit")
	}
	after, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil || *before != *after {
		t.Fatalf("terminal not restored: %v", err)
	}
}
