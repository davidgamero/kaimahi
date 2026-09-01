package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kaimahi-agents/kaimahi/plane/internal/config"
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

// P5a: a tool upstream may carry its OWN credential (the Slack MCP
// server's SLACK_MCP_API_KEY), named — never valued — in the committed
// table, exactly like the LLM upstreams' credential_file.
func TestParseKeyedToolUpstreams(t *testing.T) {
	c, err := config.Parse([]byte(`{
	  "upstreams": {"o": {"base_url": "http://o", "path": "v1/chat/completions", "classification": "free"}},
	  "tool_upstreams": {"slack": {
	    "url": "http://kaimahi-slack-mcp.kaimahi:13080/mcp",
	    "credential_file": "/etc/kaimahi/upstream-creds/slack/mcp-api-key",
	    "credential_header": "Authorization"
	  }}
	}`))
	require.NoError(t, err)
	require.Equal(t, "/etc/kaimahi/upstream-creds/slack/mcp-api-key", c.ToolUpstreams["slack"].CredentialFile)
	require.Equal(t, "Authorization", c.ToolUpstreams["slack"].CredentialHeader)

	base := `{"upstreams": {"o": {"base_url": "http://o", "path": "p", "classification": "free"}}, "tool_upstreams": `
	for name, bad := range map[string]string{
		// A header with no file would silently forward bare — the
		// confusing direction of fail-open. Reject at load.
		"header without file": `{"t": {"url": "http://x/mcp", "credential_header": "Authorization"}}`,
		"malformed header":    `{"t": {"url": "http://x/mcp", "credential_file": "/f", "credential_header": "X Api Key"}}`,
		// Key material never belongs in the committed table.
		"inline credential": `{"t": {"url": "http://x/mcp", "credential": "xoxb-secret"}}`,
	} {
		_, err := config.Parse([]byte(base + bad + `}`))
		require.Error(t, err, name)
	}
}

const inboundBase = `"upstreams": {"ollama": {"base_url": "http://x", "path": "p", "classification": "free"}}`

func TestParseInboundHooks(t *testing.T) {
	c, err := config.Parse([]byte(`{` + inboundBase + `, "inbound_hooks": {
	  "demo": {"credential": "inbound-demo", "auth": "kaimahi-hmac", "signing_secret_file": "/etc/kaimahi/inbound/demo",
	           "agent_namespace": "kagent", "agent": "hello-world", "budget_credential": "hello-world"},
	  "plain": {"credential": "inbound-plain", "auth": "bearer",
	            "agent_namespace": "kagent", "agent": "hello-world", "budget_credential": "hello-world",
	            "max_body_bytes": 1024, "rate_per_minute": 5, "burst": 2}
	}}`))
	require.NoError(t, err)
	require.Len(t, c.InboundHooks, 2)
	demo := c.InboundHooks["demo"].Bounded()
	require.Equal(t, int64(config.DefaultInboundMaxBody), demo.MaxBodyBytes)
	require.Equal(t, config.DefaultInboundRate, demo.RatePerMinute)
	require.Equal(t, config.DefaultInboundBurst, demo.Burst)
	plain := c.InboundHooks["plain"].Bounded()
	require.Equal(t, int64(1024), plain.MaxBodyBytes)
	require.Equal(t, 5, plain.RatePerMinute)
	require.Equal(t, 2, plain.Burst)
}

func TestParseInboundHooksRejects(t *testing.T) {
	hook := func(fields string) string {
		return `{` + inboundBase + `, "inbound_hooks": {"demo": {` + fields + `}}}`
	}
	good := `"credential": "c", "auth": "bearer", "agent_namespace": "kagent", "agent": "a", "budget_credential": "b"`
	cases := map[string]string{
		"unknown auth":              hook(`"credential": "c", "auth": "basic", "agent_namespace": "kagent", "agent": "a", "budget_credential": "b"`),
		"hmac without secret file":  hook(`"credential": "c", "auth": "kaimahi-hmac", "agent_namespace": "kagent", "agent": "a", "budget_credential": "b"`),
		"slack without secret file": hook(`"credential": "c", "auth": "slack", "agent_namespace": "kagent", "agent": "a", "budget_credential": "b"`),
		"bearer with secret file":   hook(good + `, "signing_secret_file": "/x"`),
		"missing credential":        hook(`"auth": "bearer", "agent_namespace": "kagent", "agent": "a", "budget_credential": "b"`),
		"missing agent":             hook(`"credential": "c", "auth": "bearer", "agent_namespace": "kagent", "budget_credential": "b"`),
		"uppercase agent":           hook(`"credential": "c", "auth": "bearer", "agent_namespace": "kagent", "agent": "Hello", "budget_credential": "b"`),
		"body bound too large":      hook(good + `, "max_body_bytes": 99999999`),
		"negative rate":             hook(good + `, "rate_per_minute": -1`),
		"unknown field (typo)":      hook(good + `, "signing_secret": "x"`),
		"bad hook name":             `{` + inboundBase + `, "inbound_hooks": {"Demo Hook": {` + good + `}}}`,
	}
	for name, raw := range cases {
		_, err := config.Parse([]byte(raw))
		require.Error(t, err, name)
	}
}
