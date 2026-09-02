// Package metrics is the plane's Prometheus surface (P9, D24): what an
// operator watches, on its own cluster-internal listener, with NO
// identifier as a label value. Every label is drawn from a fixed
// vocabulary — the seams, the decisions, the refusal reasons, the grant
// kinds, the queues — or from two operator-chosen names that are
// already public in the repo and printed by every audit command: a
// credential's NAME (never its token) and an upstream's name. A channel
// id, a user id, a request id, a delivery id, a model string, or any
// free text never becomes a label; metrics_test.go asserts the label
// set and the value shapes on the live registry.
package metrics

import (
	"context"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Seam is which enforcement point decided.
type Seam string

const (
	SeamProxy   Seam = "proxy"
	SeamGateway Seam = "gateway"
	SeamInbound Seam = "inbound"
)

// Decision is what the seam did with the call.
type Decision string

const (
	// Allowed: admitted by configuration (a cap with room, an allowlist).
	Allowed Decision = "allowed"
	// Granted: admitted by a live time-boxed grant (a use consumed).
	Granted Decision = "granted"
	Denied  Decision = "denied"
)

// Reason says why, from a fixed list. Allowed and granted decisions
// carry the mechanism ("ok", "budget", "allowlist", "grant", …);
// denials carry the refusal class.
type Reason string

const (
	ReasonOK                  Reason = "ok"
	ReasonBudget              Reason = "budget"
	ReasonAllowlist           Reason = "allowlist"
	ReasonGrant               Reason = "grant"
	ReasonUnauthorized        Reason = "unauthorized"
	ReasonCredentialStore     Reason = "credential_store"
	ReasonRoute               Reason = "route"
	ReasonBadRequest          Reason = "bad_request"
	ReasonUnpricedModel       Reason = "unpriced_model"
	ReasonAuditDegraded       Reason = "audit_degraded"
	ReasonMetering            Reason = "metering"
	ReasonUpstreamCredential  Reason = "upstream_credential"
	ReasonUpstreamError       Reason = "upstream_error"
	ReasonUpstreamUnreachable Reason = "upstream_unreachable"
	// ReasonEgressRefused (P10): the egress policy refused the upstream
	// before any byte left — a private/metadata answer, a forbidden port
	// or scheme, no hardened client — as distinct from an upstream that
	// was dialed and did not answer (a cut body counts as upstream_error).
	ReasonEgressRefused Reason = "egress_refused"
	ReasonMethod        Reason = "method"
	ReasonGrantCheck    Reason = "grant_check"
	ReasonRateLimit     Reason = "rate_limit"
	ReasonTooLarge      Reason = "too_large"
	ReasonReplay        Reason = "replay"
	ReasonQueueFull     Reason = "queue_full"
	ReasonHookConfig    Reason = "hook_config"
	ReasonAdmission     Reason = "admission"
	ReasonNotApprover   Reason = "not_approver"
	ReasonIgnored       Reason = "ignored"
	ReasonChallenge     Reason = "challenge"
	ReasonCommand       Reason = "command"
	ReasonOther         Reason = "other"
)

// Queue names a bounded per-replica queue.
type Queue string

const (
	QueueInbound  Queue = "inbound_jobs"
	QueueNotifier Queue = "notifier"
)

// Vocabulary is the complete set of allowed values per fixed label; the
// test walks the registry against it, and Decide/SetQueue refuse
// anything outside it at the type level.
var Vocabulary = map[string][]string{
	"seam":     {string(SeamProxy), string(SeamGateway), string(SeamInbound)},
	"decision": {string(Allowed), string(Granted), string(Denied)},
	"reason": {string(ReasonOK), string(ReasonBudget), string(ReasonAllowlist), string(ReasonGrant),
		string(ReasonUnauthorized), string(ReasonCredentialStore), string(ReasonRoute), string(ReasonBadRequest),
		string(ReasonUnpricedModel), string(ReasonAuditDegraded), string(ReasonMetering), string(ReasonUpstreamCredential),
		string(ReasonUpstreamError), string(ReasonUpstreamUnreachable), string(ReasonEgressRefused), string(ReasonMethod), string(ReasonGrantCheck),
		string(ReasonRateLimit), string(ReasonTooLarge), string(ReasonReplay), string(ReasonQueueFull),
		string(ReasonHookConfig), string(ReasonAdmission), string(ReasonNotApprover), string(ReasonIgnored),
		string(ReasonChallenge), string(ReasonCommand), string(ReasonOther)},
	"kind":  {"tool", "budget", "inbound"},
	"queue": {string(QueueInbound), string(QueueNotifier)},
}

// Name shapes for the two operator-chosen labels. A credential name is
// what admin.go accepts at issue time; an upstream name is a key of the
// committed upstream table. Anything else is reported as "other" rather
// than admitted as a label — the shape is enforced here, not trusted.
var (
	credentialShape = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	upstreamShape   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)
)

