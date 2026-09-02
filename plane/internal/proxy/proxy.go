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

	"github.com/kaimahi-agents/kaimahi/plane/internal/config"
	"github.com/kaimahi-agents/kaimahi/plane/internal/store"
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
	// P4c approvals: deny-and-pend filing (data path) and the decision
	// surface (admin).
	FileApprovalRequest(ctx context.Context, credential, kind, subject, detail string) (filed bool, err error)
	PendingApprovals(ctx context.Context) ([]store.ApprovalRequest, error)
	ApproveRequest(ctx context.Context, id string, expiresAt *time.Time, maxUses *int32, amount *int64, decidedBy string) (store.Grant, error)
	DenyApprovalRequest(ctx context.Context, id string, decidedBy string) error
	Grants(ctx context.Context, credential string, limit int) ([]store.Grant, []bool, error)
	ApprovalAudit(ctx context.Context, credential string, limit int) ([]store.ApprovalAuditEntry, error)
	// P7b inbound: the audit trail read (admin); the bridge's own data
	// path uses the narrower inbound.Store.
	InboundAudit(ctx context.Context, hook string, limit int) ([]store.InboundAuditEntry, error)
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
