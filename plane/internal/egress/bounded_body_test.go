package egress

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stalledReader hands out a short body and then reports whatever the upstream
// stack happened to report when the connection was cut.
type stalledReader struct {
	first []byte
	then  error
	done  bool
}

func (r *stalledReader) Read(p []byte) (int, error) {
	if !r.done {
		r.done = true
		n := copy(p, r.first)
		return n, nil
	}
	return 0, r.then
}

func (r *stalledReader) Close() error { return nil }

// The lifetime deadline must never be reported as a clean end of body.
//
// `TestStalledBodyIsCutAtTheLifetimeDeadline` asserts this end to end, and it
// flaked once in CI with "expected error ... but got nil" — a SHORT body and
// no error, which is precisely the silent truncation this type exists to
// prevent. io.ReadAll returns nil only when the reader returns io.EOF, so
// whatever the transport does at the moment of the cut, the hole is here: a
// read that ends in io.EOF was passed through as a clean end even when our own
// deadline had already fired and cut the body short.
//
// Both shapes are covered because which one the transport produces at the cut
// is exactly the race that made the end-to-end test flaky: an explicit error
// (the common case) and a bare io.EOF (the case that slipped through).
func TestALifetimeCutIsNeverACleanEndOfBody(t *testing.T) {
	for _, tc := range []struct {
		name      string
		upstream  error
		wantMatch error
	}{
		{"the transport reports the cancellation", context.Canceled, ErrBodyLifetime},
		{"the transport reports a deadline", context.DeadlineExceeded, ErrBodyLifetime},
		// The flaky one: the cut arrives looking like a clean end.
		{"the cut arrives as a bare EOF", io.EOF, ErrBodyLifetime},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A context whose deadline has already passed, with our cause —
			// the state the body is in the instant the lifetime is reached.
			ctx, cancel := context.WithTimeoutCause(context.Background(), time.Nanosecond, ErrBodyLifetime)
			defer cancel()
			<-ctx.Done()
			require.ErrorIs(t, context.Cause(ctx), ErrBodyLifetime)

			body := &boundedBody{
				body:   &stalledReader{first: []byte("partial"), then: tc.upstream},
				ctx:    ctx,
				cancel: cancel,
				left:   DefaultMaxResponseBytes,
			}
			got, err := io.ReadAll(body)
			assert.Equal(t, "partial", string(got), "the bytes that did arrive are still handed over")
			require.Error(t, err, "a cut body must never read as a clean end")
			assert.ErrorIs(t, err, tc.wantMatch)
			assert.True(t, IsBodyCut(err), "the cut must classify as a body cut")
		})
	}
}

// A body that ends cleanly while nothing has cut it is still a clean end —
// the fix above must not turn every EOF into an error.
func TestACleanEndIsStillACleanEnd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := &boundedBody{
		body:   &stalledReader{first: []byte("all of it"), then: io.EOF},
		ctx:    ctx,
		cancel: cancel,
		left:   DefaultMaxResponseBytes,
	}
	got, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, "all of it", string(got))
}

// A caller that gave up keeps its own error: only OUR deadline is a lifetime
// cut, and mislabelling the agent going away would send the operator hunting
// for an upstream problem that never happened.
func TestACallerCancellationIsNotALifetimeCut(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body := &boundedBody{
		body:   &stalledReader{first: []byte("partial"), then: context.Canceled},
		ctx:    ctx,
		cancel: cancel,
		left:   DefaultMaxResponseBytes,
	}
	_, err := io.ReadAll(body)
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrBodyLifetime), "a caller's cancellation is not our deadline: %v", err)
}
