package meter_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kaimahi-agents/kaimahi/plane/internal/meter"
	"github.com/kaimahi-agents/kaimahi/plane/internal/store"
)

// fakeStore records what Reserve hands the locked admission and answers
// with a scripted verdict; the admission logic itself (caps, grants,
// holds under the credential lock) is proven against a real Postgres in
// internal/store.
type fakeStore struct {
	admission store.Admission
	admitErr  error
	calls     int
	gotHold   store.SpendHold
	gotMonth  time.Time
	gotTTL    time.Duration

	cents, tokens int64
	usageErr      error
	extra         int64
	extraErr      error
}

func (f *fakeStore) AdmitSpend(_ context.Context, _ string, hold store.SpendHold, monthStart time.Time, ttl time.Duration) (store.Admission, error) {
	f.calls++
	f.gotHold, f.gotMonth, f.gotTTL = hold, monthStart, ttl
	return f.admission, f.admitErr
}

func (f *fakeStore) MonthCommitted(_ context.Context, _ string, monthStart time.Time) (int64, int64, error) {
	f.gotMonth = monthStart
	return f.cents, f.tokens, f.usageErr
}

func (f *fakeStore) LiveBudgetGrantSum(_ context.Context, _, _ string) (int64, error) {
	return f.extra, f.extraErr
}

func i64(v int64) *int64 { return &v }

func TestReserveAdmitsWithReservation(t *testing.T) {
	f := &fakeStore{admission: store.Admission{ReservationID: "r1"}}
	now := time.Date(2026, 8, 31, 15, 4, 5, 0, time.UTC)
	m := &meter.Meter{Store: f, Now: func() time.Time { return now }}
	res, err := m.Reserve(context.Background(), store.Credential{Name: "a"}, false)
	require.NoError(t, err)
	require.Equal(t, "r1", res.ID)
	require.False(t, res.Granted)
	require.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), f.gotMonth)
	require.Equal(t, meter.DefaultHoldTTL, f.gotTTL)
}

func TestHoldIsTheLeastACallCanSpend(t *testing.T) {
	// One token always; one cent only when the model is priced — a hold
	// is never an estimate, so a free upstream holds no cents.
	require.Equal(t, store.SpendHold{Tokens: 1}, meter.Hold(false))
	require.Equal(t, store.SpendHold{Tokens: 1, Cents: 1}, meter.Hold(true))
	f := &fakeStore{}
	m := &meter.Meter{Store: f}
	_, _ = m.Reserve(context.Background(), store.Credential{Name: "a"}, true)
	require.Equal(t, store.SpendHold{Tokens: 1, Cents: 1}, f.gotHold)
}

func TestReserveDeniesAtCapNamingSubject(t *testing.T) {
	f := &fakeStore{admission: store.Admission{Denied: true, Subject: "tokens"}}
	m := &meter.Meter{Store: f}
	_, err := m.Reserve(context.Background(), store.Credential{Name: "a", CapTokens: i64(5)}, false)
	var d meter.Denial
	require.ErrorAs(t, err, &d)
	require.Equal(t, http.StatusTooManyRequests, d.Status)
	require.Equal(t, "tokens", d.BudgetSubject)
	require.Equal(t, "monthly token budget reached", d.Msg)

	f.admission = store.Admission{Denied: true, Subject: "cents"}
	_, err = m.Reserve(context.Background(), store.Credential{Name: "a", CapCents: i64(5)}, true)
	require.ErrorAs(t, err, &d)
	require.Equal(t, "cents", d.BudgetSubject)
	require.Equal(t, "monthly budget reached", d.Msg)
}

func TestReserveFailsClosedOnStoreError(t *testing.T) {
	m := &meter.Meter{Store: &fakeStore{admitErr: errors.New("db down")}}
	_, err := m.Reserve(context.Background(), store.Credential{Name: "a", CapTokens: i64(10)}, false)
	var d meter.Denial
	require.ErrorAs(t, err, &d)
	require.Equal(t, http.StatusForbidden, d.Status)
	require.Empty(t, d.BudgetSubject, "a store failure files no budget request")
}

func TestReserveFailsClosedOnVanishedCredential(t *testing.T) {
	m := &meter.Meter{Store: &fakeStore{admitErr: store.ErrNotFound}}
	_, err := m.Reserve(context.Background(), store.Credential{Name: "a"}, false)
	var d meter.Denial
	require.ErrorAs(t, err, &d)
	require.Equal(t, http.StatusForbidden, d.Status)
}

