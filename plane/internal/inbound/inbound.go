// Package inbound is the P7b inbound bridge: the governance plane's
// first INGRESS surface. An external event (a webhook) may trigger a
// kagent agent through it — and only through it, on the terms the plane
// enforces:
//
//   - the committed inbound_hooks table is the whole surface: a hook the
//     config does not name does not exist (401, no work done);
//   - authentication before any work: the caller proves it is the hook's
//     bound credential — a kmh_ bearer (resolved by sha256, exactly as the
//     proxy and gateway do), or a signature under a plane-held signing
//     secret (Kaimahi's v1 scheme, or Slack's v0 for the Events API);
//   - ingress bounds that reject rather than buffer: a per-hook body
//     limit, a per-hook token bucket, and one bounded queue of
//     outstanding invocations;
//   - replay protection: signed timestamps within a window, and a
//     delivery id that is unique among ADMITTED events per hook (the
//     audit row's index — an honoured event cannot be honoured twice);
//   - triggering is an APPROVABLE action (P4c): each admitted event
//     consumes one use of a live, bounded 'inbound' grant for the hook;
//     without one the event is denied and a request is filed (deny and
//     pend, deduped) — approval is constructive, exactly as P5a ruled;
//   - every inbound event causes spend, so the door previews the target
//     agent's governed budget (the credential its preset carries) and
//     refuses what the proxy could not admit — without consuming a grant
//     use, which stays the proxy's job;
//   - fail-closed degradation: an event that cannot be recorded is not
//     honoured (503 while the audit trail cannot be written).
//
// Admitted events are invoked asynchronously (202, then a worker sends
// A2A message/send to the agent via the kagent controller's endpoint):
// webhook sources demand a fast acknowledgement, and an agent turn on
// the demo model takes tens of seconds. The outcome is appended to the
// audit trail when it lands.
package inbound

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kaimahi-agents/kaimahi/plane/internal/config"
	"github.com/kaimahi-agents/kaimahi/plane/internal/meter"
	"github.com/kaimahi-agents/kaimahi/plane/internal/store"
)

// Store is what the bridge needs from Postgres. *store.Store satisfies it.
type Store interface {
	CredentialByTokenHash(ctx context.Context, tokenHash []byte) (store.Credential, error)
	CredentialByName(ctx context.Context, name string) (store.Credential, error)
	RecordInboundAudit(ctx context.Context, e store.InboundAuditEntry) error
	AdmitInboundEvent(ctx context.Context, hook, credential, delivery, agent string) (eventID, grantID string, err error)
	FileApprovalRequest(ctx context.Context, credential, kind, subject, detail string) (filed bool, err error)
	// P8b: approval commands from Slack decide requests here, with the
	// approver's identity.
	RequestByPrefix(ctx context.Context, prefix string) (store.ApprovalRequest, error)
	ApproveRequest(ctx context.Context, id string, expiresAt *time.Time, maxUses *int32, amount *int64, decidedBy string) (store.Grant, error)
	DenyApprovalRequest(ctx context.Context, id string, decidedBy string) error
}

// Replier posts a Slack command's outcome back into the thread it was
// typed in, through the governed posting path (notify.Poster). Nil
// means outcomes are logged, not posted.
type Replier interface {
	Reply(text, threadTS string)
}

// Meter previews the target budget without consuming. *meter.Meter
// satisfies it.
type Meter interface {
	Preview(ctx context.Context, cred store.Credential) error
}

type Deps struct {
	Store Store
	Meter Meter
	Hooks map[string]config.InboundHook
	// A2ABase is the kagent controller's origin; an agent is invoked at
	// {A2ABase}/api/a2a/{namespace}/{agent}/ — the one place the bridge
	// ever dials (the egress rule at this layer). Empty takes
	// DefaultA2ABase rather than a dial of "" that would fail only after
	// an event was admitted and its grant use burned.
	A2ABase string
	// Client makes the A2A call. Nil gets a default that never follows a
	// redirect (standing guidance) and bounds a call at InvokeTimeout.
	Client *http.Client
	// QueueSize bounds outstanding (queued + in-flight) invocations;
	// Workers is the invocation concurrency. Zero takes the defaults.
	QueueSize int
	Workers   int
	// InvokeTimeout bounds one agent turn (default 5 minutes).
	InvokeTimeout time.Duration
	Now           func() time.Time // nil = time.Now
	Replier       Replier
}

