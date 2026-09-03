package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// The name comes first and the flags come after — `kmx agent create billing
// --tools server:tool`. The flag package stops at the first non-flag word, so
// without parseInterspersed every flag after the name is silently dropped and
// the command reports a usage error about arguments it refused to read. That
// is not hypothetical: it is what the first end-to-end run of `kmx agent
// create` did.
func TestFlagsAreParsedOnEitherSideOfTheName(t *testing.T) {
	for _, tc := range []struct {
		name      string
		argv      []string
		wantNames []string
		wantTools string
		wantDry   bool
	}{
		{"flags after the name", []string{"billing", "--tools", "server:one,two"}, []string{"billing"}, "server:one,two", false},
		{"flags before the name", []string{"--tools", "server:one", "billing"}, []string{"billing"}, "server:one", false},
		{"flags on both sides", []string{"--dry-run", "billing", "--tools", "server:one"}, []string{"billing"}, "server:one", true},
		{"inline value", []string{"billing", "--tools=server:one"}, []string{"billing"}, "server:one", false},
		{"a bool flag after the name", []string{"billing", "--dry-run"}, []string{"billing"}, "", true},
		{"no flags", []string{"billing"}, []string{"billing"}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := newFlagSet("agent create")
			tools := fs.String("tools", "", "")
			dry := fs.Bool("dry-run", false, "")
			names, err := parseInterspersed(fs, tc.argv)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if !reflect.DeepEqual(names, tc.wantNames) {
				t.Errorf("positional args %v, want %v", names, tc.wantNames)
			}
			if *tools != tc.wantTools {
				t.Errorf("--tools %q, want %q", *tools, tc.wantTools)
			}
			if *dry != tc.wantDry {
				t.Errorf("--dry-run %v, want %v", *dry, tc.wantDry)
			}
		})
	}
}

// Two names is a mistake worth catching, not a name plus a stray word to
// ignore.
func TestASecondPositionalIsKept(t *testing.T) {
	fs := newFlagSet("agent create")
	fs.String("tools", "", "")
	names, err := parseInterspersed(fs, []string{"billing", "investigator"})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Errorf("got %v, want both positionals so the caller can refuse them", names)
	}
}

// --context is global: it may appear before or after the command, because an
// operator who has just been shown a banner naming the wrong cluster should
// be able to append it to what they already typed.
func TestContextIsExtractedFromAnywhere(t *testing.T) {
	for _, tc := range []struct {
		argv     []string
		wantKept []string
		wantCtx  string
	}{
		{[]string{"--context", "kind-x", "up"}, []string{"up"}, "kind-x"},
		{[]string{"up", "--context", "kind-x"}, []string{"up"}, "kind-x"},
		{[]string{"up", "--context=kind-x"}, []string{"up"}, "kind-x"},
		{[]string{"agent", "chat", "hello-world", "hi"}, []string{"agent", "chat", "hello-world", "hi"}, ""},
	} {
		kept, ctx, err := extractContext(tc.argv)
		if err != nil {
			t.Fatalf("%v: %v", tc.argv, err)
		}
		if !reflect.DeepEqual(kept, tc.wantKept) || ctx != tc.wantCtx {
			t.Errorf("%v -> %v, %q; want %v, %q", tc.argv, kept, ctx, tc.wantKept, tc.wantCtx)
		}
	}
	// An empty value must be refused rather than falling through to KUBE_CTX
	// or the default: `--context=$CTX` with an unset CTX is someone aiming at
	// a cluster they cannot name.
	for _, argv := range [][]string{
		{"up", "--context"},
		{"up", "--context="},
		{"up", "--context", "   "},
	} {
		if _, _, err := extractContext(argv); err == nil {
			t.Errorf("%v: an empty --context must be refused, not treated as unset", argv)
		}
	}
}

// The message that is joined back together is the operator's question.
func TestChatMessageIsJoined(t *testing.T) {
	if got := joinArgs([]string{"what", "pods", "are", "running?"}); got != "what pods are running?" {
		t.Errorf("joined %q", got)
	}
}

// `--json` must work where an operator actually types it: at the END, after
// the agent and the question. flag.Parse stops at the first non-flag
// argument, so the naive form appended "--json" to the QUESTION and printed
// the readable view anyway — the flag looked ignored and the agent was asked
// something slightly different. Caught on a live cluster, not in review.
func TestChatJSONFlagIsHonouredAfterThePositionals(t *testing.T) {
	for _, args := range [][]string{
		{"--json", "hello-world", "Who are you?"},
		{"hello-world", "Who are you?", "--json"},
		{"hello-world", "--json", "Who are you?"},
	} {
		fs := newFlagSet("agent chat")
		asJSON := fs.Bool("json", false, "")
		rest, err := parseInterspersed(fs, args)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !*asJSON {
			t.Errorf("%v: --json was not honoured", args)
		}
		if len(rest) == 0 || rest[0] != "hello-world" {
			t.Errorf("%v: agent name lost, got %v", args, rest)
		}
		if got := joinArgs(rest[1:]); got != "Who are you?" {
			t.Errorf("%v: the question must not absorb the flag, got %q", args, got)
		}
	}
}