func TestReserveReportsGrantedAdmission(t *testing.T) {
	m := &meter.Meter{Store: &fakeStore{admission: store.Admission{ReservationID: "r2", Granted: true}}}
	res, err := m.Reserve(context.Background(), store.Credential{Name: "a", CapTokens: i64(5)}, false)
	require.NoError(t, err)
	require.True(t, res.Granted)
}

func TestHoldTTLOverride(t *testing.T) {
	f := &fakeStore{}
	m := &meter.Meter{Store: f, HoldTTL: time.Minute}
	_, _ = m.Reserve(context.Background(), store.Credential{Name: "a"}, false)
	require.Equal(t, time.Minute, f.gotTTL)
}

func TestPreviewNoCapsNeverQueries(t *testing.T) {
	m := &meter.Meter{Store: &fakeStore{usageErr: errors.New("must not be called")}}
	require.NoError(t, m.Preview(context.Background(), store.Credential{Name: "a"}))
}

func TestPreviewFailsClosedOnStoreError(t *testing.T) {
	m := &meter.Meter{Store: &fakeStore{usageErr: errors.New("db down")}}
	err := m.Preview(context.Background(), store.Credential{Name: "a", CapTokens: i64(10)})
	var d meter.Denial
	require.ErrorAs(t, err, &d)
	require.Equal(t, http.StatusForbidden, d.Status)
}

func TestPreviewDeniesAtEitherCap(t *testing.T) {
	m := &meter.Meter{Store: &fakeStore{cents: 100}}
	var d meter.Denial
	require.ErrorAs(t, m.Preview(context.Background(), store.Credential{Name: "a", CapCents: i64(100)}), &d)
	require.Equal(t, http.StatusTooManyRequests, d.Status)
	require.Equal(t, "cents", d.BudgetSubject)

	m = &meter.Meter{Store: &fakeStore{tokens: 5}}
	require.ErrorAs(t, m.Preview(context.Background(), store.Credential{Name: "a", CapTokens: i64(5)}), &d)
	require.Equal(t, "tokens", d.BudgetSubject)
}

func TestPreviewAllowsUnderBothCaps(t *testing.T) {
	f := &fakeStore{cents: 99, tokens: 4}
	now := time.Date(2026, 8, 31, 15, 4, 5, 0, time.UTC)
	m := &meter.Meter{Store: f, Now: func() time.Time { return now }}
	cred := store.Credential{Name: "a", CapCents: i64(100), CapTokens: i64(5)}
	require.NoError(t, m.Preview(context.Background(), cred))
	require.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), f.gotMonth)
}

func TestPreviewNeverConsumes(t *testing.T) {
	// An exceeded cap with no live headroom previews as a denial, and
	// the locked admission is never touched.
	f := &fakeStore{tokens: 5, admission: store.Admission{ReservationID: "would-admit"}}
	m := &meter.Meter{Store: f}
	err := m.Preview(context.Background(), store.Credential{Name: "a", CapTokens: i64(5)})
	var d meter.Denial
	require.ErrorAs(t, err, &d)
	require.Equal(t, http.StatusTooManyRequests, d.Status)
	require.Equal(t, "tokens", d.BudgetSubject)
	require.Zero(t, f.calls, "preview must not admit or consume")
}

func TestPreviewAdmitsUnderLiveHeadroomWithoutConsuming(t *testing.T) {
	f := &fakeStore{tokens: 5, extra: 100}
	m := &meter.Meter{Store: f}
	require.NoError(t, m.Preview(context.Background(), store.Credential{Name: "a", CapTokens: i64(5)}))
	require.Zero(t, f.calls)
	// Headroom exactly consumed: denied again.
	f.extra, f.tokens = 1, 6
	require.Error(t, m.Preview(context.Background(), store.Credential{Name: "a", CapTokens: i64(5)}))
}

func TestPreviewFailsClosedOnHeadroomError(t *testing.T) {
	// A failed headroom read is a store outage, classified like Reserve's
	// admission failure: 403, and no budget subject to file against.
	m := &meter.Meter{Store: &fakeStore{tokens: 5, extraErr: errors.New("db down")}}
	err := m.Preview(context.Background(), store.Credential{Name: "a", CapTokens: i64(5)})
	var d meter.Denial
	require.ErrorAs(t, err, &d)
	require.Equal(t, http.StatusForbidden, d.Status)
	require.Empty(t, d.BudgetSubject)
}
