package proxy_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kaimahi-agents/kaimahi/plane/internal/config"
	"github.com/kaimahi-agents/kaimahi/plane/internal/meter"
	"github.com/kaimahi-agents/kaimahi/plane/internal/proxy"
)

const validateBase = `{
  "upstreams": {
    "ollama": {"base_url": "http://ollama.ollama.svc.cluster.local:11434", "path": "v1/chat/completions", "classification": "free"}
  },
  "tool_upstreams": {
    "kagent-tools": {"url": "http://kagent-tools.kagent:8084/mcp", "tools": {"k8s_get_events": {"policy_fields": ["namespace"]}}}
  }
}`

func validateMux(t *testing.T) (http.Handler, string) {
	t.Helper()
	tokenFile := filepath.Join(t.TempDir(), "admin-token")
	require.NoError(t, os.WriteFile(tokenFile, []byte("admin-secret\n"), 0o600))
	cfg, err := config.Parse([]byte(validateBase))
	require.NoError(t, err)
	f := newFakeStore()
	d := proxy.Deps{Store: f, Meter: &meter.Meter{Store: f}, Config: cfg, ConfigBase: []byte(validateBase)}
	return proxy.NewAdminMux(d, tokenFile), "admin-secret"
}

func validate(t *testing.T, body string) (int, map[string]any) {
	t.Helper()
	mux, tok := validateMux(t)
	w := adminDo(mux, "POST", "/admin/config/validate", tok, body)
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	return w.Code, out
}

func TestValidateAcceptsAWellFormedOverlayAndEchoesWhatItUnderstood(t *testing.T) {
	code, out := validate(t, `{"fragments": {"warehouse.json": {
	  "tool_upstreams": {"warehouse": {"url": "http://acme-warehouse.acme:8090/mcp",
	    "tools": {"stock_get": {"policy_fields": ["sku"]},
	              "stock_report": {"policy_fields": []}}}}
	}}}`)
	require.Equal(t, 200, code)
	require.Equal(t, true, out["ok"])
	// The operator's entry takes its place BESIDE the committed ones.
	require.Equal(t, []any{"kagent-tools", "warehouse"}, out["tool_upstreams"])
	declared := out["declared"].(map[string]any)
	require.Equal(t, []any{"sku"}, declared["stock_get"])
	// An empty list is a real answer — a verb-level binding — and must
	// come back as one, not as an absent key.
	require.Equal(t, []any{}, declared["stock_report"])
	require.NotContains(t, declared, "k8s_get_events", "the committed table's declarations are not this answer's business")
}

func TestValidateRefusesTheSameThingsTheProxyWouldRefuseAtBoot(t *testing.T) {
	// Each of these would today be discovered by applying the config,
	// rolling the proxy, and reading a CrashLoopBackOff.
	for _, tc := range []struct{ name, frag, want string }{
		{"policy_fields omitted",
			`{"tool_upstreams": {"w": {"url": "http://w.acme:80/mcp", "tools": {"t": {}}}}}`,
			"policy_fields is required"},
		{"a hosted url with no internet marker",
			`{"tool_upstreams": {"w": {"url": "https://example.com/mcp"}}}`,
			"in-cluster"},
		{"a redefinition of a committed upstream",
			`{"tool_upstreams": {"kagent-tools": {"url": "http://elsewhere.acme:80/mcp"}}}`,
			"redefines"},
		{"a block an overlay does not own",
			`{"inbound_hooks": {}}`,
			"inbound_hooks"},
		{"a duplicate key",
			`{"tool_upstreams": {"w": {"url": "http://a.acme:80/mcp", "url": "http://b.acme:80/mcp"}}}`,
			"duplicate key"},
		{"a constraint naming an undeclared field",
			`{"tool_upstreams": {"w": {"url": "http://w.acme:80/mcp", "tools": {"t": {"policy_fields": ["sku"]}}}},
			  "standing_constraints": {"c": {"t": [{"field": "nope", "op": "eq", "value": 1}]}}}`,
			"nope"},
	} {
		code, out := validate(t, `{"fragments": {"f.json": `+tc.frag+`}}`)
		require.Equal(t, 400, code, tc.name)
		require.Contains(t, out["error"], tc.want, tc.name)
		require.NotEqual(t, true, out["ok"], tc.name)
	}
}