func shaped(re *regexp.Regexp, v string) string {
	if re.MatchString(v) {
		return v
	}
	return "other"
}

var (
	registry = prometheus.NewRegistry()

	decisions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "kaimahi_decisions_total",
		Help: "Governance decisions by seam (proxy, gateway, inbound), decision (allowed, granted, denied) and reason.",
	}, []string{"seam", "decision", "reason"})

	upstreamLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "kaimahi_upstream_latency_seconds",
		Help:    "Time an admitted call spent at its upstream, by seam and upstream name.",
		Buckets: []float64{.05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120, 300},
	}, []string{"seam", "upstream"})

	queueDepth = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kaimahi_queue_depth",
		Help: "Items in a bounded per-replica queue (queued plus in flight).",
	}, []string{"queue"})

	queueCapacity = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kaimahi_queue_capacity",
		Help: "Capacity of a bounded per-replica queue.",
	}, []string{"queue"})

	degraded = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kaimahi_seam_degraded",
		Help: "1 while the seam refuses everything because its audit or ledger write last failed (fail closed), per replica.",
	}, []string{"seam"})

	buildInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kaimahi_build_info",
		Help: "Build information; always 1.",
	}, []string{"version", "go_version"})
)

func init() {
	registry.MustRegister(decisions, upstreamLatency, queueDepth, queueCapacity, degraded, buildInfo,
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	buildInfo.WithLabelValues(Version(), goVersion()).Set(1)
	// Pre-create the series operators alert on, so an idle plane exposes
	// zeros rather than nothing.
	for _, s := range []Seam{SeamProxy, SeamGateway, SeamInbound} {
		degraded.WithLabelValues(string(s)).Set(0)
		decisions.WithLabelValues(string(s), string(Denied), string(ReasonBudget)).Add(0)
		decisions.WithLabelValues(string(s), string(Allowed), string(ReasonOK)).Add(0)
	}
}

// Decide counts one governance decision.
func Decide(seam Seam, decision Decision, reason Reason) {
	decisions.WithLabelValues(string(seam), string(decision), string(reason)).Inc()
}

// PrimeUpstreams creates the latency series for the configured upstream
// names at boot, so a replica that has not forwarded anything yet
// exposes empty histograms rather than none. Names outside the shape
// collapse to "other" like everywhere else.
func PrimeUpstreams(seam Seam, upstreams []string) {
	for _, u := range upstreams {
		upstreamLatency.WithLabelValues(string(seam), shaped(upstreamShape, u))
	}
}

// ObserveUpstream records how long an admitted call spent upstream.
func ObserveUpstream(seam Seam, upstream string, d time.Duration) {
	upstreamLatency.WithLabelValues(string(seam), shaped(upstreamShape, upstream)).Observe(d.Seconds())
}

// SetQueue publishes a bounded queue's depth and capacity.
func SetQueue(q Queue, depth, capacity int) {
	queueDepth.WithLabelValues(string(q)).Set(float64(depth))
	queueCapacity.WithLabelValues(string(q)).Set(float64(capacity))
}

// SetDegraded publishes a seam's fail-closed breaker state.
func SetDegraded(seam Seam, tripped bool) {
	v := 0.0
	if tripped {
		v = 1
	}
	degraded.WithLabelValues(string(seam)).Set(v)
}

// buildVersion is set by the linker (plane/Dockerfile passes the git
// revision as -X); the image build has no .git to stamp from itself.
var buildVersion string

// Version is the build's revision: the linker-set value, else the VCS
// revision as Go stamped it (short), else "unknown".
func Version() string {
	if buildVersion != "" {
		return buildVersion
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "unknown"
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if dirty {
		rev += "-dirty"
	}
	return rev
}

func goVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.GoVersion != "" {
		return info.GoVersion
	}
	return "unknown"
}

// LedgerTotal is one credential's month-to-date ledger sums.
type LedgerTotal struct {
	Credential    string
	Cents, Tokens int64
}

// Source is what the scrape-time collector reads from the store (the
// replica-independent truths: they live in Postgres, not in a process).
type Source interface {
	LedgerMonthTotals(ctx context.Context, monthStart time.Time) ([]LedgerTotal, error)
	LiveGrantCounts(ctx context.Context) (map[string]int64, error)
	OpenReservations(ctx context.Context, credential string) (int64, error)
}

// storeCollector reads the store at scrape time, bounded. When the read
// fails the DB-derived series are absent for that scrape and
// kaimahi_store_up is 0 — never a stale or invented number.
type storeCollector struct {
	src        Source
	monthStart func() time.Time
	cents      *prometheus.Desc
	tokens     *prometheus.Desc
	grants     *prometheus.Desc
	holds      *prometheus.Desc
	up         *prometheus.Desc
}

// RegisterStore attaches the store-derived series. Call once, from main.
func RegisterStore(src Source, monthStart func() time.Time) {
	registry.MustRegister(&storeCollector{
		src: src, monthStart: monthStart,
		cents: prometheus.NewDesc("kaimahi_ledger_month_cents",
			"Month-to-date ledgered cost per credential name (calendar month, UTC).", []string{"credential"}, nil),
		tokens: prometheus.NewDesc("kaimahi_ledger_month_tokens",
			"Month-to-date ledgered tokens (input plus output) per credential name.", []string{"credential"}, nil),
		grants: prometheus.NewDesc("kaimahi_live_grants",
			"Time-boxed grants currently live (not expired, not exhausted), by kind.", []string{"kind"}, nil),
		holds: prometheus.NewDesc("kaimahi_open_reservations",
			"Admitted calls whose ledger row has not landed yet (spend holds), across all replicas.", nil, nil),
		up: prometheus.NewDesc("kaimahi_store_up",
			"1 when the store answered this scrape's reads; 0 when the store-derived series are absent because it did not.", nil, nil),
	})
}

func (c *storeCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.cents
	ch <- c.tokens
	ch <- c.grants
	ch <- c.holds
	ch <- c.up
}

func (c *storeCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	totals, err := c.src.LedgerMonthTotals(ctx, c.monthStart())
	if err == nil {
		var counts map[string]int64
		counts, err = c.src.LiveGrantCounts(ctx)
		if err == nil {
			var open int64
			open, err = c.src.OpenReservations(ctx, "")
			if err == nil {
				for _, t := range totals {
					name := shaped(credentialShape, t.Credential)
					ch <- prometheus.MustNewConstMetric(c.cents, prometheus.GaugeValue, float64(t.Cents), name)
					ch <- prometheus.MustNewConstMetric(c.tokens, prometheus.GaugeValue, float64(t.Tokens), name)
				}
				for _, kind := range Vocabulary["kind"] {
					ch <- prometheus.MustNewConstMetric(c.grants, prometheus.GaugeValue, float64(counts[kind]), kind)
				}
				ch <- prometheus.MustNewConstMetric(c.holds, prometheus.GaugeValue, float64(open))
			}
		}
	}
	up := 1.0
	if err != nil {
		up = 0
	}
	ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, up)
}

// Registry is the plane's registry: what the ops listener serves and
// what the label test walks.
func Registry() *prometheus.Registry { return registry }
