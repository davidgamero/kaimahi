package metrics_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"github.com/kaimahi-agents/kaimahi/plane/internal/metrics"
)

type fakeSource struct {
	totals []metrics.LedgerTotal
	err    error
}

func (f *fakeSource) LedgerMonthTotals(_ context.Context, _ time.Time) ([]metrics.LedgerTotal, error) {
	return f.totals, f.err
}
func (f *fakeSource) LiveGrantCounts(_ context.Context) (map[string]int64, error) {
	return map[string]int64{"tool": 2, "budget": 1}, f.err
}
func (f *fakeSource) OpenReservations(_ context.Context, _ string) (int64, error) { return 3, f.err }

var src = &fakeSource{totals: []metrics.LedgerTotal{
	{Credential: "hello-world", Cents: 12, Tokens: 3400},
	{Credential: "kaimahi-plane", Tokens: 10},
	// A name outside the credential shape (impossible via admin.go, but
	// the label must not trust the store): reported as "other".
	{Credential: "C0123ABC:U0EVIL;drop", Tokens: 1},
}}

func init() { metrics.RegisterStore(src, func() time.Time { return time.Now() }) }

// allowed is the whole label surface. Anything not listed here — a
// token, a channel id, a user id, a request id, a delivery id, a model
// string, free text — would fail this test the moment it appeared.
var allowed = map[string]*regexp.Regexp{
	"seam":       nil, // vocabulary
	"decision":   nil,
	"reason":     nil,
	"kind":       nil,
	"queue":      nil,
	"credential": regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$|^other$`),
	"upstream":   regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$|^other$`),
	// kaimahi_build_info's VCS revision, or go_info's Go version.
	"version":    regexp.MustCompile(`^[0-9a-f]{7,12}(-dirty)?$|^unknown$|^go[0-9][0-9A-Za-z.\-+]*$|^devel.*$`),
	"go_version": regexp.MustCompile(`^go[0-9][0-9A-Za-z.\-+]*$|^unknown$|^devel.*$`),
	// The Go and process collectors' own labels.
	"quantile": regexp.MustCompile(`^[0-9.]+$`),
	"le":       regexp.MustCompile(`^[0-9.e+\-]+$|^\+Inf$`),
	"name":     regexp.MustCompile(`^/[a-z0-9/:_-]+$`),
	"unit":     regexp.MustCompile(`^[a-z0-9-]+$`),
}

func TestEveryLabelIsFromTheFixedVocabularyOrAnAllowedShape(t *testing.T) {
	// Exercise every path so the series exist.
	for _, seam := range []metrics.Seam{metrics.SeamProxy, metrics.SeamGateway, metrics.SeamInbound} {
		for _, r := range metrics.Vocabulary["reason"] {
			metrics.Decide(seam, metrics.Denied, metrics.Reason(r))
		}
		metrics.Decide(seam, metrics.Allowed, metrics.ReasonOK)
		metrics.Decide(seam, metrics.Granted, metrics.ReasonGrant)
		metrics.ObserveUpstream(seam, "ollama", 10*time.Millisecond)
		// Free text as an upstream name is coerced to "other", never admitted.
		metrics.ObserveUpstream(seam, "https://evil.example/?token=kmh_abc", time.Millisecond)
		metrics.SetDegraded(seam, false)
	}
	metrics.SetQueue(metrics.QueueInbound, 1, 16)
	metrics.SetQueue(metrics.QueueNotifier, 0, 32)

	families, err := metrics.Registry().Gather()
	require.NoError(t, err)
	var kaimahi int
	for _, mf := range families {
		if strings.HasPrefix(mf.GetName(), "kaimahi_") {
			kaimahi++
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				re, ok := allowed[lp.GetName()]
				require.Truef(t, ok, "%s: label %q is not in the allowed label set", mf.GetName(), lp.GetName())
				if re == nil {
					require.Containsf(t, metrics.Vocabulary[lp.GetName()], lp.GetValue(),
						"%s: %s=%q is outside the fixed vocabulary", mf.GetName(), lp.GetName(), lp.GetValue())
				} else {
					require.Regexpf(t, re, lp.GetValue(),
						"%s: %s=%q has a disallowed shape", mf.GetName(), lp.GetName(), lp.GetValue())
				}
			}
		}
	}
	require.GreaterOrEqual(t, kaimahi, 10)
}

func find(t *testing.T, name string) *dto.MetricFamily {
	t.Helper()
	families, err := metrics.Registry().Gather()
	require.NoError(t, err)
	for _, mf := range families {
		if mf.GetName() == name {
			return mf
		}
	}
	t.Fatalf("metric %s not exposed", name)
	return nil
}

func labels(m *dto.Metric) map[string]string {
	out := map[string]string{}
	for _, lp := range m.GetLabel() {
		out[lp.GetName()] = lp.GetValue()
	}
	return out
}

