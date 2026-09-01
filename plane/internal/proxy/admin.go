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

	"github.com/gambtho/kaimahi/plane/internal/meter"
	"github.com/gambtho/kaimahi/plane/internal/store"
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
