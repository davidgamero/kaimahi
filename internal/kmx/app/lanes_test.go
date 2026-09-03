package app

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/run"
)

// laneApp is an App wired to a buffer, with a Runner that runs nothing: the
// lane machinery is what is under test, not kubectl.
func laneApp(out *bytes.Buffer) *App {
	return &App{
		Cfg: &config.Config{},
		Run: &run.Runner{Stdout: out, Stderr: out},
		Out: out,
		Err: out,
	}
}

func TestLanesRunConcurrently(t *testing.T) {
	var out bytes.Buffer
	a := laneApp(&out)

	// Each lane blocks until the other has started: a serial implementation
	// deadlocks here and the test times out, which is the point.
	first, second := make(chan struct{}), make(chan struct{})
	err := a.runLanes([]lane{
		{"one", func(b *App) error {
			close(first)
			<-second
			return nil
		}},
		{"two", func(b *App) error {
			close(second)
			<-first
			return nil
		}},
	})
	if err != nil {
		t.Fatalf("both lanes succeeded, got %v", err)
	}
}

func TestEveryLaneRunsEvenAfterOneFails(t *testing.T) {
	var out bytes.Buffer
	a := laneApp(&out)

	// A lane already in flight against the cluster is not abandoned when a
	// sibling fails; both outcomes are reported.
	ran := make(chan string, 2)
	err := a.runLanes([]lane{
		{"broken", func(b *App) error { ran <- "broken"; return errors.New("helm exploded") }},
		{"fine", func(b *App) error { ran <- "fine"; return nil }},
	})
	close(ran)
	seen := map[string]bool{}
	for name := range ran {
		seen[name] = true
	}
	if !seen["broken"] || !seen["fine"] {
		t.Errorf("both lanes must run, saw %v", seen)
	}
	if err == nil || !strings.Contains(err.Error(), "broken: helm exploded") {
		t.Errorf("the failure must name its lane, got %v", err)
	}
	for _, want := range []string{"[broken] START", "[broken] FAILED (", "[fine] START", "[fine] DONE ("} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("lane lifecycle lacks %q:\n%s", want, out.String())
		}
	}
}

func TestBothLaneFailuresAreReported(t *testing.T) {
	var out bytes.Buffer
	a := laneApp(&out)
	err := a.runLanes([]lane{
		{"one", func(b *App) error { return errors.New("first") }},
		{"two", func(b *App) error { return errors.New("second") }},
	})
	if err == nil || !strings.Contains(err.Error(), "one: first") || !strings.Contains(err.Error(), "two: second") {
		t.Errorf("a joined error must carry both lanes, got %v", err)
	}
}

// A single lane is the `--step` path: it runs in place, so its output is
// byte-identical to the serial version (no prefix, no reordering).
func TestASingleLaneIsUnprefixed(t *testing.T) {
	var out bytes.Buffer
	a := laneApp(&out)
	err := a.runLanes([]lane{
		{"only", func(b *App) error { fmt.Fprintln(b.Err, "plain line"); return nil }},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "plain line\n" {
		t.Errorf("a single lane must not be prefixed, got %q", out.String())
	}
}

func TestLaneOutputIsTaggedAndWholeLined(t *testing.T) {
	var out bytes.Buffer
	a := laneApp(&out)
	err := a.runLanes([]lane{
		{"ollama", func(b *App) error {
			// Written in fragments, and ended with a carriage return the
			// way `ollama pull` redraws its progress: both must still
			// arrive as one tagged line.
			b.Err.Write([]byte("pulling "))
			b.Err.Write([]byte("42%\r"))
			fmt.Fprintln(b.Err, "done")
			return nil
		}},
		{"kagent", func(b *App) error {
			fmt.Fprintln(b.Err, "condition met")
			// A trailing partial line is flushed when the lane ends.
			b.Err.Write([]byte("no newline here"))
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"[ollama] pulling 42%\n",
		"[ollama] done\n",
		"[kagent] condition met\n",
		"[kagent] no newline here\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
}

// Interleaved writes must never splice one lane's line into another's.
func TestLinesFromDifferentLanesAreNeverSpliced(t *testing.T) {
	var out bytes.Buffer
	a := laneApp(&out)
	var start sync.WaitGroup
	start.Add(2)
	err := a.runLanes([]lane{
		{"a", func(b *App) error {
			start.Done()
			start.Wait()
			for i := 0; i < 200; i++ {
				b.Err.Write([]byte("aaaa"))
				b.Err.Write([]byte("aaaa\n"))
			}
			return nil
		}},
		{"b", func(b *App) error {
			start.Done()
			start.Wait()
			for i := 0; i < 200; i++ {
				b.Err.Write([]byte("bbbb"))
				b.Err.Write([]byte("bbbb\n"))
			}
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		payload := line == "[a] aaaaaaaa" || line == "[b] bbbbbbbb"
		lifecycle := strings.HasPrefix(line, "[a] START") || strings.HasPrefix(line, "[a] DONE (") ||
			strings.HasPrefix(line, "[b] START") || strings.HasPrefix(line, "[b] DONE (")
		if !payload && !lifecycle {
			t.Fatalf("spliced line %q", line)
		}
	}
}

// failingWriter is a stream that cannot be written to — a closed pipe, a
// full disk. Every write fails.
type failingWriter struct{ writes int }

func (f *failingWriter) Write(p []byte) (int, error) {
	f.writes++
	return 0, errors.New("broken pipe")
}

// Output that could not be written must not read as a lane that ran quietly.
func TestAFailedWriteIsReportedToTheCommandWritingIt(t *testing.T) {
	var mu sync.Mutex
	w := &prefixWriter{mu: &mu, out: &failingWriter{}, prefix: "[x] "}
	n, err := w.Write([]byte("a line\n"))
	if err == nil {
		t.Fatal("a failed write must be reported, not swallowed")
	}
	if n >= len("a line\n") {
		t.Errorf("a failed write must not claim every byte was consumed, got %d", n)
	}
	// The failure is latched: a later write reports it too, rather than
	// succeeding into a stream that is gone.
	if _, err := w.Write([]byte("another line\n")); err == nil {
		t.Error("the latched failure must be reported by later writes")
	}
	if err := w.flush(); err == nil {
		t.Error("flush must report the latched failure")
	}
}

func TestRunLanesFailsWhenALaneOutputCannotBeWritten(t *testing.T) {
	a := laneApp(&bytes.Buffer{})
	a.Err = &failingWriter{}
	err := a.runLanes([]lane{
		{"ollama", func(b *App) error { fmt.Fprintln(b.Err, "pulling"); return nil }},
		{"kagent", func(b *App) error { fmt.Fprintln(b.Err, "waiting"); return nil }},
	})
	if err == nil {
		t.Fatal("lanes whose output went nowhere must not report success")
	}
	for _, want := range []string{"output could not be written", "broken pipe"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
}
