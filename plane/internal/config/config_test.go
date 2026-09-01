package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gambtho/kaimahi/plane/internal/config"
)

func TestParseValid(t *testing.T) {
	c, err := config.Parse([]byte(`{
	  "upstreams": {
	    "ollama": {"base_url": "http://ollama.ollama.svc:11434", "path": "v1/chat/completions", "classification": "free"},
	    "copilot": {"base_url": "https://api.githubcopilot.com", "path": "chat/completions",
	                "classification": "metered", "credential_file": "/etc/x/token",
	                "prices": {"gpt-5-mini": {"in_cents_per_1m": 25, "out_cents_per_1m": 200}}}
	  }
	}`))
	require.NoError(t, err)
	require.Len(t, c.Upstreams, 2)
	require.Equal(t, config.ClassFree, c.Upstreams["ollama"].Classification)
	require.Equal(t, 25, c.Upstreams["copilot"].Prices["gpt-5-mini"].InCentsPer1M)
}

func TestParseRejects(t *testing.T) {
	cases := map[string]string{
		"no upstreams":         `{"upstreams": {}}`,
		"missing class":        `{"upstreams": {"a": {"base_url": "http://x", "path": "p"}}}`,
		"inferred class":       `{"upstreams": {"a": {"base_url": "http://x", "path": "p", "classification": "local"}}}`,
		"free with prices":     `{"upstreams": {"a": {"base_url": "http://x", "path": "p", "classification": "free", "prices": {"m": {"in_cents_per_1m": 1, "out_cents_per_1m": 1}}}}}`,
		"bad base_url":         `{"upstreams": {"a": {"base_url": "not a url", "path": "p", "classification": "free"}}}`,
		"leading-slash path":   `{"upstreams": {"a": {"base_url": "http://x", "path": "/p", "classification": "free"}}}`,
		"empty path":           `{"upstreams": {"a": {"base_url": "http://x", "path": "", "classification": "free"}}}`,
		"negative price":       `{"upstreams": {"a": {"base_url": "http://x", "path": "p", "classification": "metered", "prices": {"m": {"in_cents_per_1m": -1, "out_cents_per_1m": 1}}}}}`,
		"unknown field (typo)": `{"upstreams": {"a": {"base_url": "http://x", "path": "p", "classification": "free", "credental_file": "x"}}}`,
	}
	for name, raw := range cases {
		_, err := config.Parse([]byte(raw))
		require.Error(t, err, name)
	}
}

func TestParseToolUpstreams(t *testing.T) {
	c, err := config.Parse([]byte(`{
	  "upstreams": {"o": {"base_url": "http://o", "path": "v1/chat/completions", "classification": "free"}},
	  "tool_upstreams": {"kagent-tools": {"url": "http://kagent-tools.kagent:8084/mcp"}}
	}`))
	require.NoError(t, err)
	require.Equal(t, "http://kagent-tools.kagent:8084/mcp", c.ToolUpstreams["kagent-tools"].URL)

	// Optional: a P4a-only config still parses with no tool upstreams.
	c, err = config.Parse([]byte(`{"upstreams": {"o": {"base_url": "http://o", "path": "p", "classification": "free"}}}`))
	require.NoError(t, err)
	require.Empty(t, c.ToolUpstreams)

	base := `{"upstreams": {"o": {"base_url": "http://o", "path": "p", "classification": "free"}}, "tool_upstreams": `
	for name, bad := range map[string]string{
		"empty url":     `{"t": {"url": ""}}`,
		"relative url":  `{"t": {"url": "not-a-url"}}`,
		"non-http":      `{"t": {"url": "ftp://x/mcp"}}`,
		"unknown field": `{"t": {"url": "http://x/mcp", "extra": true}}`,
	} {
		_, err := config.Parse([]byte(base + bad + `}`))
		require.Error(t, err, name)
	}
}
