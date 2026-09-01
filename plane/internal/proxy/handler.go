package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gambtho/kaimahi/plane/internal/config"
	"github.com/gambtho/kaimahi/plane/internal/meter"
	"github.com/gambtho/kaimahi/plane/internal/pricing"
	"github.com/gambtho/kaimahi/plane/internal/store"
)

const (
	maxRequestBody  = 10 << 20 // an LLM chat request; far beyond any sane prompt
	maxBufferedResp = 50 << 20
)

type handler struct {
	d Deps
}

// NewDataMux serves the governed data plane: the surface kagent's OpenAI
// client talks to. One route — POST /upstream/{name}/{path...} — plus a
// health probe. Everything else 404s with no upstream contact.
func NewDataMux(d Deps) *http.ServeMux {
	h := &handler{d: d}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /upstream/{name}/{path...}", h.forward)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

// record appends a ledger row on a cancel-free context: a client
// disconnect must not drop the record of a call the proxy already made
// (ported audit rule). Bounded so a stalled pool cannot hang the response.
func (h *handler) record(r *http.Request, e store.LedgerEntry) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	if err := h.d.Store.RecordLedger(ctx, e); err != nil {
		slog.Error("proxy: ledger append failed", "credential", e.CredentialName,
			"upstream", e.Upstream, "status", e.Status, "err", err)
	}
}

// deny is the single exit for every pre-forward refusal: the denial is
// ledgered (zero usage, cost_source=denied), then answered.
func (h *handler) deny(w http.ResponseWriter, r *http.Request, cred store.Credential,
	upstream, model string, status int, msg string) {
	h.record(r, store.LedgerEntry{
		CredentialName: cred.Name,
		Upstream:       upstream,
		Model:          model,
		CostSource:     "denied",
		Status:         status,
	})
	http.Error(w, msg, status)
}

func (h *handler) forward(w http.ResponseWriter, r *http.Request) {
	// Authenticate the Kaimahi-issued token. It travels where the OpenAI
	// API key would (the SDK can send exactly one credential header) and
	// is known to the store only by hash.
	bearer, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if bearer == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	hash := sha256.Sum256([]byte(bearer))
	cred, err := h.d.Store.CredentialByTokenHash(r.Context(), hash[:])
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err != nil {
		// Fail closed, but distinguishably: no credential visibility, no egress.
		slog.Error("proxy: credential lookup failed", "err", err)
		http.Error(w, "credential store unavailable", http.StatusServiceUnavailable)
		return
	}

	// Authorize the route. One (method, path) per upstream IS the blast
	// radius: anything else is denied before any upstream contact.
	name := r.PathValue("name")
	up, ok := h.d.Config.Upstreams[name]
	if !ok {
		h.deny(w, r, cred, name, "", http.StatusForbidden, "unknown upstream")
		return
	}
	if r.PathValue("path") != up.Path {
		h.deny(w, r, cred, name, "", http.StatusForbidden, "path not allowed")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBody))
	if err != nil {
		h.deny(w, r, cred, name, "", http.StatusBadRequest, "request body unreadable or too large")
		return
	}
	var req struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		h.deny(w, r, cred, name, "", http.StatusBadRequest, "request body is not valid JSON")
		return
	}

	// Priced-pair gate (ported pattern): under a cents budget, a metered
	// model with no configured price cannot be admitted — its spend could
	// never be charged against the budget. Fail closed; a token-only
	// budget still governs unpriced models.
	price, priced := up.Prices[req.Model]
	if up.Classification == config.ClassMetered && cred.CapCents != nil && !priced {
		h.deny(w, r, cred, name, req.Model, http.StatusForbidden,
			"model has no configured price; a cents budget requires one (set a price or use a token budget)")
		return
	}

	// Budget check, fail closed.
	if err := h.d.Meter.Check(r.Context(), cred); err != nil {
		status := http.StatusForbidden
		var d meter.Denial
		if errors.As(err, &d) && (d.Status == http.StatusForbidden || d.Status == http.StatusTooManyRequests) {
			status = d.Status
		}
		h.deny(w, r, cred, name, req.Model, status, err.Error())
		return
	}

	// Resolve the real upstream credential — a Secret-mounted file only
	// the proxy can read; the agent side never sees it. Read per request
	// so rotation (expiring Copilot tokens, D8) needs no restart.
	var secret string
	if up.CredentialFile != "" {
		raw, err := os.ReadFile(up.CredentialFile)
		secret = strings.TrimSpace(string(raw))
		if err != nil || secret == "" {
			slog.Error("proxy: upstream credential unavailable", "upstream", name, "err", err)
			h.deny(w, r, cred, name, req.Model, http.StatusServiceUnavailable,
				"upstream credential unavailable")
			return
		}
	}

	// On streamed requests ask the upstream to append the usage chunk;
	// without it a streamed response would be unmeterable.
	outBody := body
	if req.Stream {
		if outBody, err = withIncludeUsage(body); err != nil {
			h.deny(w, r, cred, name, req.Model, http.StatusBadRequest, "request body is not a JSON object")
			return
		}
	}

	outReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		strings.TrimSuffix(up.BaseURL, "/")+"/"+up.Path, bytes.NewReader(outBody))
	if err != nil {
		h.deny(w, r, cred, name, req.Model, http.StatusBadGateway, "upstream request build failed")
		return
	}
	copyRequestHeaders(outReq.Header, r.Header)
	outReq.ContentLength = int64(len(outBody))
	if up.CredentialFile != "" {
		header := up.CredentialHeader
		if header == "" || strings.EqualFold(header, "authorization") {
			outReq.Header.Set("Authorization", "Bearer "+secret)
		} else {
			outReq.Header.Set(header, secret)
		}
	}
	for k, v := range up.ExtraHeaders {
		outReq.Header.Set(k, v)
	}

	resp, err := h.d.client().Do(outReq)
	if err != nil {
		slog.Error("proxy: upstream call failed", "upstream", name, "err", err)
		// The attempt is ledgered even though it failed — spend is
		// recorded before failures are honored (standing guidance); a
		// transport failure has no usage to bill, so tokens are zero.
		h.record(r, ledgerFor(cred, name, req.Model, up, priced, price, usage{}, http.StatusBadGateway))
		http.Error(w, "upstream unreachable", http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	copyResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	var u usage
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		u = relayStream(w, resp.Body)
	} else {
		u = relayBuffered(w, resp.Body)
	}
	if resp.StatusCode < 300 && u == (usage{}) {
		slog.Warn("proxy: no usage in upstream response; ledgering zero tokens",
			"upstream", name, "model", req.Model, "stream", req.Stream)
	}
	h.record(r, ledgerFor(cred, name, req.Model, up, priced, price, u, resp.StatusCode))
}

