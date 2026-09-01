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
// consuming a use. *store.Store satisfies it; nil disables grants
// (caps enforce exactly as before).
type Grants interface {
	ConsumeBudgetGrant(ctx context.Context, credential, subject string, used, cap int64) (bool, error)
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
	if cred.CapCents != nil && cents >= *cred.CapCents {
		if !m.grantAdmits(ctx, cred.Name, "cents", cents, *cred.CapCents) {
			return Denial{Status: http.StatusTooManyRequests, Msg: "monthly budget reached", BudgetSubject: "cents"}
		}
	}
	if cred.CapTokens != nil && tokens >= *cred.CapTokens {
		if !m.grantAdmits(ctx, cred.Name, "tokens", tokens, *cred.CapTokens) {
			return Denial{Status: http.StatusTooManyRequests, Msg: "monthly token budget reached", BudgetSubject: "tokens"}
		}
	}
	return nil
}

// grantAdmits asks the grant store to admit one over-cap request,
// consuming a use. Fail closed: no grant machinery, a read error, or an
// insufficient raise all report false — the cap denial stands. The
// grant is evaluated in the store at call time (expiry and use count
// in SQL), never from a cached copy.
func (m *Meter) grantAdmits(ctx context.Context, credential, subject string, used, cap int64) bool {
	if m.Grants == nil {
		return false
	}
	ok, err := m.Grants.ConsumeBudgetGrant(ctx, credential, subject, used, cap)
	if err != nil {
		slog.Error("meter: budget grant check failed, denying", "credential", credential, "subject", subject, "err", err)
		return false
	}
	return ok
}
