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
	admit    bool
	err      error
	consumed int
	subject  string
}

func (f *fakeGrants) ConsumeBudgetGrant(_ context.Context, _, subject string, _, _ int64) (bool, error) {
	f.consumed++
	f.subject = subject
	return f.admit, f.err
}

func TestBudgetGrantAdmitsOverCap(t *testing.T) {
	g := &fakeGrants{admit: true}
	m := &meter.Meter{Usage: &fakeUsage{tokens: 5}, Grants: g}
	require.NoError(t, m.Check(context.Background(), store.Credential{Name: "a", CapTokens: i64(5)}))
	require.Equal(t, 1, g.consumed, "an over-cap admit consumes exactly one use")
	require.Equal(t, "tokens", g.subject)
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
	require.Zero(t, g.consumed, "under-cap traffic must not burn grant uses")
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
