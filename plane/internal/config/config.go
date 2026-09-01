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
	"strings"

	"github.com/gambtho/kaimahi/plane/internal/pricing"
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

type Config struct {
	Upstreams map[string]Upstream `json:"upstreams"`
	// ToolUpstreams is the MCP gateway's table (P4b). Optional: a
	// P4a-only config still parses; an absent table relays nothing.
	ToolUpstreams map[string]ToolUpstream `json:"tool_upstreams,omitempty"`
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
	return c, nil
}

// validHeaderName accepts an empty name (the Authorization default) or a
// well-formed RFC 7230 token — the value is operator-committed, but a
// malformed name would be silently dropped by net/http rather than
// enforced, so reject it at load.
func validHeaderName(name string) bool {
	if name == "" {
		return true
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}
