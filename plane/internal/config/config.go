// Package config loads the proxy's upstream table from a mounted file
// (committed ConfigMap — no key material lives here; credential values
// come from Secret-mounted files the config only names). The table plays
// tomte-old's ProviderRoute role: one upstream base and exactly one
// allowed forwarded path per upstream is the whole blast radius.
package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/kaimahi-agents/kaimahi/plane/internal/pricing"
)

const (
	// ClassFree is an EXPLICIT $0 classification (in-cluster ollama).
	// Never inferred — standing guidance forbids blanket $0 by inference.
	ClassFree = "free"
	// ClassMetered counts tokens always; cost applies only when a real
	// price row is configured for the model (the priced-pair gate).
	ClassMetered = "metered"
)

type Upstream struct {
	// BaseURL is the upstream origin plus any path prefix it expects.
	BaseURL string `json:"base_url"`
	// Path is the single allowed forwarded remainder (no leading slash) —
	// exactly what kagent's OpenAI client appends to the governed preset's
	// baseUrl (e.g. "v1/chat/completions").
	Path           string `json:"path"`
	Classification string `json:"classification"`
	// CredentialFile, when set, is a Secret-mounted file holding the real
	// upstream credential; read per request so rotation needs no restart.
	// Empty means the upstream is keyless and requests are forwarded bare.
	CredentialFile string `json:"credential_file,omitempty"`
	// CredentialHeader is the header the credential is injected into.
	// "authorization" (the default) sends "Authorization: Bearer <v>".
	CredentialHeader string `json:"credential_header,omitempty"`
	// ExtraHeaders are set on every forwarded request (after client-header
	// passthrough, so they win). Non-secret values only.
	ExtraHeaders map[string]string `json:"extra_headers,omitempty"`
	// Prices maps model name -> configured price. Only meaningful on
	// metered upstreams.
	Prices map[string]pricing.Price `json:"prices,omitempty"`
}

// ToolUpstream is one MCP tool server the gateway may relay to. The
// committed table is the whole egress surface at this layer: the gateway
// forwards nowhere it does not name (cluster-level NetworkPolicy is a
// documented P4b limitation, not built here).
type ToolUpstream struct {
	// URL is the full MCP endpoint (e.g. the in-cluster
	// http://kagent-tools.kagent:8084/mcp).
	URL string `json:"url"`
	// CredentialFile, when set, is a Secret-mounted file holding the
	// tool server's OWN bearer credential — the same proxy-side custody
	// the LLM upstreams use (Upstream.CredentialFile), applied to the
	// tool seam: the gateway injects it, so a tool server can refuse
	// every caller that did not come through the gateway. Read per
	// request, so rotation needs no restart. Empty means the upstream
	// is unauthenticated and requests are forwarded bare.
	CredentialFile string `json:"credential_file,omitempty"`
	// CredentialHeader is the header the credential is injected into.
	// "authorization" (the default) sends "Authorization: Bearer <v>".
	CredentialHeader string `json:"credential_header,omitempty"`
}

// Inbound authentication modes. Every mode binds the caller to the hook's
// configured credential; they differ in how the caller PROVES it.
const (
	// AuthBearer: the caller presents the credential's own kmh_ token as
	// a bearer (for sources that can set a header but cannot sign).
	AuthBearer = "bearer"
	// AuthKaimahiHMAC: Kaimahi's generic signed-webhook scheme — HMAC-SHA256
	// over "v1:<timestamp>:<delivery-id>:<body>" with a shared signing
	// secret (Secret-mounted, plane-side only). Preferred over a bearer:
	// the secret never travels, the body is bound, and the timestamp plus
	// signed delivery id give replay protection.
	AuthKaimahiHMAC = "kaimahi-hmac"
	// AuthSlack: Slack's request signing for the Events API —
	// "v0:<timestamp>:<body>" under the app's signing secret.
	AuthSlack = "slack"
)