type usage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
}

// ledgerFor prices one forwarded call. cost_source is explicit: 'free' is
// a classification, never an inference; 'unpriced' keeps the token counts
// honest when a metered model has no configured price.
func ledgerFor(cred store.Credential, upstream, model string, up config.Upstream,
	priced bool, price pricing.Price, u usage, status int) store.LedgerEntry {
	e := store.LedgerEntry{
		CredentialName: cred.Name,
		Upstream:       upstream,
		Model:          model,
		InputTokens:    u.PromptTokens,
		OutputTokens:   u.CompletionTokens,
		Status:         status,
	}
	switch {
	case up.Classification == config.ClassFree:
		e.CostSource = "free"
	case priced:
		e.CostSource = "priced"
		e.CostCents = pricing.CostCents(price, u.PromptTokens, u.CompletionTokens)
	default:
		e.CostSource = "unpriced"
	}
	return e
}

// relayBuffered copies a non-streamed response through while extracting
// usage from the JSON body. The relay is byte-faithful: parse failures
// only cost usage extraction, never the response.
func relayBuffered(w http.ResponseWriter, body io.Reader) usage {
	raw, err := io.ReadAll(io.LimitReader(body, maxBufferedResp))
	if err != nil {
		slog.Error("proxy: reading upstream response", "err", err)
	}
	_, _ = w.Write(raw)
	var envelope struct {
		Usage usage `json:"usage"`
	}
	_ = json.Unmarshal(raw, &envelope)
	return envelope.Usage
}

// relayStream forwards SSE lines as they arrive (flushing each) and scans
// the data chunks for the final usage payload requested via
// stream_options.include_usage.
func relayStream(w http.ResponseWriter, body io.Reader) usage {
	flusher, _ := w.(http.Flusher)
	var u usage
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		line := sc.Text()
		_, _ = io.WriteString(w, line+"\n")
		if flusher != nil {
			flusher.Flush()
		}
		payload, ok := strings.CutPrefix(line, "data: ")
		if !ok || payload == "[DONE]" {
			continue
		}
		var chunk struct {
			Usage *usage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err == nil && chunk.Usage != nil {
			u = *chunk.Usage
		}
	}
	if err := sc.Err(); err != nil {
		slog.Error("proxy: relaying stream", "err", err)
	}
	return u
}

// withIncludeUsage sets stream_options.include_usage on the request JSON,
// preserving any other stream options the client sent.
func withIncludeUsage(body []byte) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	opts := map[string]json.RawMessage{}
	if raw, ok := m["stream_options"]; ok {
		if err := json.Unmarshal(raw, &opts); err != nil {
			return nil, err
		}
	}
	opts["include_usage"] = json.RawMessage("true")
	rawOpts, err := json.Marshal(opts)
	if err != nil {
		return nil, err
	}
	m["stream_options"] = rawOpts
	return json.Marshal(m)
}

// hopByHop are headers that must not be forwarded in either direction.
var hopByHop = map[string]bool{
	"Connection": true, "Keep-Alive": true, "Proxy-Authenticate": true,
	"Proxy-Authorization": true, "Te": true, "Trailer": true,
	"Transfer-Encoding": true, "Upgrade": true,
}

// copyRequestHeaders forwards client headers minus hop-by-hop, every
// credential slot (the Kaimahi token must never reach the upstream — the
// real credential goes in the upstream's native slot only), and
// Accept-Encoding (the proxy must read plaintext bodies to meter usage).
func copyRequestHeaders(dst, src http.Header) {
	for k, vs := range src {
		ck := http.CanonicalHeaderKey(k)
		if hopByHop[ck] || ck == "Authorization" || ck == "X-Api-Key" ||
			ck == "Api-Key" || ck == "Accept-Encoding" || ck == "Content-Length" || ck == "Host" {
			continue
		}
		dst[ck] = append([]string(nil), vs...)
	}
}

func copyResponseHeaders(dst, src http.Header) {
	for k, vs := range src {
		ck := http.CanonicalHeaderKey(k)
		if hopByHop[ck] {
			continue
		}
		dst[ck] = append([]string(nil), vs...)
	}
}
