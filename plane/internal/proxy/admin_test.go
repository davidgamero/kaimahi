package proxy_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gambtho/kaimahi/plane/internal/config"
	"github.com/gambtho/kaimahi/plane/internal/meter"
	"github.com/gambtho/kaimahi/plane/internal/proxy"
	"github.com/gambtho/kaimahi/plane/internal/store"
)

func adminMux(t *testing.T, f *fakeStore) (http.Handler, string) {
	t.Helper()
	tokenFile := filepath.Join(t.TempDir(), "admin-token")
	require.NoError(t, os.WriteFile(tokenFile, []byte("admin-secret\n"), 0o600))
	d := proxy.Deps{Store: f, Meter: &meter.Meter{Usage: f}, Config: config.Config{}}
	return proxy.NewAdminMux(d, tokenFile), "admin-secret"
}

func adminDo(mux http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func TestAdminRequiresToken(t *testing.T) {
	mux, _ := adminMux(t, newFakeStore())
	require.Equal(t, 401, adminDo(mux, "POST", "/admin/credentials", "", `{"name": "a"}`).Code)
	require.Equal(t, 401, adminDo(mux, "POST", "/admin/credentials", "wrong", `{"name": "a"}`).Code)
}

func TestAdminFailsClosedWithoutTokenFile(t *testing.T) {
	d := proxy.Deps{Store: newFakeStore(), Meter: &meter.Meter{Usage: newFakeStore()}}
	mux := proxy.NewAdminMux(d, "/nonexistent/token")
	require.Equal(t, 503, adminDo(mux, "POST", "/admin/credentials", "any", `{"name": "a"}`).Code)
}

func TestIssueCredentialRoundTrip(t *testing.T) {
	f := newFakeStore()
	mux, tok := adminMux(t, f)
	w := adminDo(mux, "POST", "/admin/credentials", tok, `{"name": "hello-world"}`)
	require.Equal(t, 201, w.Code)
	var resp struct{ Name, Token string }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "hello-world", resp.Name)
	require.True(t, strings.HasPrefix(resp.Token, "kmh_"))
	require.Len(t, resp.Token, 4+64)

	// The issued token authenticates on the data plane.
	dataMux := proxy.NewDataMux(proxy.Deps{Store: f, Meter: &meter.Meter{Usage: f},
		Config: config.Config{Upstreams: map[string]config.Upstream{}}})
	require.Equal(t, 403, doChat(t, dataMux, resp.Token, "/upstream/x/y", chatBody).Code,
		"known token reaches authorization (403 unknown upstream), not 401")

	// Duplicate name conflicts.
	require.Equal(t, 409, adminDo(mux, "POST", "/admin/credentials", tok, `{"name": "hello-world"}`).Code)
}

func TestIssueCredentialRejectsBadNames(t *testing.T) {
	mux, tok := adminMux(t, newFakeStore())
	for _, body := range []string{`{}`, `{"name": "UPPER"}`, `{"name": "has space"}`, `not json`} {
		require.Equal(t, 400, adminDo(mux, "POST", "/admin/credentials", tok, body).Code, body)
	}
}

func TestSetBudgetAndLedger(t *testing.T) {
	f := newFakeStore()
	f.addToken("tok", store.Credential{Name: "hello"})
	f.ledger = append(f.ledger, store.LedgerEntry{CredentialName: "hello", Upstream: "ollama",
		Model: "m", InputTokens: 1, OutputTokens: 2, CostSource: "free", Status: 200})
	mux, tok := adminMux(t, f)

	require.Equal(t, 204, adminDo(mux, "PUT", "/admin/budgets", tok,
		`{"credential": "hello", "cap_tokens": 5}`).Code)
	require.Equal(t, 404, adminDo(mux, "PUT", "/admin/budgets", tok,
		`{"credential": "nope", "cap_tokens": 5}`).Code)
	require.Equal(t, 400, adminDo(mux, "PUT", "/admin/budgets", tok,
		`{"credential": "hello", "cap_tokens": -1}`).Code)

	w := adminDo(mux, "GET", "/admin/ledger?credential=hello", tok, "")
	require.Equal(t, 200, w.Code)
	var resp struct {
		Entries     []store.LedgerEntry `json:"entries"`
		MonthCents  *int64              `json:"month_cents"`
		MonthTokens *int64              `json:"month_tokens"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Entries, 1)
	require.NotNil(t, resp.MonthCents)
	require.NotNil(t, resp.MonthTokens)
}