// InboundHook is one webhook the plane accepts: who may fire it (a
// credential and a proof mode), what it triggers (a kagent agent, invoked
// through the controller's A2A endpoint), whose budget the resulting
// spend lands in, and its ingress bounds. The committed table is the
// whole inbound surface: the plane accepts nothing it does not name.
type InboundHook struct {
	// Credential is the plane credential this hook is bound to: the
	// identity that is granted (P4c 'inbound' grants, subject = hook
	// name) and audited. A bearer caller must present THIS credential's
	// token; an HMAC caller proves it via the signing secret.
	Credential string `json:"credential"`
	// Auth is one of the Auth* modes above.
	Auth string `json:"auth"`
	// SigningSecretFile, required for the HMAC modes, is a Secret-mounted
	// file holding the shared signing secret; read per request so
	// rotation needs no restart. Unreadable at request time fails the
	// hook closed (503) — never open.
	SigningSecretFile string `json:"signing_secret_file,omitempty"`
	// AgentNamespace/Agent name the kagent Agent the event triggers.
	AgentNamespace string `json:"agent_namespace"`
	Agent          string `json:"agent"`
	// SlackChannelsFile (slack auth only, P8) names a Secret-mounted file
	// listing the channel IDs whose mentions may trigger this hook —
	// comma- or newline-separated, read per request. The committed table
	// names the FILE because a channel ID is a workspace identifier this
	// public repo never carries; the demo mounts the same Secret key
	// that restricts where the Slack MCP server may post
	// (SLACK_MCP_ADD_MESSAGE_TOOL), so "the private test channel" has one
	// source of truth for both directions. Unreadable, empty, or a
	// server-side "anywhere" value fails the hook closed (503). Required
	// for slack auth: a hook without it would trigger from any room the
	// app is mentioned in, and an ingress that widens because a key was
	// dropped from the table is the silent failure this file refuses.
	SlackChannelsFile string `json:"slack_channels_file,omitempty"`
	// BudgetCredential is the governed credential the triggered agent
	// spends under (its governed preset's credential). An event is
	// refused at the door when that budget is already exhausted, so the
	// agent is not invoked only to be denied at the proxy.
	BudgetCredential string `json:"budget_credential"`
	// MaxBodyBytes bounds the event payload (default 64 KiB).
	MaxBodyBytes int64 `json:"max_body_bytes,omitempty"`
	// RatePerMinute/Burst size the per-hook token bucket (defaults 60/10).
	RatePerMinute int `json:"rate_per_minute,omitempty"`
	Burst         int `json:"burst,omitempty"`
}

const (
	DefaultInboundMaxBody = 64 << 10
	DefaultInboundRate    = 60
	DefaultInboundBurst   = 10
	maxInboundBody        = 4 << 20
	maxInboundRate        = 100_000
)

type Config struct {
	Upstreams map[string]Upstream `json:"upstreams"`
	// ToolUpstreams is the MCP gateway's table (P4b). Optional: a
	// P4a-only config still parses; an absent table relays nothing.
	ToolUpstreams map[string]ToolUpstream `json:"tool_upstreams,omitempty"`
	// InboundHooks is the inbound bridge's table (P7b). Optional: absent
	// means the inbound listener accepts nothing.
	InboundHooks map[string]InboundHook `json:"inbound_hooks,omitempty"`
}

func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return Parse(raw)
}

