package meter_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gambtho/kaimahi/plane/internal/meter"
	"github.com/gambtho/kaimahi/plane/internal/store"
)

type fakeUsage struct {
	cents, tokens int64
	err           error
	gotMonth      time.Time
}

func (f *fakeUsage) MonthUsage(_ context.Context, _ string, monthStart time.Time) (int64, int64, error) {
	f.gotMonth = monthStart
	return f.cents, f.tokens, f.err
}

func i64(v int64) *int64 { return &v }

func TestNoCapsNeverQueriesAndAllows(t *testing.T) {
	f := &fakeUsage{err: errors.New("must not be called")}
	m := &meter.Meter{Usage: f}
	require.NoError(t, m.Check(context.Background(), store.Credential{Name: "a"}))
}

func TestFailClosedOnStoreError(t *testing.T) {
	m := &meter.Meter{Usage: &fakeUsage{err: errors.New("db down")}}
	err := m.Check(context.Background(), store.Credential{Name: "a", CapTokens: i64(10)})
	var d meter.Denial
	require.ErrorAs(t, err, &d)
	require.Equal(t, http.StatusForbidden, d.Status)
}

func TestDeniesAtCentsCap(t *testing.T) {
	m := &meter.Meter{Usage: &fakeUsage{cents: 100}}
	err := m.Check(context.Background(), store.Credential{Name: "a", CapCents: i64(100)})
	var d meter.Denial
	require.ErrorAs(t, err, &d)
	require.Equal(t, http.StatusTooManyRequests, d.Status)
}

func TestDeniesAtTokenCap(t *testing.T) {
	m := &meter.Meter{Usage: &fakeUsage{tokens: 5}}
	err := m.Check(context.Background(), store.Credential{Name: "a", CapTokens: i64(5)})
	var d meter.Denial
	require.ErrorAs(t, err, &d)
	require.Equal(t, http.StatusTooManyRequests, d.Status)
}

type fakeGrants struct {
	admit bool
	err   error
	calls int
	needs []store.BudgetNeed
}

func (f *fakeGrants) ConsumeBudgetGrants(_ context.Context, _ string, needs []store.BudgetNeed) (string, error) {
	f.calls++
	f.needs = needs
	if f.err != nil || !f.admit {
		return needs[0].Subject, f.err
	}
	return "", nil
}

func TestBudgetGrantAdmitsOverCap(t *testing.T) {
	g := &fakeGrants{admit: true}
	m := &meter.Meter{Usage: &fakeUsage{tokens: 5}, Grants: g}
	require.NoError(t, m.Check(context.Background(), store.Credential{Name: "a", CapTokens: i64(5)}))
	require.Equal(t, 1, g.calls, "an over-cap admit is one grant transaction")
	require.Equal(t, []store.BudgetNeed{{Subject: "tokens", Used: 5, Cap: 5}}, g.needs)
}

func TestBothCapsExceededIsOneGrantTransaction(t *testing.T) {
	// Both caps exceeded: the grant store gets BOTH needs in one call —
	// all-or-nothing, so a denial on either burns no uses on the other.
	g := &fakeGrants{admit: true}
	m := &meter.Meter{Usage: &fakeUsage{cents: 100, tokens: 5}, Grants: g}
	cred := store.Credential{Name: "a", CapCents: i64(100), CapTokens: i64(5)}
	require.NoError(t, m.Check(context.Background(), cred))
	require.Equal(t, 1, g.calls)
	require.Len(t, g.needs, 2)
	require.Equal(t, "cents", g.needs[0].Subject)
	require.Equal(t, "tokens", g.needs[1].Subject)
}

func TestBudgetGrantDenialNamesSubject(t *testing.T) {
	g := &fakeGrants{admit: false}
	m := &meter.Meter{Usage: &fakeUsage{tokens: 5}, Grants: g}
	err := m.Check(context.Background(), store.Credential{Name: "a", CapTokens: i64(5)})
	var d meter.Denial
	require.ErrorAs(t, err, &d)
	require.Equal(t, "tokens", d.BudgetSubject)
}

