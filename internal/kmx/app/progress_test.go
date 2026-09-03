package app

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunPhaseDelimitsNativeOutputAndReportsElapsedTime(t *testing.T) {
	var out bytes.Buffer
	times := []time.Time{
		time.Unix(0, 0),
		time.Unix(0, 0).Add(1250 * time.Millisecond),
	}
	a := &App{Err: &out, now: func() time.Time {
		value := times[0]
		times = times[1:]
		return value
	}}

	err := a.runPhase(phase{current: 2, total: 6, name: "Deploy Ollama"}, func() error {
		out.WriteString("kubectl apply output\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "\nPHASE  [2/6] Deploy Ollama\n" +
		"kubectl apply output\n" +
		"DONE   [2/6] Deploy Ollama (1.3s)\n"
	if out.String() != want {
		t.Fatalf("unexpected phase transcript:\n%q", out.String())
	}
}

func TestRunPhaseReportsFailureWithoutHidingTheError(t *testing.T) {
	var out bytes.Buffer
	times := []time.Time{time.Unix(0, 0), time.Unix(2, 0)}
	a := &App{Err: &out, now: func() time.Time {
		value := times[0]
		times = times[1:]
		return value
	}}
	wantErr := errors.New("rollout timed out")
	err := a.runPhase(phase{current: 3, total: 3, name: "Deploy and verify plane"}, func() error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("phase replaced its underlying error: %v", err)
	}
	if !strings.Contains(out.String(), "FAILED [3/3] Deploy and verify plane (2s)") {
		t.Fatalf("failure boundary missing from %q", out.String())
	}
}

func TestFormatElapsedUsesUsefulPrecision(t *testing.T) {
	for _, tc := range []struct {
		elapsed time.Duration
		want    string
	}{
		{345 * time.Millisecond, "350ms"},
		{12*time.Second + 349*time.Millisecond, "12.3s"},
		{2*time.Minute + 29*time.Second + 600*time.Millisecond, "2m30s"},
	} {
		if got := formatElapsed(tc.elapsed); got != tc.want {
			t.Errorf("formatElapsed(%s)=%q, want %q", tc.elapsed, got, tc.want)
		}
	}
}
