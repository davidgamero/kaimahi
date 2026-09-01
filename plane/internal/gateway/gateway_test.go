package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gambtho/kaimahi/plane/internal/config"
	"github.com/gambtho/kaimahi/plane/internal/store"
)

const goodToken = "kmh_test_token"

type fakeStore struct {
	allow      []string
	allowErr   error
	auditErr   error
	credErr    error
	audits     []store.ToolAuditEntry
	credential store.Credential
}

func (f *fakeStore) CredentialByTokenHash(_ context.Context, hash []byte) (store.Credential, error) {
	if f.credErr != nil {
		return store.Credential{}, f.credErr
	}
	want := sha256.Sum256([]byte(goodToken))
	if string(hash) != string(want[:]) {
		return store.Credential{}, store.ErrNotFound
	}
	return f.credential, nil
}

func (f *fakeStore) ToolAllowlist(_ context.Context, _ string) ([]string, error) {
	return f.allow, f.allowErr
}

func (f *fakeStore) RecordToolAudit(_ context.Context, e store.ToolAuditEntry) error {
	if f.auditErr != nil {
		return f.auditErr
	}
	f.audits = append(f.audits, e)
	return nil
}

func rpc(t *testing.T, method string, params any) []byte {
	t.Helper()
	m := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		m["params"] = params
	}
	raw, err := json.Marshal(m)
	require.NoError(t, err)
	return raw
}

func newGateway(t *testing.T, fs *fakeStore, upstream *httptest.Server) http.Handler {
	t.Helper()
	ups := map[string]config.ToolUpstream{}
	if upstream != nil {
		ups["kagent-tools"] = config.ToolUpstream{URL: upstream.URL + "/mcp"}
	}
	return NewMux(Deps{Store: fs, Upstreams: ups})
}

func post(h http.Handler, token string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/upstream/kagent-tools/mcp", strings.NewReader(string(body)))
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuthRequired(t *testing.T) {
	upstreamHit := false
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHit = true
	}))
	defer up.Close()
	fs := &fakeStore{credential: store.Credential{Name: "hello-tools"}}
	h := newGateway(t, fs, up)

	assert.Equal(t, http.StatusUnauthorized, post(h, "", rpc(t, "tools/list", nil)).Code)
	assert.Equal(t, http.StatusUnauthorized, post(h, "kmh_wrong", rpc(t, "tools/list", nil)).Code)
	// Bearer prefix optional: headersFrom sends the Secret value verbatim.
	fs.allow = []string{"a"}
	assert.Equal(t, http.StatusOK, post(h, goodToken, rpc(t, "ping", nil)).Code)
	assert.Equal(t, http.StatusOK, post(h, "Bearer "+goodToken, rpc(t, "ping", nil)).Code)
	assert.False(t, upstreamHit, "unauthenticated or local requests must not reach the upstream")
	assert.Empty(t, fs.audits, "auth failures have no credential to audit")
}

func TestCredentialStoreFailureClosed(t *testing.T) {
	fs := &fakeStore{credErr: errors.New("pg down")}
	h := newGateway(t, fs, nil)
	assert.Equal(t, http.StatusServiceUnavailable, post(h, goodToken, rpc(t, "tools/list", nil)).Code)
}

func TestUnknownUpstreamDenied(t *testing.T) {
	fs := &fakeStore{credential: store.Credential{Name: "hello-tools"}}
	h := NewMux(Deps{Store: fs, Upstreams: map[string]config.ToolUpstream{}})
	rec := post(h, goodToken, rpc(t, "tools/call", map[string]any{"name": "x"}))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	require.Len(t, fs.audits, 1)
	assert.Equal(t, "denied", fs.audits[0].Decision)
}

func TestMethodScopeDenied(t *testing.T) {
	upstreamHit := false
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHit = true
	}))
	defer up.Close()
	fs := &fakeStore{credential: store.Credential{Name: "hello-tools"}}
	h := newGateway(t, fs, up)

	for _, method := range []string{"resources/list", "prompts/list", "completion/complete", "logging/setLevel"} {
		rec := post(h, goodToken, rpc(t, method, nil))
		assert.Equal(t, http.StatusOK, rec.Code, method)
		assert.Contains(t, rec.Body.String(), "tools only", method)
		assert.Contains(t, rec.Body.String(), `"error"`, method)
	}
	assert.False(t, upstreamHit, "denied methods must never be relayed")
	require.Len(t, fs.audits, 4)
	for _, e := range fs.audits {
		assert.Equal(t, "denied", e.Decision)
		assert.Equal(t, http.StatusForbidden, e.Status)
	}
}