func TestBudgetGrantErrorFailsClosed(t *testing.T) {
	g := &fakeGrants{admit: true, err: errors.New("pg down")}
	m := &meter.Meter{Usage: &fakeUsage{cents: 100}, Grants: g}
	err := m.Check(context.Background(), store.Credential{Name: "a", CapCents: i64(100)})
	var d meter.Denial
	require.ErrorAs(t, err, &d, "a grant-store failure must not admit")
	require.Equal(t, "cents", d.BudgetSubject)
}

func TestUnderCapNeverConsultsGrants(t *testing.T) {
	g := &fakeGrants{admit: true}
	m := &meter.Meter{Usage: &fakeUsage{tokens: 4}, Grants: g}
	require.NoError(t, m.Check(context.Background(), store.Credential{Name: "a", CapTokens: i64(5)}))
	require.Zero(t, g.calls, "under-cap traffic must not burn grant uses")
}

func TestNilGrantsPreservesCapDenial(t *testing.T) {
	m := &meter.Meter{Usage: &fakeUsage{tokens: 5}}
	var d meter.Denial
	require.ErrorAs(t, m.Check(context.Background(), store.Credential{Name: "a", CapTokens: i64(5)}), &d)
}

func TestAllowsUnderBothCaps(t *testing.T) {
	f := &fakeUsage{cents: 99, tokens: 4}
	now := time.Date(2026, 8, 31, 15, 4, 5, 0, time.UTC)
	m := &meter.Meter{Usage: f, Now: func() time.Time { return now }}
	cred := store.Credential{Name: "a", CapCents: i64(100), CapTokens: i64(5)}
	require.NoError(t, m.Check(context.Background(), cred))
	require.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), f.gotMonth)
}

type fakeHeadroom struct {
	extra int64
	err   error
}

func (f *fakeHeadroom) LiveBudgetGrantSum(_ context.Context, _, _ string) (int64, error) {
	return f.extra, f.err
}

func TestPreviewNeverConsumes(t *testing.T) {
	// Grants would admit (consuming), but Preview must not touch them:
	// with no headroom credited, an exceeded cap previews as a denial.
	g := &fakeGrants{admit: true}
	m := &meter.Meter{Usage: &fakeUsage{tokens: 5}, Grants: g}
	err := m.Preview(context.Background(), store.Credential{Name: "a", CapTokens: i64(5)})
	var d meter.Denial
	require.ErrorAs(t, err, &d)
	require.Equal(t, http.StatusTooManyRequests, d.Status)
	require.Equal(t, "tokens", d.BudgetSubject)
	require.Zero(t, g.calls, "preview must not consume a grant use")
}

func TestPreviewAdmitsUnderLiveHeadroomWithoutConsuming(t *testing.T) {
	g := &fakeGrants{admit: false}
	m := &meter.Meter{Usage: &fakeUsage{tokens: 5}, Grants: g, Headroom: &fakeHeadroom{extra: 100}}
	require.NoError(t, m.Preview(context.Background(), store.Credential{Name: "a", CapTokens: i64(5)}))
	require.Zero(t, g.calls)
	// Headroom exactly consumed: denied again.
	m.Headroom = &fakeHeadroom{extra: 1}
	m.Usage = &fakeUsage{tokens: 6}
	require.Error(t, m.Preview(context.Background(), store.Credential{Name: "a", CapTokens: i64(5)}))
}

func TestPreviewFailsClosedOnHeadroomError(t *testing.T) {
	m := &meter.Meter{Usage: &fakeUsage{tokens: 5}, Headroom: &fakeHeadroom{err: errors.New("db down")}}
	err := m.Preview(context.Background(), store.Credential{Name: "a", CapTokens: i64(5)})
	var d meter.Denial
	require.ErrorAs(t, err, &d)
	require.Equal(t, http.StatusTooManyRequests, d.Status)
}

func TestPreviewNoCapsAllows(t *testing.T) {
	m := &meter.Meter{Usage: &fakeUsage{err: errors.New("must not be called")}}
	require.NoError(t, m.Preview(context.Background(), store.Credential{Name: "a"}))
}
