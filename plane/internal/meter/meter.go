// Package meter enforces budget caps at the proxy, before every upstream
// call. Adapted from tomte-old's meter: the fail-closed contract (no spend
// visibility, no spend), the calendar-month UTC window, and the 403/429
// split survive; identity moved from tenant/run to the Kaimahi-issued
// credential, and token caps joined cents caps (the free ollama tier costs
// $0 by classification, so only a token cap can ever exhaust there).
package meter

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gambtho/kaimahi/plane/internal/store"
)

// Denial is a typed refusal the proxy maps onto the HTTP response.
// 429 = budget reached; 403 = metering unavailable (fail closed).
// BudgetSubject names the exceeded cap ('cents' or 'tokens') on a
// budget denial so the caller can file the approval request (P4c);
// empty on other denials.
type Denial struct {
	Status        int
	Msg           string
	BudgetSubject string
}

func (d Denial) Error() string { return d.Msg }

type UsageSource interface {
	MonthUsage(ctx context.Context, credentialName string, monthStart time.Time) (cents, tokens int64, err error)
}

// Grants admits one over-cap request under live budget grants (P4c),
// consuming one use per exceeded cap — all caps in one transaction, so
// a denial burns no uses. *store.Store satisfies it; nil disables
// grants (caps enforce exactly as before).
type Grants interface {
	ConsumeBudgetGrants(ctx context.Context, credential string, needs []store.BudgetNeed) (failedSubject string, err error)
}

// Headroom is the read-only view of live budget grants (P7b): how much a
// cap is currently raised by, consuming nothing. *store.Store satisfies
// it; nil disables previews' grant credit (caps preview exactly).
type Headroom interface {
	LiveBudgetGrantSum(ctx context.Context, credential, subject string) (int64, error)
}

type Meter struct {
	Usage    UsageSource
	Grants   Grants
	Headroom Headroom
	Now      func() time.Time // nil = time.Now
}

func (m *Meter) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// MonthStartUTC is ported verbatim from tomte-old.
func MonthStartUTC(now time.Time) time.Time {
	u := now.UTC()
	return time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// Check denies when either monthly cap is already reached. Fail closed:
// if spend cannot be read, the request is denied — and logged, so
// operators can tell "store down" from "cap reached".
func (m *Meter) Check(ctx context.Context, cred store.Credential) error {
	needs, err := m.exceeded(ctx, cred)
	if err != nil || len(needs) == 0 {
		return err
	}
	if failed := m.grantsFail(ctx, cred.Name, needs); failed != "" {
		return capDenial(failed)
	}
	return nil
}

// Preview answers "would Check admit a call right now?" WITHOUT
// consuming a grant use (P7b: the inbound door refuses an event whose
// spend the proxy could not admit, and leaves the actual consumption to
// the proxy — one use per admitted call, never two per event). Same
// fail-closed contract as Check; a missing Headroom credits nothing.
func (m *Meter) Preview(ctx context.Context, cred store.Credential) error {
	needs, err := m.exceeded(ctx, cred)
	if err != nil || len(needs) == 0 {
		return err
	}
	for _, n := range needs {
		var extra int64
		if m.Headroom != nil {
			extra, err = m.Headroom.LiveBudgetGrantSum(ctx, cred.Name, n.Subject)
			if err != nil {
				slog.Error("meter: budget headroom check failed, denying", "credential", cred.Name, "err", err)
				return capDenial(n.Subject)
			}
		}
		if extra <= 0 || n.Used >= n.Cap+extra {
			return capDenial(n.Subject)
		}
	}
	return nil
}

// exceeded reads month-to-date usage and collects EVERY exceeded cap:
// grants must cover all of them in one transaction, or none is consumed
// (a denial burns no uses).
func (m *Meter) exceeded(ctx context.Context, cred store.Credential) ([]store.BudgetNeed, error) {
	if cred.CapCents == nil && cred.CapTokens == nil {
		return nil, nil
	}
	cents, tokens, err := m.Usage.MonthUsage(ctx, cred.Name, MonthStartUTC(m.now()))
	if err != nil {
		slog.Error("meter: spend check failed, denying request",
			"credential", cred.Name, "err", err)
		return nil, Denial{Status: http.StatusForbidden, Msg: "metering unavailable"}
	}
	var needs []store.BudgetNeed
	if cred.CapCents != nil && cents >= *cred.CapCents {
		needs = append(needs, store.BudgetNeed{Subject: "cents", Used: cents, Cap: *cred.CapCents})
	}
	if cred.CapTokens != nil && tokens >= *cred.CapTokens {
		needs = append(needs, store.BudgetNeed{Subject: "tokens", Used: tokens, Cap: *cred.CapTokens})
	}
	return needs, nil
}

func capDenial(subject string) Denial {
	msg := "monthly budget reached"
	if subject == "tokens" {
		msg = "monthly token budget reached"
	}
	return Denial{Status: http.StatusTooManyRequests, Msg: msg, BudgetSubject: subject}
}

// grantsFail asks the grant store to admit one over-cap request,
// consuming one use per exceeded cap atomically; it returns the first
// uncovered subject ("" = admitted). Fail closed: no grant machinery or
// a store error denies on the first exceeded cap. Grants are evaluated
// in the store at call time (expiry and use count in SQL), never from a
// cached copy.
func (m *Meter) grantsFail(ctx context.Context, credential string, needs []store.BudgetNeed) string {
	if m.Grants == nil {
		return needs[0].Subject
	}
	failed, err := m.Grants.ConsumeBudgetGrants(ctx, credential, needs)
	if err != nil {
		slog.Error("meter: budget grant check failed, denying", "credential", credential, "err", err)
		if failed == "" {
			failed = needs[0].Subject
		}
	}
	return failed
}