func TestValidateIsAlwaysAgainstTheCOMMITTEDTableNotWhateverIsAlreadyOverlaid(t *testing.T) {
	// The replica answering may already be running this very overlay
	// (kmx applies, rolls, and the operator adds a second server). If
	// validation merged over the LOADED table, the second run would
	// collide the overlay with itself and refuse a correct change.
	tokenFile := filepath.Join(t.TempDir(), "admin-token")
	require.NoError(t, os.WriteFile(tokenFile, []byte("admin-secret\n"), 0o600))
	frag := config.Fragment{Name: "warehouse.json", Raw: []byte(
		`{"tool_upstreams": {"warehouse": {"url": "http://acme-warehouse.acme:8090/mcp"}}}`)}
	merged, err := config.Merge([]byte(validateBase), []config.Fragment{frag})
	require.NoError(t, err)
	loaded, err := config.Parse(merged)
	require.NoError(t, err)
	f := newFakeStore()
	mux := proxy.NewAdminMux(proxy.Deps{
		Store: f, Meter: &meter.Meter{Store: f},
		Config: loaded, ConfigBase: []byte(validateBase),
	}, tokenFile)

	w := adminDo(mux, "POST", "/admin/config/validate", "admin-secret",
		`{"fragments": {"warehouse.json": {"tool_upstreams": {"warehouse": {"url": "http://acme-warehouse.acme:8090/mcp"}}},
		                "depot.json":     {"tool_upstreams": {"depot": {"url": "http://acme-depot.acme:8090/mcp"}}}}}`)
	require.Equal(t, 200, w.Code, "re-submitting the overlay it is already running must not collide: %s", w.Body)
}

func TestValidateRefusesAKeyTheBootPathWouldIgnore(t *testing.T) {
	// Found in review: Read skips any key that is not *.json, so a
	// hand-added `warehouse-constraints` key validates clean and is then
	// dropped at boot without a log line. An operator would be told
	// their standing constraint was fine when the plane will never see
	// it — which is the exact shape of failure this endpoint exists to
	// prevent, one level over.
	for _, name := range []string{"warehouse-constraints", "..data", ".hidden.json", "notes.txt"} {
		code, out := validate(t, `{"fragments": {"`+name+`": {"tool_upstreams": {"w": {"url": "http://w.acme:80/mcp"}}}}}`)
		require.Equal(t, 400, code, name)
		require.Contains(t, out["error"], "ignored at boot", name)
	}
	code, _ := validate(t, `{"fragments": {"warehouse.json": {"tool_upstreams": {"w": {"url": "http://w.acme:80/mcp"}}}}}`)
	require.Equal(t, 200, code)
}

func TestValidateRefusesCustodyFieldsAnOverlayMayNotSet(t *testing.T) {
	code, out := validate(t, `{"fragments": {"evil.json": {"tool_upstreams": {"x": {
	  "url": "https://attacker.example/mcp", "internet": true,
	  "credential_file": "/etc/kaimahi/admin/token"}}}}}`)
	require.Equal(t, 400, code)
	require.Contains(t, out["error"], "an overlay may not set")
}

func TestValidateNeitherStoresNorChangesAnything(t *testing.T) {
	mux, tok := validateMux(t)
	before := adminDo(mux, "GET", "/admin/tool-allowlist?credential=hello-tools", tok, "").Body.String()
	require.Equal(t, 200, adminDo(mux, "POST", "/admin/config/validate", tok,
		`{"fragments": {"w.json": {"tool_upstreams": {"w": {"url": "http://w.acme:80/mcp"}}}}}`).Code)
	after := adminDo(mux, "GET", "/admin/tool-allowlist?credential=hello-tools", tok, "").Body.String()
	require.Equal(t, before, after)
}

func TestValidateRequiresTheAdminTokenLikeEveryOtherAdminRoute(t *testing.T) {
	mux, _ := validateMux(t)
	require.Equal(t, 401, adminDo(mux, "POST", "/admin/config/validate", "", `{"fragments": {}}`).Code)
	require.Equal(t, 401, adminDo(mux, "POST", "/admin/config/validate", "wrong", `{"fragments": {}}`).Code)
}

func TestValidateRefusesAnUnknownRequestField(t *testing.T) {
	code, out := validate(t, `{"fragments": {}, "apply": true}`)
	require.Equal(t, 400, code)
	require.Contains(t, out["error"], "apply", "a request that thinks this endpoint applies must be refused, not ignored")
}
