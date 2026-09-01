// Package proxy is the governance plane's egress gateway for LLM traffic:
// the budget meter's enforcement point and the only place real upstream
// credentials are attached to outbound requests. It mounts at kagent's
// ModelConfig baseUrl seam — the governed preset points openAI.baseUrl
// here and carries only a Kaimahi-issued opaque token.
//
// Adapted from tomte-old's proxy package. Ported patterns: one upstream
// base and exactly one allowed (method, path) per upstream as the whole
// blast radius; client auth slots stripped before the real credential is
// injected; fail-closed ordering (authenticate, authorize route, meter,
// then forward); ledger writes on a cancel-free context so a client
// disconnect cannot drop the record of a billed call.
package proxy

import (
	"context"
	"net/http"
	"time"

	"github.com/gambtho/kaimahi/plane/internal/config"
	"github.com/gambtho/kaimahi/plane/internal/store"
)

// Store is what the proxy needs from Postgres. *store.Store satisfies it.
type Store interface {
	CredentialByTokenHash(ctx context.Context, tokenHash []byte) (store.Credential, error)
	RecordLedger(ctx context.Context, e store.LedgerEntry) error
	CreateCredential(ctx context.Context, name string, tokenHash []byte) error
	SetBudget(ctx context.Context, name string, capCents, capTokens *int64) error
	Ledger(ctx context.Context, credentialName string, limit int) ([]store.LedgerEntry, error)
	MonthUsage(ctx context.Context, credentialName string, monthStart time.Time) (cents, tokens int64, err error)
	// P4b tool governance (admin surface; the gateway's own data path
	// uses the narrower gateway.Store).
	SetToolAllowlist(ctx context.Context, credentialName string, tools []string) error
	ToolAllowlist(ctx context.Context, credentialName string) ([]string, error)
	ToolAudit(ctx context.Context, credentialName string, limit int) ([]store.ToolAuditEntry, error)
}

// Meter admits or denies a request under the credential's budget caps.
// *meter.Meter satisfies it.
type Meter interface {
	Check(ctx context.Context, cred store.Credential) error
}

type Deps struct {
	Store  Store
	Meter  Meter
	Config config.Config
	// Client makes upstream calls. Nil gets a default that REFUSES
	// redirects (a keyed call must never follow one — standing guidance)
	// and bounds a call at 5 minutes.
	Client *http.Client
}

func (d Deps) client() *http.Client {
	if d.Client != nil {
		return d.Client
	}
	return &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // surface the 3xx; never follow it with a credential
		},
	}
}