func TestBatchRejected(t *testing.T) {
	fs := &fakeStore{credential: store.Credential{Name: "hello-tools"}}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer up.Close()
	h := newGateway(t, fs, up)
	rec := post(h, goodToken, []byte(`[{"jsonrpc":"2.0","id":1,"method":"tools/list"}]`))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestLifecycleRelayed(t *testing.T) {
	var sawMethods []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&m)
		sawMethods = append(sawMethods, m.Method)
		assert.Empty(t, r.Header.Get("Authorization"), "the kmh_ token must never reach the tool server")
		w.Header().Set("Mcp-Session-Id", "s-1")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"fake"}}}`))
	}))
	defer up.Close()
	fs := &fakeStore{credential: store.Credential{Name: "hello-tools"}}
	h := newGateway(t, fs, up)

	rec := post(h, goodToken, rpc(t, "initialize", map[string]any{"protocolVersion": "2025-03-26"}))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "s-1", rec.Header().Get("Mcp-Session-Id"), "session header must relay back")

	rec = post(h, goodToken, []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"initialize", "notifications/initialized"}, sawMethods)
	assert.Empty(t, fs.audits, "successful lifecycle relays are not audited")
}

func TestToolsListProjected(t *testing.T) {
	listing := `{"jsonrpc":"2.0","id":1,"result":{"tools":[` +
		`{"name":"k8s_get_resources","description":"get"},` +
		`{"name":"k8s_describe_resource","description":"describe"},` +
		`{"name":"k8s_get_events","description":"events"}]}}`
	for name, respond := range map[string]http.HandlerFunc{
		"json": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(listing))
		},
		"sse": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: message\ndata: " + listing + "\n\n"))
		},
		// The space after "data:" is optional per the SSE format.
		"sse-no-space": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: message\ndata:" + listing + "\n\n"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			up := httptest.NewServer(respond)
			defer up.Close()
			fs := &fakeStore{credential: store.Credential{Name: "hello-tools"},
				allow: []string{"k8s_get_resources"}}
			h := newGateway(t, fs, up)
			rec := post(h, goodToken, rpc(t, "tools/list", nil))
			require.Equal(t, http.StatusOK, rec.Code)
			var out struct {
				Result struct {
					Tools []struct {
						Name string `json:"name"`
					} `json:"tools"`
				} `json:"result"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
			require.Len(t, out.Result.Tools, 1, "projection must hide unallowed tools")
			assert.Equal(t, "k8s_get_resources", out.Result.Tools[0].Name)
		})
	}
}

func TestToolsListEmptyAllowlistProjectsNothing(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"k8s_get_resources"}]}}`))
	}))
	defer up.Close()
	fs := &fakeStore{credential: store.Credential{Name: "hello-tools"}} // nil allowlist
	h := newGateway(t, fs, up)
	rec := post(h, goodToken, rpc(t, "tools/list", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"tools":[]`)
}