func TestUsageNamesEveryCommand(t *testing.T) {
	for _, command := range []string{"ctx", "up", "agent create", "agent chat", "status", "down", "version"} {
		if !strings.Contains(usage, command) {
			t.Errorf("usage does not mention %q", command)
		}
	}
}

// Milestone 3's verbs, one test per ARGUMENT ORDER.
//
// This is the trap milestone 1 fell into and paid for: Go's `flag` stops at
// the first non-flag word, so `kmx approve <id> --uses 1` would parse no
// flags at all and mint an UNBOUNDED grant — or, worse for the operator,
// report a usage error about the id it had just refused to read. Every verb
// below takes its positional first, because that is how people type them.

func TestParseUse(t *testing.T) {
	for _, tc := range []struct {
		name       string
		argv       []string
		wantPreset string
		wantAgent  string
		wantErr    bool
	}{
		{name: "the preset alone", argv: []string{"ollama"}, wantPreset: "ollama", wantAgent: "hello-world"},
		{name: "agent after the preset", argv: []string{"anthropic", "--agent", "billing"},
			wantPreset: "anthropic", wantAgent: "billing"},
		{name: "agent before the preset", argv: []string{"--agent", "billing", "anthropic"},
			wantPreset: "anthropic", wantAgent: "billing"},
		{name: "inline value", argv: []string{"anthropic", "--agent=billing"},
			wantPreset: "anthropic", wantAgent: "billing"},
		{name: "no preset", argv: nil, wantErr: true},
		{name: "two presets", argv: []string{"ollama", "anthropic"}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			preset, opt, err := parseUse(tc.argv)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseUse(%v) was accepted", tc.argv)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if preset != tc.wantPreset || opt.Agent != tc.wantAgent {
				t.Errorf("got preset %q agent %q, want %q / %q", preset, opt.Agent, tc.wantPreset, tc.wantAgent)
			}
		})
	}
}

func TestParseBudget(t *testing.T) {
	for _, tc := range []struct {
		name       string
		argv       []string
		wantCred   string
		wantCents  any
		wantTokens any
		wantErr    bool
	}{
		// No flags at all CLEARS both caps — `make budget` with no CAP_*,
		// which CI uses to open the budget back up mid-run.
		{name: "no arguments", argv: nil, wantCred: "hello-world", wantCents: nil, wantTokens: nil},
		{name: "caps after the credential", argv: []string{"demo", "--cents", "100", "--tokens", "-"},
			wantCred: "demo", wantCents: int64(100), wantTokens: nil},
		{name: "caps before the credential", argv: []string{"--tokens", "1", "demo"},
			wantCred: "demo", wantCents: nil, wantTokens: int64(1)},
		{name: "caps on both sides", argv: []string{"--cents", "5", "demo", "--tokens", "7"},
			wantCred: "demo", wantCents: int64(5), wantTokens: int64(7)},
		{name: "a dash is no cap", argv: []string{"demo", "--cents", "-", "--tokens", "-"},
			wantCred: "demo", wantCents: nil, wantTokens: nil},
		{name: "zero is a cap, not an absence", argv: []string{"demo", "--cents", "0"},
			wantCred: "demo", wantCents: int64(0), wantTokens: nil},
		{name: "a negative cap", argv: []string{"demo", "--cents", "-5"}, wantErr: true},
		{name: "a non-numeric cap", argv: []string{"demo", "--tokens", "lots"}, wantErr: true},
		{name: "two credentials", argv: []string{"a", "b"}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cred, cents, tokens, err := parseBudget(tc.argv, "hello-world")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseBudget(%v) was accepted", tc.argv)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cred != tc.wantCred {
				t.Errorf("credential %q, want %q", cred, tc.wantCred)
			}
			if !samePtr(cents, tc.wantCents) {
				t.Errorf("--cents %v, want %v", show(cents), tc.wantCents)
			}
			if !samePtr(tokens, tc.wantTokens) {
				t.Errorf("--tokens %v, want %v", show(tokens), tc.wantTokens)
			}
		})
	}
}