const (
	// DefaultA2ABase is the kagent controller Service as the chart
	// installs it.
	DefaultA2ABase       = "http://kagent-controller.kagent:8083"
	defaultQueueSize     = 16
	defaultWorkers       = 2
	defaultInvokeTimeout = 5 * time.Minute
	maxBufferedResp      = 8 << 20
	userIDPrefix         = "kaimahi-inbound/"
)

type job struct {
	eventID  string
	hook     string
	h        config.InboundHook
	delivery string
	text     string
}

type Bridge struct {
	d       Deps
	limiter *limiter
	jobs    chan job
	// slots bounds outstanding events: a slot is taken BEFORE an event
	// is admitted (so a full queue denies without burning a grant use)
	// and released when its invocation has been audited.
	slots chan struct{}
	// auditDegraded trips when an audit write fails and clears on the
	// next success. While tripped the bridge denies everything: an event
	// that cannot be recorded must not be honoured (the ledger/tool
	// audit contract, applied to ingress).
	auditDegraded atomic.Bool
	wg            sync.WaitGroup
}

// New applies the defaults (recorded on the bridge's copy of Deps, so
// what it runs with is what it reports) and wires the bounded queue.
func New(d Deps) *Bridge {
	if d.A2ABase == "" {
		d.A2ABase = DefaultA2ABase
	}
	if d.QueueSize <= 0 {
		d.QueueSize = defaultQueueSize
	}
	if d.Workers <= 0 {
		d.Workers = defaultWorkers
	}
	if d.InvokeTimeout <= 0 {
		d.InvokeTimeout = defaultInvokeTimeout
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Client == nil {
		d.Client = &http.Client{
			Timeout: d.InvokeTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	b := &Bridge{
		d:     d,
		jobs:  make(chan job, d.QueueSize),
		slots: make(chan struct{}, d.QueueSize),
	}
	// One clock: the limiter borrows the bridge's, so a test (or a
	// future injected clock) has a single thing to set.
	b.limiter = newLimiter(func() time.Time { return b.d.Now() })
	return b
}

// Mux serves the inbound surface: POST /hook/{name} and a health probe.
// Everything else is refused by the mux (404, or 405 for another method
// on the hook route) having done nothing.
func (b *Bridge) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /hook/{name}", b.receive)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

// Run starts the invocation workers and blocks until ctx is done and
// each worker has finished the event it was on. Events still queued at
// that point are lost to the restart — their 'admitted' row stands
// without an outcome, which is how the audit trail reports it.
func (b *Bridge) Run(ctx context.Context) {
	for i := 0; i < b.d.Workers; i++ {
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case j := <-b.jobs:
					b.process(j)
					<-b.slots
				}
			}
		}()
	}
	b.wg.Wait()
}

// audit appends one inbound row on a cancel-free context (a client
// disconnect must not drop the record of a decision already made) and
// reports whether it was written. A denial stands either way; an
// acknowledgement (challenge, ignored) is withheld when its row is not.
func (b *Bridge) audit(ctx context.Context, e store.InboundAuditEntry) bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := b.d.Store.RecordInboundAudit(ctx, e); err != nil {
		b.auditDegraded.Store(true)
		slog.Error("inbound: audit append failed; denying events until a write succeeds",
			"hook", e.Hook, "decision", e.Decision, "err", err)
		return false
	}
	b.auditDegraded.Store(false)
	return true
}

// deny is the single exit for every attributable refusal: audited
// (denied, with the refusal's status), then answered.
func (b *Bridge) deny(w http.ResponseWriter, r *http.Request, name string, h config.InboundHook,
	delivery string, status int, msg string) {
	b.audit(r.Context(), store.InboundAuditEntry{Hook: name, CredentialName: h.Credential,
		DeliveryID: delivery, Decision: "denied", Status: status, Detail: msg, Agent: agentRef(h)})
	slackRetryPolicy(w, h, status)
	http.Error(w, msg, status)
}

// slackRetryPolicy tells Slack not to retry a refusal that a retry
// cannot change. Slack re-delivers any event it did not get a 2xx for,
// up to three times, and counts failures towards disabling the
// subscription — so a 4xx that would only re-file the same denial (no
// grant, wrong channel, replay, malformed) says so with
// X-Slack-No-Retry. A 429 is the one 4xx a retry CAN change (the token
// bucket refills; a budget gets raised), so it carries nothing, like a
// 5xx: the plane is not refusing the event, it is refusing it NOW.
// Other hooks' callers have their own contracts and get no Slack header.
func slackRetryPolicy(w http.ResponseWriter, h config.InboundHook, status int) {
	if h.Auth == config.AuthSlack && status >= 400 && status < 500 && status != http.StatusTooManyRequests {
		w.Header().Set("X-Slack-No-Retry", "1")
	}
}