func TestExpectedMetricsAreExposed(t *testing.T) {
	for _, name := range []string{
		"kaimahi_decisions_total", "kaimahi_upstream_latency_seconds", "kaimahi_queue_depth",
		"kaimahi_queue_capacity", "kaimahi_seam_degraded", "kaimahi_build_info",
		"kaimahi_ledger_month_cents", "kaimahi_ledger_month_tokens", "kaimahi_live_grants",
		"kaimahi_open_reservations", "kaimahi_store_up",
		"go_goroutines", "process_resident_memory_bytes",
	} {
		find(t, name)
	}
}

func TestStoreDerivedSeriesCarryCredentialNamesOnly(t *testing.T) {
	src.err = nil
	tokens := find(t, "kaimahi_ledger_month_tokens")
	got := map[string]float64{}
	for _, m := range tokens.GetMetric() {
		got[labels(m)["credential"]] = m.GetGauge().GetValue()
	}
	require.Equal(t, map[string]float64{"hello-world": 3400, "kaimahi-plane": 10, "other": 1}, got)
	grants := find(t, "kaimahi_live_grants")
	byKind := map[string]float64{}
	for _, m := range grants.GetMetric() {
		byKind[labels(m)["kind"]] = m.GetGauge().GetValue()
	}
	require.Equal(t, map[string]float64{"tool": 2, "budget": 1, "inbound": 0}, byKind)
	require.EqualValues(t, 3, find(t, "kaimahi_open_reservations").GetMetric()[0].GetGauge().GetValue())
	require.EqualValues(t, 1, find(t, "kaimahi_store_up").GetMetric()[0].GetGauge().GetValue())
}

func TestStoreOutageDropsDerivedSeriesAndReportsDown(t *testing.T) {
	src.err = errors.New("db down")
	defer func() { src.err = nil }()
	families, err := metrics.Registry().Gather()
	require.NoError(t, err)
	names := map[string]bool{}
	for _, mf := range families {
		names[mf.GetName()] = true
	}
	require.False(t, names["kaimahi_ledger_month_tokens"], "no stale or invented totals while the store is down")
	require.EqualValues(t, 0, find(t, "kaimahi_store_up").GetMetric()[0].GetGauge().GetValue())
}

func TestPrimedUpstreamsExposeEmptyHistograms(t *testing.T) {
	metrics.PrimeUpstreams(metrics.SeamGateway, []string{"kagent-tools", "not a name!"})
	seen := map[string]bool{}
	for _, m := range find(t, "kaimahi_upstream_latency_seconds").GetMetric() {
		l := labels(m)
		if l["seam"] == "gateway" {
			seen[l["upstream"]] = true
		}
	}
	require.True(t, seen["kagent-tools"], "primed series present before any observation")
	require.True(t, seen["other"], "a name outside the shape primes as other")
	require.False(t, seen["not a name!"])
}

func TestDecisionCountsIncrement(t *testing.T) {
	before := decisionValue(t, "proxy", "denied", "budget")
	metrics.Decide(metrics.SeamProxy, metrics.Denied, metrics.ReasonBudget)
	metrics.Decide(metrics.SeamProxy, metrics.Denied, metrics.ReasonBudget)
	require.Equal(t, before+2, decisionValue(t, "proxy", "denied", "budget"))
}

func decisionValue(t *testing.T, seam, decision, reason string) float64 {
	t.Helper()
	for _, m := range find(t, "kaimahi_decisions_total").GetMetric() {
		l := labels(m)
		if l["seam"] == seam && l["decision"] == decision && l["reason"] == reason {
			return m.GetCounter().GetValue()
		}
	}
	return 0
}

func TestVocabularyMatchesTheConstants(t *testing.T) {
	// Every Reason constant must be in the vocabulary (a new constant
	// added without a vocabulary entry would pass Decide and fail the
	// label test only once exercised — catch it here unconditionally).
	for _, r := range []metrics.Reason{metrics.ReasonOK, metrics.ReasonBudget, metrics.ReasonAllowlist,
		metrics.ReasonGrant, metrics.ReasonUnauthorized, metrics.ReasonCredentialStore, metrics.ReasonRoute,
		metrics.ReasonBadRequest, metrics.ReasonUnpricedModel, metrics.ReasonAuditDegraded, metrics.ReasonMetering,
		metrics.ReasonUpstreamCredential, metrics.ReasonUpstreamError, metrics.ReasonUpstreamUnreachable,
		metrics.ReasonMethod, metrics.ReasonGrantCheck, metrics.ReasonRateLimit, metrics.ReasonTooLarge,
		metrics.ReasonReplay, metrics.ReasonQueueFull, metrics.ReasonHookConfig, metrics.ReasonAdmission,
		metrics.ReasonNotApprover, metrics.ReasonIgnored, metrics.ReasonChallenge, metrics.ReasonCommand,
		metrics.ReasonOther} {
		require.Contains(t, metrics.Vocabulary["reason"], string(r))
	}
}