func TestParseApprove(t *testing.T) {
	const id = "00000000-0000-4000-8000-000000000001"
	for _, tc := range []struct {
		name                          string
		argv                          []string
		wantTTL, wantUses, wantAmount any
		wantErr                       bool
	}{
		// The order CI and the Makefile actually produce.
		{name: "bounds after the id", argv: []string{id, "--ttl", "10m", "--uses", "1", "--amount", "-"},
			wantTTL: int64(600), wantUses: int64(1), wantAmount: nil},
		{name: "bounds before the id", argv: []string{"--uses", "1", id},
			wantTTL: nil, wantUses: int64(1), wantAmount: nil},
		{name: "bounds on both sides", argv: []string{"--ttl", "1d", id, "--amount", "1000000"},
			wantTTL: int64(86400), wantUses: nil, wantAmount: int64(1000000)},
		{name: "inline values", argv: []string{id, "--ttl=2h"},
			wantTTL: int64(7200), wantUses: nil, wantAmount: nil},
		{name: "no bounds parses; the refusal is CheckBounds'", argv: []string{id},
			wantTTL: nil, wantUses: nil, wantAmount: nil},
		{name: "no id", argv: []string{"--ttl", "10m"}, wantErr: true},
		{name: "a bad ttl", argv: []string{id, "--ttl", "5w"}, wantErr: true},
		{name: "a bad use count", argv: []string{id, "--uses", "many"}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ttl, uses, amount, err := parseApprove(tc.argv)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseApprove(%v) was accepted", tc.argv)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != id {
				t.Errorf("id %q", got)
			}
			if !samePtr(ttl, tc.wantTTL) || !samePtr(uses, tc.wantUses) || !samePtr(amount, tc.wantAmount) {
				t.Errorf("bounds ttl=%v uses=%v amount=%v, want %v/%v/%v",
					show(ttl), show(uses), show(amount), tc.wantTTL, tc.wantUses, tc.wantAmount)
			}
		})
	}
}

func TestParseRequest(t *testing.T) {
	// The filing credential's default depends on the KIND — the Makefile's
	// REQ_CRED rule — so a tool request filed against the chat credential
	// would ask the wrong subject for permission.
	for _, tc := range []struct {
		name                            string
		argv                            []string
		wantCred, wantKind, wantSubject string
		wantArgs                        map[string]any
		wantErr                         bool
	}{
		{name: "a tool request defaults to the tools credential",
			argv:     []string{"tool", "k8s_get_events"},
			wantCred: "hello-tools", wantKind: "tool", wantSubject: "k8s_get_events"},
		{name: "a budget request defaults to the chat credential",
			argv:     []string{"budget", "tokens"},
			wantCred: "hello-world", wantKind: "budget", wantSubject: "tokens"},
		{name: "an explicit credential wins",
			argv:     []string{"tool", "k8s_get_events", "--credential", "ap-agent"},
			wantCred: "ap-agent", wantKind: "tool", wantSubject: "k8s_get_events"},
		{name: "the call comes after the subject",
			argv:     []string{"tool", "k8s_get_events", "--args", `{"namespace": "default"}`},
			wantCred: "hello-tools", wantKind: "tool", wantSubject: "k8s_get_events",
			wantArgs: map[string]any{"namespace": "default"}},
		{name: "the call comes before the subject",
			argv:     []string{"--args", `{"namespace": "default"}`, "tool", "k8s_get_events"},
			wantCred: "hello-tools", wantKind: "tool", wantSubject: "k8s_get_events",
			wantArgs: map[string]any{"namespace": "default"}},
		{name: "only the kind", argv: []string{"tool"}, wantErr: true},
		{name: "nothing", argv: nil, wantErr: true},
		{name: "args that are not an object", argv: []string{"tool", "x", "--args", `["a"]`}, wantErr: true},
		{name: "args that are not JSON", argv: []string{"tool", "x", "--args", `{namespace: default}`}, wantErr: true},
		{name: "args with a second document", argv: []string{"tool", "x", "--args", `{"a":1} {"b":2}`}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cred, kind, subject, args, err := parseRequest(tc.argv, "hello-world", "hello-tools")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseRequest(%v) was accepted", tc.argv)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cred != tc.wantCred || kind != tc.wantKind || subject != tc.wantSubject {
				t.Errorf("got %q %q %q, want %q %q %q", cred, kind, subject,
					tc.wantCred, tc.wantKind, tc.wantSubject)
			}
			if tc.wantArgs == nil {
				if args != nil {
					t.Errorf("an omitted call became %v — that is 'any call', not 'no arguments'", args)
				}
				return
			}
			for k, v := range tc.wantArgs {
				if fmt.Sprint(args[k]) != fmt.Sprint(v) {
					t.Errorf("--args[%q] = %v, want %v", k, args[k], v)
				}
			}
		})
	}
}