func TestToolsListUnparseableFailsClosed(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json at all`))
	}))
	defer up.Close()
	fs := &fakeStore{credential: store.Credential{Name: "hello-tools"}, allow: []string{"a"}}
	h := newGateway(t, fs, up)
	assert.Equal(t, http.StatusBadGateway, post(h, goodToken, rpc(t, "tools/list", nil)).Code)
}

func TestToolsCallAllowedRelayedAndAudited(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"three pods\"}]}}\n\n"))
	}))
	defer up.Close()
	fs := &fakeStore{credential: store.Credential{Name: "hello-tools"},
		allow: []string{"k8s_get_resources"}}
	h := newGateway(t, fs, up)
	rec := post(h, goodToken, rpc(t, "tools/call",
		map[string]any{"name": "k8s_get_resources", "arguments": map[string]any{"kind": "pods"}}))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "three pods", "tool responses relay byte-faithfully")
	require.Len(t, fs.audits, 1)
	e := fs.audits[0]
	assert.Equal(t, "allowed", e.Decision)
	assert.Equal(t, "k8s_get_resources", e.Tool)
	assert.Equal(t, "tools/call", e.Method)
	assert.Equal(t, http.StatusOK, e.Status)
	assert.Equal(t, "hello-tools", e.CredentialName)
}

func TestToolsCallDeniedOutsideAllowlist(t *testing.T) {
	upstreamHit := false
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHit = true
	}))
	defer up.Close()
	fs := &fakeStore{credential: store.Credential{Name: "hello-tools"},
		allow: []string{"k8s_get_resources"}}
	h := newGateway(t, fs, up)
	rec := post(h, goodToken, rpc(t, "tools/call", map[string]any{"name": "k8s_describe_resource"}))
	require.Equal(t, http.StatusOK, rec.Code, "denial travels as a JSON-RPC error")
	assert.Contains(t, rec.Body.String(), "not permitted")
	assert.False(t, upstreamHit, "denied calls must never reach the upstream")
	require.Len(t, fs.audits, 1)
	assert.Equal(t, "denied", fs.audits[0].Decision)
	assert.Equal(t, "k8s_describe_resource", fs.audits[0].Tool)
	assert.Equal(t, http.StatusForbidden, fs.audits[0].Status)
}

func TestToolsCallEmptyAllowlistDenied(t *testing.T) {
	fs := &fakeStore{credential: store.Credential{Name: "hello-tools"}} // nil allowlist
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream must not be reached")
	}))
	defer up.Close()
	h := newGateway(t, fs, up)
	rec := post(h, goodToken, rpc(t, "tools/call", map[string]any{"name": "k8s_get_resources"}))
	assert.Contains(t, rec.Body.String(), "not permitted")
}

func TestAllowlistReadFailureClosed(t *testing.T) {
	fs := &fakeStore{credential: store.Credential{Name: "hello-tools"},
		allowErr: errors.New("pg down")}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream must not be reached")
	}))
	defer up.Close()
	h := newGateway(t, fs, up)
	assert.Equal(t, http.StatusServiceUnavailable,
		post(h, goodToken, rpc(t, "tools/call", map[string]any{"name": "x"})).Code)
	assert.Equal(t, http.StatusServiceUnavailable,
		post(h, goodToken, rpc(t, "tools/list", nil)).Code)
}

func TestAuditDegradationFailsClosedAndRecovers(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`))
	}))
	defer up.Close()
	fs := &fakeStore{credential: store.Credential{Name: "hello-tools"},
		allow: []string{"k8s_get_resources"}, auditErr: errors.New("pg down")}
	h := newGateway(t, fs, up)
	call := rpc(t, "tools/call", map[string]any{"name": "k8s_get_resources"})

	// First call relays (the response was already committed when the audit
	// write failed) but trips the breaker...
	assert.Equal(t, http.StatusOK, post(h, goodToken, call).Code)
	// ...so the next request is denied before any upstream contact.
	assert.Equal(t, http.StatusServiceUnavailable, post(h, goodToken, call).Code)

	// The denial's own audit attempt is the recovery probe: once writes
	// succeed again, traffic resumes.
	fs.auditErr = nil
	assert.Equal(t, http.StatusServiceUnavailable, post(h, goodToken, call).Code,
		"the request that heals the breaker is itself still denied")
	assert.Equal(t, http.StatusOK, post(h, goodToken, call).Code)
}

func TestDuplicateKeysCannotSmuggle(t *testing.T) {
	var sawUpstream []byte
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawUpstream, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer up.Close()
	fs := &fakeStore{credential: store.Credential{Name: "hello-tools"},
		allow: []string{"k8s_get_resources"}}
	h := newGateway(t, fs, up)

	// A duplicated "method" key: the gateway decides on the LAST value
	// (initialize) and must forward bytes carrying only that value, so a
	// first-key-wins upstream cannot execute the smuggled tools/call.
	rec := post(h, goodToken,
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"k8s_delete_resource"},"method":"initialize"}`))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, string(sawUpstream), "tools/call")
	assert.Contains(t, string(sawUpstream), `"method":"initialize"`)

	// A duplicated tool "name" in params: checked last-wins as the
	// allowed tool, so the forwarded params must carry only that name.
	rec = post(h, goodToken,
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"k8s_delete_resource","name":"k8s_get_resources"}}`))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, string(sawUpstream), "k8s_delete_resource")
	assert.Contains(t, string(sawUpstream), "k8s_get_resources")
}

func TestUpstreamRedirectRefused(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://evil.example/mcp", http.StatusTemporaryRedirect)
	}))
	defer up.Close()
	fs := &fakeStore{credential: store.Credential{Name: "hello-tools"},
		allow: []string{"k8s_get_resources"}}
	h := newGateway(t, fs, up)

	for _, body := range [][]byte{
		rpc(t, "tools/call", map[string]any{"name": "k8s_get_resources"}),
		rpc(t, "tools/list", nil),
		rpc(t, "initialize", nil),
	} {
		rec := post(h, goodToken, body)
		assert.Equal(t, http.StatusBadGateway, rec.Code)
		assert.Empty(t, rec.Header().Get("Location"), "Location must not leak through")
	}
}

func TestGetAnswers405(t *testing.T) {
	fs := &fakeStore{credential: store.Credential{Name: "hello-tools"}}
	h := newGateway(t, fs, nil)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/upstream/kagent-tools/mcp", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestDeleteRelaysSessionTermination(t *testing.T) {
	sawDelete := false
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawDelete = r.Method == http.MethodDelete
		assert.Equal(t, "s-1", r.Header.Get("Mcp-Session-Id"))
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()
	fs := &fakeStore{credential: store.Credential{Name: "hello-tools"}}
	h := newGateway(t, fs, up)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/upstream/kagent-tools/mcp", nil)
	req.Header.Set("Authorization", goodToken)
	req.Header.Set("Mcp-Session-Id", "s-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, sawDelete)
}