func agentRef(h config.InboundHook) string { return h.AgentNamespace + "/" + h.Agent }

func (b *Bridge) receive(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	h, ok := b.d.Hooks[name]
	if !ok {
		// A hook the config does not name gets the same answer as a
		// missing credential, and is unaudited: nothing to attribute it
		// to. (Named hooks do answer differently to oversize or flooded
		// calls, so names are not a secret; the credential is.)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	h = h.Bounded()

	// The token bucket runs BEFORE authentication and is not audited:
	// every later refusal writes a row, so the bucket is what bounds the
	// audit-write rate an unauthenticated flood can cause. The trade is
	// deliberate — a flood starves its hook, not the database.
	if !b.limiter.allow(name, h.RatePerMinute, h.Burst) {
		slog.Warn("inbound: rate limit exceeded", "hook", name)
		slackRetryPolicy(w, h, http.StatusTooManyRequests)
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	// A tripped audit trail fails the bridge closed for everything past
	// the bucket — nothing is honoured, not even a handshake, while
	// decisions cannot be recorded. The denial's own record attempt is
	// the recovery probe.
	if b.auditDegraded.Load() {
		b.deny(w, r, name, h, "", http.StatusServiceUnavailable, "inbound audit unavailable")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.MaxBodyBytes))
	if err != nil {
		b.deny(w, r, name, h, "", http.StatusRequestEntityTooLarge, "request body unreadable or too large")
		return
	}

	// Authenticate: the caller must prove it is the hook's bound
	// credential. A failed proof is a 401 (a 403 for a real credential
	// bound elsewhere); a malformed event is a 400; an unreadable
	// secret or store is a 503. All audited under the hook's credential.
	cred, delivery, ev, ok := b.authenticate(w, r, name, h, body)
	if !ok {
		return
	}

	if ev.challenge != "" {
		// Slack's URL verification: echo, audit, trigger nothing. An
		// acknowledgement the trail cannot record is not given (503,
		// which Slack retries), the same rule as everything else here.
		if !b.audit(r.Context(), store.InboundAuditEntry{Hook: name, CredentialName: cred.Name,
			Decision: "challenge", Status: http.StatusOK, Agent: agentRef(h)}) {
			http.Error(w, "inbound audit unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"challenge": ev.challenge})
		return
	}

	if ev.ignored != "" {
		// A well-formed event that is deliberately not a trigger (Slack:
		// anything but a human's app_mention). Acknowledged so the source
		// does not retry it, audited so the trail shows it arrived, and
		// nothing else: no budget preview, no grant use, no agent.
		if !b.audit(r.Context(), store.InboundAuditEntry{Hook: name, CredentialName: cred.Name,
			DeliveryID: delivery, Decision: "ignored", Status: http.StatusOK, Detail: ev.ignored, Agent: agentRef(h)}) {
			// Unrecorded is unacknowledged: a 503 makes Slack redeliver
			// once the trail is back, so the row is written eventually.
			http.Error(w, "inbound audit unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ignored", "reason": ev.ignored})
		return
	}

	// Where the mention was said must be a channel the hook is bound to
	// (P8). The list is read per request from plane custody, like the
	// signing secret; unreadable fails closed. The signature proved the
	// event came from the workspace; this proves it came from the room
	// the demo agreed to be in.
	if h.SlackChannelsFile != "" {
		channels, err := readSlackChannels(h.SlackChannelsFile)
		if err != nil {
			slog.Error("inbound: slack channel allowlist unavailable", "hook", name, "file", h.SlackChannelsFile, "err", err)
			b.deny(w, r, name, h, delivery, http.StatusServiceUnavailable, "hook channel allowlist unavailable")
			return
		}
		if !channels[ev.slack.channel] {
			b.deny(w, r, name, h, delivery, http.StatusForbidden,
				"channel "+ev.slack.channel+" is not one this hook is bound to")
			return
		}
	}

	// P8b: an approval command (`approve <id> …` / `deny <id>`) is the
	// second verb on this boundary. Recognised here — after the signature
	// and the channel, before the budget and the grant — because deciding
	// a request must not itself need a grant, and never runs the agent.
	// Anything that is not a command continues exactly as before.
	if h.Auth == config.AuthSlack {
		if c, ok := parseCommand(ev.slack.text); ok {
			b.handleCommand(w, r, name, h, delivery, cred, ev, c)
			return
		}
	}

	// Budget preview on the credential the triggered agent spends under.
	// A target whose credential does not exist is not governed, and an
	// ungoverned agent is not triggerable from outside.
	target, err := b.d.Store.CredentialByName(r.Context(), h.BudgetCredential)
	if errors.Is(err, store.ErrNotFound) {
		b.deny(w, r, name, h, delivery, http.StatusForbidden,
			"target agent is not governed: budget credential "+h.BudgetCredential+" is not issued")
		return
	}
	if err != nil {
		slog.Error("inbound: budget credential lookup failed", "hook", name, "err", err)
		b.deny(w, r, name, h, delivery, http.StatusServiceUnavailable, "credential store unavailable")
		return
	}
	if err := b.d.Meter.Preview(r.Context(), target); err != nil {
		// A cap denial keeps the meter's 429; anything else (metering
		// unavailable) is the plane degraded, which on an ingress is a
		// 503 like every other outage here — a caller must be able to
		// tell "refused" from "try later".
		status := http.StatusServiceUnavailable
		msg := err.Error()
		var d meter.Denial
		if errors.As(err, &d) && d.BudgetSubject != "" {
			status = http.StatusTooManyRequests
		}
		// Deny-and-pend (D13) under the AGENT's credential: the same
		// request the proxy would file when the agent is denied there,
		// deduped with it.
		if d.BudgetSubject != "" && b.fileRequest(r.Context(), target.Name, "budget", d.BudgetSubject,
			"denied inbound event via hook "+name) {
			msg += "; approval request filed — run 'make approvals'"
		}
		b.deny(w, r, name, h, delivery, status, "target budget: "+msg)
		return
	}

	// Reserve capacity BEFORE admitting: a full queue must deny without
	// burning a grant use or recording an admission it cannot honour.
	select {
	case b.slots <- struct{}{}:
	default:
		b.deny(w, r, name, h, delivery, http.StatusServiceUnavailable, "inbound queue full — retry later")
		return
	}
	release := func() { <-b.slots }

	// Admit: the audit row (replay guard) and the grant use, atomically —
	// on a cancel-free context: webhook sources hang up fast, and a
	// disconnect mid-commit must not leave a committed admission (use
	// burned, replay slot taken) that this side treats as a failure and
	// never queues.
	admitCtx, cancelAdmit := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	eventID, grantID, err := b.d.Store.AdmitInboundEvent(admitCtx, name, cred.Name, delivery, agentRef(h))
	cancelAdmit()
	switch {
	case errors.Is(err, store.ErrReplay):
		release()
		b.deny(w, r, name, h, delivery, http.StatusConflict, "replay: delivery already admitted")
		return
	case errors.Is(err, store.ErrNoGrant):
		release()
		msg := "inbound trigger not permitted: no live grant for hook " + name
		if b.fileRequest(r.Context(), cred.Name, "inbound", name, "denied inbound event on hook "+name) {
			msg += "; approval request filed — run 'make approvals'"
		}
		b.deny(w, r, name, h, delivery, http.StatusForbidden, msg)
		return
	case err != nil:
		release()
		slog.Error("inbound: admission failed", "hook", name, "err", err)
		b.deny(w, r, name, h, delivery, http.StatusServiceUnavailable, "inbound admission unavailable")
		return
	}

	// The slot is held; the queue has at least that much room by
	// construction, so this send cannot block.
	b.jobs <- job{eventID: eventID, hook: name, h: h, delivery: delivery, text: ev.text}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"event_id": eventID, "hook": name, "agent": agentRef(h), "grant": grantID, "status": "admitted"})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// authenticate resolves the hook's credential by the hook's proof mode
// and extracts the event. It answers (audited) on failure.
func (b *Bridge) authenticate(w http.ResponseWriter, r *http.Request, name string, h config.InboundHook,
	body []byte) (store.Credential, string, event, bool) {
	now := b.d.Now()
	switch h.Auth {
	case config.AuthBearer:
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			b.deny(w, r, name, h, "", http.StatusUnauthorized, "unauthorized")
			return store.Credential{}, "", event{}, false
		}
		hash := sha256.Sum256([]byte(token))
		cred, err := b.d.Store.CredentialByTokenHash(r.Context(), hash[:])
		if errors.Is(err, store.ErrNotFound) {
			b.deny(w, r, name, h, "", http.StatusUnauthorized, "unauthorized")
			return store.Credential{}, "", event{}, false
		}
		if err != nil {
			slog.Error("inbound: credential lookup failed", "hook", name, "err", err)
			b.deny(w, r, name, h, "", http.StatusServiceUnavailable, "credential store unavailable")
			return store.Credential{}, "", event{}, false
		}
		if cred.Name != h.Credential {
			b.deny(w, r, name, h, "", http.StatusForbidden, "credential is not bound to this hook")
			return store.Credential{}, "", event{}, false
		}
		delivery := r.Header.Get(HeaderDelivery)
		if !deliveryRe.MatchString(delivery) {
			b.deny(w, r, name, h, "", http.StatusBadRequest, HeaderDelivery+" header required (a unique delivery id)")
			return store.Credential{}, "", event{}, false
		}
		ev, ok := genericEvent(body)
		if !ok {
			b.deny(w, r, name, h, delivery, http.StatusBadRequest, "event carries no text")
			return store.Credential{}, "", event{}, false
		}
		return cred, delivery, ev, true

	case config.AuthKaimahiHMAC, config.AuthSlack:
		secret, err := readSecret(h.SigningSecretFile)
		if err != nil {
			slog.Error("inbound: signing secret unavailable", "hook", name, "file", h.SigningSecretFile)
			b.deny(w, r, name, h, "", http.StatusServiceUnavailable, "hook signing secret unavailable")
			return store.Credential{}, "", event{}, false
		}
		var delivery string
		var ev event
		if h.Auth == config.AuthKaimahiHMAC {
			ts := r.Header.Get(HeaderTimestamp)
			delivery = r.Header.Get(HeaderDelivery)
			if !deliveryRe.MatchString(delivery) || !freshTimestamp(ts, now) ||
				!verifySignature(secret, r.Header.Get(HeaderSignature), kaimahiVersion, kaimahiBase(ts, delivery, body)) {
				// One answer for a bad, missing, or stale signature: a
				// forger learns nothing from which.
				b.deny(w, r, name, h, "", http.StatusUnauthorized, "unauthorized")
				return store.Credential{}, "", event{}, false
			}
			ok := false
			if ev, ok = genericEvent(body); !ok {
				b.deny(w, r, name, h, delivery, http.StatusBadRequest, "event carries no text")
				return store.Credential{}, "", event{}, false
			}
		} else {
			ts := r.Header.Get(slackTimestamp)
			if !freshTimestamp(ts, now) ||
				!verifySignature(secret, r.Header.Get(slackSignature), slackVersion, slackBase(ts, body)) {
				b.deny(w, r, name, h, "", http.StatusUnauthorized, "unauthorized")
				return store.Credential{}, "", event{}, false
			}
			ok := false
			if ev, ok = slackEvent(body); !ok {
				b.deny(w, r, name, h, "", http.StatusBadRequest, "unrecognised Slack event envelope")
				return store.Credential{}, "", event{}, false
			}
			delivery = ev.delivery
			if ev.challenge == "" && !deliveryRe.MatchString(delivery) {
				b.deny(w, r, name, h, "", http.StatusBadRequest, "event_id missing or malformed")
				return store.Credential{}, "", event{}, false
			}
		}
		// The signature proved the caller holds the hook's secret; the
		// identity charged is the hook's configured credential, which
		// must have been issued (grants and requests hang off it).
		cred, err := b.d.Store.CredentialByName(r.Context(), h.Credential)
		if errors.Is(err, store.ErrNotFound) {
			b.deny(w, r, name, h, delivery, http.StatusServiceUnavailable,
				"hook credential "+h.Credential+" is not issued (run make inbound-credential)")
			return store.Credential{}, "", event{}, false
		}
		if err != nil {
			slog.Error("inbound: credential lookup failed", "hook", name, "err", err)
			b.deny(w, r, name, h, delivery, http.StatusServiceUnavailable, "credential store unavailable")
			return store.Credential{}, "", event{}, false
		}
		return cred, delivery, ev, true
	}
	// config.Parse admits only the modes above.
	b.deny(w, r, name, h, "", http.StatusServiceUnavailable, "hook auth mode unsupported")
	return store.Credential{}, "", event{}, false
}

// fileRequest files a pending approval request on a cancel-free context.
// A filing failure never un-denies and never trips the breaker — the
// denial is the safe state (P4b/P4c contract).
func (b *Bridge) fileRequest(ctx context.Context, credential, kind, subject, detail string) bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, err := b.d.Store.FileApprovalRequest(ctx, credential, kind, subject, detail); err != nil {
		slog.Error("inbound: filing approval request failed (denial stands)",
			"credential", credential, "kind", kind, "subject", subject, "err", err)
		return false
	}
	return true
}

