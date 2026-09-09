//go:build linux

package app

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
	"golang.org/x/sys/unix"
)

func TestCreateWizardPTYCancellationRestoresTerminal(t *testing.T) {
	master, slave := chatPTY(t, 80)
	before, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	a := &App{Cfg: &config.Config{}, Stdin: slave, Out: &stdout, Err: slave}
	done := make(chan error, 1)
	go func() { done <- a.CreateAgentInteractive(CreateOptions{}) }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		fds := []unix.PollFd{{Fd: int32(master.Fd()), Events: unix.POLLIN}}
		n, pollErr := unix.Poll(fds, 20)
		if pollErr != nil {
			t.Fatal(pollErr)
		}
		if n > 0 {
			var buf [4096]byte
			if _, err := unix.Read(int(master.Fd()), buf[:]); err != nil {
				t.Fatal(err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("wizard did not render")
		}
	}
	if _, err := io.WriteString(master, "\x03"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cancellation returned an error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wizard did not exit on ctrl-c")
	}
	after, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil || *before != *after {
		t.Fatalf("wizard did not restore terminal: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("wizard wrote terminal output to stdout: %q", stdout.String())
	}
}
