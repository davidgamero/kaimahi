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

type Meter struct {
	Usage  UsageSource
	Grants Grants
	Now    func() time.Time // nil = time.Now
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
	if cred.CapCents == nil && cred.CapTokens == nil {
		return nil
	}
	cents, tokens, err := m.Usage.MonthUsage(ctx, cred.Name, MonthStartUTC(m.now()))
	if err != nil {
		slog.Error("meter: spend check failed, denying request",
			"credential", cred.Name, "err", err)
		return Denial{Status: http.StatusForbidden, Msg: "metering unavailable"}
	}
	// Collect every exceeded cap first: grants must cover ALL of them in
	// one transaction, or none is consumed (a denial burns no uses).
	var needs []store.BudgetNeed
	if cred.CapCents != nil && cents >= *cred.CapCents {
		needs = append(needs, store.BudgetNeed{Subject: "cents", Used: cents, Cap: *cred.CapCents})
	}
	if cred.CapTokens != nil && tokens >= *cred.CapTokens {
		needs = append(needs, store.BudgetNeed{Subject: "tokens", Used: tokens, Cap: *cred.CapTokens})
	}
	if len(needs) == 0 {
		return nil
	}
	if failed := m.grantsFail(ctx, cred.Name, needs); failed != "" {
		msg := "monthly budget reached"
		if failed == "tokens" {
			msg = "monthly token budget reached"
		}
		return Denial{Status: http.StatusTooManyRequests, Msg: msg, BudgetSubject: failed}
	}
	return nil
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