// process runs one admitted event: the A2A call, then the outcome row.
// Detached from any request context — the caller was answered 202 long
// ago — and bounded by the invoke timeout.
func (b *Bridge) process(j job) {
	ctx, cancel := context.WithTimeout(context.Background(), b.d.InvokeTimeout)
	defer cancel()
	out := b.invoke(ctx, j)
	e := store.InboundAuditEntry{Hook: j.hook, CredentialName: j.h.Credential, DeliveryID: j.delivery,
		Status: out.status, Agent: agentRef(j.h), InputTokens: out.inTokens, OutputTokens: out.outTokens}
	if out.err != nil {
		e.Decision, e.Detail = "failed", out.err.Error()
		slog.Error("inbound: invocation failed", "hook", j.hook, "event", j.eventID, "err", out.err)
	} else {
		e.Decision, e.Detail = "completed", "task "+out.taskID
	}
	b.audit(ctx, e)
}

type outcome struct {
	status              int
	taskID              string
	inTokens, outTokens int64
	err                 error
}

// invoke sends A2A message/send (protocol 0.3 as served by kagent
// 0.9.12 — measured on the live agent card) through the controller's
// per-agent endpoint. Success is a well-formed positive only: HTTP 200,
// no JSON-RPC error, a task in state "completed" with non-empty text —
// the rule scripts/verify-chat.py applies to `make chat`.
func (b *Bridge) invoke(ctx context.Context, j job) outcome {
	msgID := "kaimahi-inbound-" + j.eventID
	reqBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      msgID,
		"method":  "message/send",
		"params": map[string]any{
			"message": map[string]any{
				"kind":      "message",
				"role":      "user",
				"messageId": msgID,
				"parts":     []map[string]string{{"kind": "text", "text": j.text}},
			},
		},
	})
	if err != nil {
		return outcome{err: err}
	}
	url := strings.TrimSuffix(b.d.A2ABase, "/") + "/api/a2a/" + j.h.AgentNamespace + "/" + j.h.Agent + "/"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return outcome{err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	// kagent attributes the session to this id; the hook, not the
	// external sender, is the actor the plane vouches for.
	req.Header.Set("x-user-id", userIDPrefix+j.hook)
	resp, err := b.d.Client.Do(req)
	if err != nil {
		return outcome{err: fmt.Errorf("agent unreachable: %w", err)}
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBufferedResp))
	if err != nil {
		return outcome{status: resp.StatusCode, err: fmt.Errorf("reading agent response: %w", err)}
	}
	o := outcome{status: resp.StatusCode}
	if resp.StatusCode != http.StatusOK {
		o.err = fmt.Errorf("agent endpoint answered HTTP %d", resp.StatusCode)
		return o
	}
	var env struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Result struct {
			ID     string `json:"id"`
			Kind   string `json:"kind"`
			Status struct {
				State string `json:"state"`
			} `json:"status"`
			Artifacts []struct {
				Parts []struct {
					Kind string `json:"kind"`
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"artifacts"`
			History []struct {
				Role     string `json:"role"`
				Metadata struct {
					Usage *struct {
						Prompt     int64 `json:"promptTokenCount"`
						Candidates int64 `json:"candidatesTokenCount"`
					} `json:"kagent_usage_metadata"`
				} `json:"metadata"`
			} `json:"history"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		o.err = fmt.Errorf("agent response is not a JSON-RPC message")
		return o
	}
	if env.Error != nil {
		o.err = fmt.Errorf("agent returned JSON-RPC error %d: %s", env.Error.Code, env.Error.Message)
		return o
	}
	o.taskID = env.Result.ID
	for _, m := range env.Result.History {
		if m.Role == "agent" && m.Metadata.Usage != nil {
			o.inTokens += max(m.Metadata.Usage.Prompt, 0)
			o.outTokens += max(m.Metadata.Usage.Candidates, 0)
		}
	}
	var text strings.Builder
	for _, a := range env.Result.Artifacts {
		for _, p := range a.Parts {
			if p.Kind == "text" {
				text.WriteString(p.Text)
			}
		}
	}
	if env.Result.Kind != "task" || env.Result.Status.State != "completed" || strings.TrimSpace(text.String()) == "" {
		o.err = fmt.Errorf("task %s did not complete with a reply (state %q)", env.Result.ID, env.Result.Status.State)
	}
	return o
}