func Parse(raw []byte) (Config, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if len(c.Upstreams) == 0 {
		return Config{}, fmt.Errorf("config: no upstreams configured")
	}
	for name, u := range c.Upstreams {
		parsed, err := url.Parse(u.BaseURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return Config{}, fmt.Errorf("config: upstream %q: invalid base_url %q", name, u.BaseURL)
		}
		if u.Path == "" || strings.HasPrefix(u.Path, "/") {
			return Config{}, fmt.Errorf("config: upstream %q: path must be non-empty with no leading slash", name)
		}
		switch u.Classification {
		case ClassFree:
			if len(u.Prices) > 0 {
				return Config{}, fmt.Errorf("config: upstream %q: free classification cannot carry prices", name)
			}
		case ClassMetered:
		default:
			return Config{}, fmt.Errorf("config: upstream %q: classification must be %q or %q (explicit — never inferred)", name, ClassFree, ClassMetered)
		}
		for model, p := range u.Prices {
			// The $10k/1M-token ceiling is far beyond any real price and
			// keeps pricing.CostCents' int64 math overflow-free for any
			// token count an HTTP response can carry.
			const maxCentsPer1M = 1_000_000
			if p.InCentsPer1M < 0 || p.OutCentsPer1M < 0 ||
				p.InCentsPer1M > maxCentsPer1M || p.OutCentsPer1M > maxCentsPer1M {
				return Config{}, fmt.Errorf("config: upstream %q model %q: price out of range [0, %d]", name, model, maxCentsPer1M)
			}
		}
	}
	for name, t := range c.ToolUpstreams {
		parsed, err := url.Parse(t.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return Config{}, fmt.Errorf("config: tool upstream %q: invalid url %q (want absolute http(s))", name, t.URL)
		}
		// A credential header without a credential file (or the reverse
		// via a bare header name) is a misconfiguration that would fail
		// open in the confusing direction — reject it at load.
		if t.CredentialHeader != "" && t.CredentialFile == "" {
			return Config{}, fmt.Errorf("config: tool upstream %q: credential_header set without credential_file", name)
		}
		if !validHeaderName(t.CredentialHeader) {
			return Config{}, fmt.Errorf("config: tool upstream %q: invalid credential_header %q", name, t.CredentialHeader)
		}
	}
	for name, h := range c.InboundHooks {
		if !dnsLabel.MatchString(name) {
			return Config{}, fmt.Errorf("config: inbound hook %q: name must be a lowercase DNS label", name)
		}
		if !dnsLabel.MatchString(h.Credential) || !dnsLabel.MatchString(h.BudgetCredential) {
			return Config{}, fmt.Errorf("config: inbound hook %q: credential and budget_credential must be lowercase DNS labels", name)
		}
		if !dnsLabel.MatchString(h.Agent) || !dnsLabel.MatchString(h.AgentNamespace) {
			return Config{}, fmt.Errorf("config: inbound hook %q: agent and agent_namespace must be lowercase DNS labels", name)
		}
		if h.SlackChannelsFile != "" && h.Auth != AuthSlack {
			return Config{}, fmt.Errorf("config: inbound hook %q: slack_channels_file is meaningless with %s auth", name, h.Auth)
		}
		switch h.Auth {
		case AuthBearer:
			if h.SigningSecretFile != "" {
				return Config{}, fmt.Errorf("config: inbound hook %q: signing_secret_file is meaningless with bearer auth", name)
			}
		case AuthKaimahiHMAC, AuthSlack:
			// A signed hook with nothing to sign against would have to
			// fail closed on every request; refuse the config instead so
			// the mistake is loud at rollout, not silent at first event.
			if h.SigningSecretFile == "" {
				return Config{}, fmt.Errorf("config: inbound hook %q: %s auth requires signing_secret_file", name, h.Auth)
			}
			if h.Auth == AuthSlack && h.SlackChannelsFile == "" {
				return Config{}, fmt.Errorf("config: inbound hook %q: slack auth requires slack_channels_file (the channel(s) the hook may be triggered from)", name)
			}
		default:
			return Config{}, fmt.Errorf("config: inbound hook %q: auth must be %q, %q or %q", name, AuthBearer, AuthKaimahiHMAC, AuthSlack)
		}
		if h.MaxBodyBytes < 0 || h.MaxBodyBytes > maxInboundBody {
			return Config{}, fmt.Errorf("config: inbound hook %q: max_body_bytes out of range [0, %d]", name, maxInboundBody)
		}
		if h.RatePerMinute < 0 || h.RatePerMinute > maxInboundRate || h.Burst < 0 || h.Burst > maxInboundRate {
			return Config{}, fmt.Errorf("config: inbound hook %q: rate_per_minute/burst out of range [0, %d]", name, maxInboundRate)
		}
	}
	return c, nil
}

// dnsLabel is the shape shared by credential names (the admin surface's
// rule), Kubernetes object names, and hook names: interpolated into
// URLs, audit rows and grant subjects, so keep it to a plain identifier.
var dnsLabel = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// Bounded returns the hook with its defaults applied.
func (h InboundHook) Bounded() InboundHook {
	if h.MaxBodyBytes == 0 {
		h.MaxBodyBytes = DefaultInboundMaxBody
	}
	if h.RatePerMinute == 0 {
		h.RatePerMinute = DefaultInboundRate
	}
	if h.Burst == 0 {
		h.Burst = DefaultInboundBurst
	}
	return h
}

// validHeaderName accepts an empty name (the Authorization default) or a
// well-formed RFC 7230 field-name token — the full tchar set, so a legal
// header like "X-Api.Key" is not rejected for being unusual. The value is
// operator-committed, but a malformed name would be silently dropped by
// net/http rather than enforced, so reject it at load.
func validHeaderName(name string) bool {
	if name == "" {
		return true
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			strings.ContainsRune("!#$%&'*+-.^_`|~", r) {
			continue
		}
		return false
	}
	return true
}