func TestParseTools(t *testing.T) {
	for _, tc := range []struct {
		name      string
		argv      []string
		wantVerb  string
		wantCred  string
		wantTools string
		wantAgent string
		wantFirst string
		wantErr   bool
	}{
		{name: "govern with its defaults", argv: []string{"govern"},
			wantVerb: "govern", wantCred: "hello-tools"},
		{name: "govern with an allowlist", argv: []string{"govern", "--tools", "a,b"},
			wantVerb: "govern", wantCred: "hello-tools", wantTools: "a,b"},
		{name: "govern with the empty allowlist", argv: []string{"govern", "--tools", "-"},
			wantVerb: "govern", wantCred: "hello-tools", wantTools: "-"},
		{name: "govern for another credential", argv: []string{"govern", "--credential", "hello-github", "--agent", "gh"},
			wantVerb: "govern", wantCred: "hello-github", wantAgent: "gh"},
		{name: "govern takes no positional", argv: []string{"govern", "a,b"}, wantErr: true},
		{name: "ungovern", argv: []string{"ungovern"}, wantVerb: "ungovern", wantCred: "hello-tools"},
		// `ungovern` re-applies ONE committed manifest, which names one
		// agent — so an --agent it could not honour is not spellable, and
		// the App refuses a non-default one set any other way.
		{name: "ungovern does not take --agent", argv: []string{"ungovern", "--agent", "other"}, wantErr: true},
		{name: "allow: the list first, the flag after", argv: []string{"allow", "a,b", "--credential", "demo"},
			wantVerb: "allow", wantCred: "demo", wantFirst: "a,b"},
		{name: "allow: the flag first, the list after", argv: []string{"allow", "--credential", "demo", "a,b"},
			wantVerb: "allow", wantCred: "demo", wantFirst: "a,b"},
		{name: "allow needs a list", argv: []string{"allow"}, wantErr: true},
		{name: "allowlist defaults to CRED_TOOLS", argv: []string{"allowlist"},
			wantVerb: "allowlist", wantCred: "hello-tools"},
		{name: "allowlist takes a credential positionally", argv: []string{"allowlist", "hello-github"},
			wantVerb: "allowlist", wantCred: "hello-github", wantFirst: "hello-github"},
		{name: "allowlist takes it as a flag too", argv: []string{"allowlist", "--credential", "hello-github"},
			wantVerb: "allowlist", wantCred: "hello-github"},
		{name: "no verb", argv: nil, wantErr: true},
		{name: "an unknown verb", argv: []string{"revoke"}, wantErr: true},
		// `--tools` belongs to govern; on `allow` the list is positional.
		{name: "allow does not take --tools", argv: []string{"allow", "--tools", "a"}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verb, opt, positional, err := parseTools(tc.argv, "hello-tools")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseTools(%v) was accepted: %s %+v %v", tc.argv, verb, opt, positional)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if verb != tc.wantVerb || opt.Credential != tc.wantCred {
				t.Errorf("verb %q credential %q, want %q / %q", verb, opt.Credential, tc.wantVerb, tc.wantCred)
			}
			if opt.Tools != tc.wantTools {
				t.Errorf("--tools %q, want %q", opt.Tools, tc.wantTools)
			}
			if opt.Agent != tc.wantAgent {
				t.Errorf("--agent %q, want %q", opt.Agent, tc.wantAgent)
			}
			first := ""
			if len(positional) > 0 {
				first = positional[0]
			}
			if first != tc.wantFirst {
				t.Errorf("positional %q, want %q", first, tc.wantFirst)
			}
		})
	}
}

func TestParseBackupAndMetrics(t *testing.T) {
	if file, err := parseOptionalFile("backup", nil); err != nil || file != "" {
		t.Errorf("bare backup: %q, %v", file, err)
	}
	if file, err := parseOptionalFile("backup", []string{"ci-backup.sql"}); err != nil || file != "ci-backup.sql" {
		t.Errorf("backup <file>: %q, %v", file, err)
	}
	if _, err := parseOptionalFile("backup", []string{"a", "b"}); err == nil {
		t.Error("backup took two files")
	}
	if pod, err := parseMetrics(nil); err != nil || pod != "" {
		t.Errorf("bare metrics: %q, %v", pod, err)
	}
	if pod, err := parseMetrics([]string{"--pod", "kaimahi-proxy-1"}); err != nil || pod != "kaimahi-proxy-1" {
		t.Errorf("metrics --pod: %q, %v", pod, err)
	}
	// The pod is a FLAG, not a positional: `kmx metrics kaimahi-proxy-1` is
	// a mistake, and reading it as the pod would hide the typo.
	if _, err := parseMetrics([]string{"kaimahi-proxy-1"}); err == nil {
		t.Error("metrics took a positional pod")
	}
}

// samePtr compares an optional bound against an expected value, where nil
// means "not set" — the difference between "no cap" and "a cap of zero".
func samePtr(got *int64, want any) bool {
	if want == nil {
		return got == nil
	}
	return got != nil && *got == want.(int64)
}

func show(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}
