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

type Config struct {
	Upstreams map[string]Upstream `json:"upstreams"`
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
	return c, nil
}
