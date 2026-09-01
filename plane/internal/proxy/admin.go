package proxy

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kaimahi-agents/kaimahi/plane/internal/meter"
	"github.com/kaimahi-agents/kaimahi/plane/internal/store"
)

// NewAdminMux serves the control surface: issue governed credentials, set
// budgets, read the ledger. It listens on a separate port that no Service
// for the data plane exposes and requires the admin bearer token from a
// Secret-mounted file (read per request so rotation needs no restart).
// Reaching it in the demo flow means kubectl port-forward — i.e. cluster
// credentials gate it before the token does.
func NewAdminMux(d Deps, adminTokenFile string) *http.ServeMux {
	h := &handler{d: d}
	mux := http.NewServeMux()
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			raw, err := os.ReadFile(adminTokenFile)
			want := strings.TrimSpace(string(raw))
			if err != nil || want == "" {
				// Fail closed: no readable admin token, no admin surface.
				slog.Error("admin: token file unreadable", "err", err)
				http.Error(w, "admin auth unavailable", http.StatusServiceUnavailable)
				return
			}
			got, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			// Compare digests so the check is constant-time and
			// length-independent.
			wantH, gotH := sha256.Sum256([]byte(want)), sha256.Sum256([]byte(got))
			if got == "" || subtle.ConstantTimeCompare(wantH[:], gotH[:]) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("POST /admin/credentials", auth(h.createCredential))
	mux.HandleFunc("PUT /admin/budgets", auth(h.setBudget))
	mux.HandleFunc("GET /admin/ledger", auth(h.ledger))
	mux.HandleFunc("PUT /admin/tool-allowlist", auth(h.setToolAllowlist))
	mux.HandleFunc("GET /admin/tool-allowlist", auth(h.toolAllowlist))
	mux.HandleFunc("GET /admin/tool-audit", auth(h.toolAudit))
	mux.HandleFunc("POST /admin/requests", auth(h.fileRequest))
	mux.HandleFunc("GET /admin/approvals", auth(h.listApprovals))
	mux.HandleFunc("POST /admin/approvals/{id}/approve", auth(h.approve))
	mux.HandleFunc("POST /admin/approvals/{id}/deny", auth(h.denyRequest))
	mux.HandleFunc("GET /admin/grants", auth(h.listGrants))
	mux.HandleFunc("GET /admin/approval-audit", auth(h.approvalAudit))
	mux.HandleFunc("GET /admin/inbound-audit", auth(h.inboundAudit))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

var credentialName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// createCredential mints a governed opaque token server-side and returns
// it exactly once; only its sha256 is stored. The caller pipes the token
// straight into the agent-side K8s Secret.
func (h *handler) createCredential(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil ||
		!credentialName.MatchString(req.Name) {
		http.Error(w, "body must be {\"name\": \"<lowercase-dns-label>\"}", http.StatusBadRequest)
		return
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		http.Error(w, "entropy unavailable", http.StatusInternalServerError)
		return
	}
	token := "kmh_" + hex.EncodeToString(buf)
	hash := sha256.Sum256([]byte(token))
	if err := h.d.Store.CreateCredential(r.Context(), req.Name, hash[:]); err != nil {
		if errors.Is(err, store.ErrExists) {
			http.Error(w, "credential name already exists", http.StatusConflict)
			return
		}
		slog.Error("admin: create credential", "name", req.Name, "err", err)
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"name": req.Name, "token": token})
}

func (h *handler) setBudget(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Credential string `json:"credential"`
		CapCents   *int64 `json:"cap_cents"`
		CapTokens  *int64 `json:"cap_tokens"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil ||
		req.Credential == "" ||
		(req.CapCents != nil && *req.CapCents < 0) ||
		(req.CapTokens != nil && *req.CapTokens < 0) {
		http.Error(w, "body must be {\"credential\": ..., \"cap_cents\": n|null, \"cap_tokens\": n|null}", http.StatusBadRequest)
		return
	}
	if err := h.d.Store.SetBudget(r.Context(), req.Credential, req.CapCents, req.CapTokens); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "no such credential", http.StatusNotFound)
			return
		}
		slog.Error("admin: set budget", "credential", req.Credential, "err", err)
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Tool names as MCP servers report them (kagent's are snake_case);
// bounded so an allowlist entry is always a plain identifier.
var toolName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// setToolAllowlist replaces a credential's whole tool allowlist (P4b).
// An empty list is valid and means nothing callable — fail closed is the
// default state, not an error.
func (h *handler) setToolAllowlist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Credential string `json:"credential"`
		// A pointer so an ABSENT tools field is a 400, never a silent
		// clear — clearing the allowlist takes an explicit [].
		Tools *[]string `json:"tools"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(&req); err != nil ||
		!credentialName.MatchString(req.Credential) || req.Tools == nil {
		http.Error(w, "body must be {\"credential\": ..., \"tools\": [...]} (tools required; [] clears)", http.StatusBadRequest)
		return
	}
	for _, t := range *req.Tools {
		if !toolName.MatchString(t) {
			http.Error(w, "invalid tool name "+strconv.Quote(t), http.StatusBadRequest)
			return
		}
	}
	if err := h.d.Store.SetToolAllowlist(r.Context(), req.Credential, *req.Tools); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "no such credential", http.StatusNotFound)
			return
		}
		slog.Error("admin: set tool allowlist", "credential", req.Credential, "err", err)
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) toolAllowlist(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("credential")
	if !credentialName.MatchString(name) {
		http.Error(w, "credential query parameter required", http.StatusBadRequest)
		return
	}
	tools, err := h.d.Store.ToolAllowlist(r.Context(), name)
	if err != nil {
		slog.Error("admin: tool allowlist read", "credential", name, "err", err)
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	if tools == nil {
		tools = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"credential": name, "tools": tools})
}

func (h *handler) toolAudit(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("credential")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	entries, err := h.d.Store.ToolAudit(r.Context(), name, limit)
	if err != nil {
		slog.Error("admin: tool audit read", "err", err)
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"entries": entries})
}

// inboundAudit reads the P7b inbound trail: every decision about an
// attributable event, and each admitted event's outcome.
func (h *handler) inboundAudit(w http.ResponseWriter, r *http.Request) {
	hook := r.URL.Query().Get("hook")
	if hook != "" && !credentialName.MatchString(hook) {
		http.Error(w, "hook must be a lowercase DNS label", http.StatusBadRequest)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	entries, err := h.d.Store.InboundAudit(r.Context(), hook, limit)
	if err != nil {
		slog.Error("admin: inbound audit read", "err", err)
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	if entries == nil {
		entries = []store.InboundAuditEntry{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"entries": entries})
}

func (h *handler) ledger(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("credential")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	entries, err := h.d.Store.Ledger(r.Context(), name, limit)
	if err != nil {
		slog.Error("admin: ledger read", "err", err)
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	out := map[string]any{"entries": entries}
	if name != "" {
		cents, tokens, err := h.d.Store.MonthUsage(r.Context(), name, meter.MonthStartUTC(time.Now()))
		if err != nil {
			slog.Error("admin: month usage", "credential", name, "err", err)
			http.Error(w, "store error", http.StatusInternalServerError)
			return
		}
		out["month_cents"] = cents
		out["month_tokens"] = tokens
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
