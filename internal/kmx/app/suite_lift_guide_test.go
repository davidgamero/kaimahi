package app

import (
	"context"
	"encoding/json"
	"github.com/charmbracelet/x/ansi"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite/workspace"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

func TestGuidedLiftUsesCopiedSelectorsAndSharedPlanApply(t *testing.T) {
	a, p, f := quickstartSuiteApplyFixture(t)
	r, err := a.reviewSuiteWorkspace(t.Context(), p.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	e := suiteConsoleEntry{Name: r.name(), Root: r.root, Review: r, Reviewed: true}
	g := &suiteLiftGuide{entry: e, version: suitePublicationChoice{Reference: p.Plan.options.Reference, Layout: p.Plan.options.FromLayout, Evidence: p.Evidence}, env: agentTUIEnvironment{Name: "kind-test", Local: true}, namespace: OrkaNamespace, provider: "provider:inference"}
	g.server, err = a.consoleCreateTarget(t.Context(), g.env)
	if err != nil {
		t.Fatal(err)
	}
	m := &suiteConsole{a: a, ctx: t.Context(), workers: &workers, events: make(chan suiteConsoleProgress, 256), input: textinput.New(), liftGuide: g, entries: []suiteConsoleEntry{e}, authoringView: true}
	before := f.taskCreates(t)
	consoleFinish(t, m, m.guidePlan())
	if m.plan == nil || m.liftGuide != nil || m.plan.options.ToContext != "kind-test" || m.plan.options.Inference != "provider:inference" {
		t.Fatal("guided selection not passed to shared planner")
	}
	if f.taskCreates(t) != before {
		t.Fatal("planning executed cases")
	}
	consoleFinish(t, m, m.submit(suiteConsoleForm{action: "apply", values: []string{"kind-test"}}))
	if m.entry().Deployment == "" || f.taskCreates(t) != before {
		t.Fatal("shared apply did not retain no-send placement")
	}
	workers.Wait()
}

func TestGuidedLiftMissingProofOffersForwardActionsWithoutSends(t *testing.T) {
	a, p, f := quickstartSuiteApplyFixture(t)
	var workers sync.WaitGroup
	g := &suiteLiftGuide{version: suitePublicationChoice{Reference: p.Plan.options.Reference, Layout: p.Plan.options.FromLayout, Evidence: "/absent"}, env: agentTUIEnvironment{Name: "kind-test"}, namespace: OrkaNamespace, provider: "provider:inference"}
	var err error
	g.server, err = a.consoleCreateTarget(t.Context(), g.env)
	if err != nil {
		t.Fatal(err)
	}
	m := &suiteConsole{a: a, ctx: t.Context(), workers: &workers, events: make(chan suiteConsoleProgress, 256), input: textinput.New(), liftGuide: g}
	before := f.taskCreates(t)
	consoleFinish(t, m, m.guidePlan())
	if m.plan != nil || m.liftGuide.stage != "proof-refused" || f.taskCreates(t) != before {
		t.Fatal("missing proof crossed planning boundary")
	}
	if len(m.liftGuide.choices) < 3 {
		t.Fatal("no useful recovery choices")
	}
	workers.Wait()
}

func TestGuidedLiftNamespaceDiscoveryFiltersDeletingAndPreservesIdentity(t *testing.T) {
	a, _, _, _ := extractionFixture(t)
	a.captureCommand = func(_ context.Context, c *exec.Cmd) error {
		if !strings.Contains(strings.Join(c.Args, " "), "get namespaces") {
			t.Fatalf("unexpected mutation/read %v", c.Args)
		}
		_, err := io.WriteString(c.Stdout, `{"items":[{"metadata":{"name":"default"}},{"metadata":{"name":"orka-system"}},{"metadata":{"name":"gone","deletionTimestamp":"2026-10-15T00:00:00Z"}}]}`)
		return err
	}
	g := &suiteLiftGuide{env: agentTUIEnvironment{Name: "kind-test"}, namespace: OrkaNamespace}
	if err := g.namespaces(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if len(g.choices) != 2 || g.choices[0].value != OrkaNamespace {
		t.Fatalf("namespace choices %+v", g.choices)
	}
}

func TestGuidedLiftExplicitConsentAndFuzzySelection(t *testing.T) {
	g := &suiteLiftGuide{}
	g.input("publish", "Publish?", "", "publish")
	m := &suiteConsole{input: textinput.New(), liftGuide: g}
	m.input.SetValue("no")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || m.running {
		t.Fatal("publication started without consent")
	}
	g.pick("builder", "Builder", []liftGuideChoice{{label: "alpha", value: "alpha"}, {label: "production-builder", value: "prod"}})
	m.Update(tea.KeyPressMsg{Code: 'p', Text: "prdb"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if g.builder != "prod" || g.stage != "publish" || m.running {
		t.Fatal("builder choice failed or wrote before confirmation")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.liftGuide != nil {
		t.Fatal("cancel left workflow active")
	}
}

type guideRoundTrip func(*http.Request) (*http.Response, error)

func (f guideRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestGuidedRegistryCheckReadOnlyAndRejectsGenericHTML(t *testing.T) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	valid := false
	http.DefaultTransport = guideRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.Path != "/v2/" || r.Header.Get("Authorization") != "" {
			t.Fatal("connectivity probe sent credentials/write")
		}
		h := http.Header{}
		if valid {
			h.Set("Docker-Distribution-Api-Version", "registry/2.0")
		}
		return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	if err := checkGuideRegistry(t.Context(), "registry.example"); err == nil {
		t.Fatal("HTML accepted as registry")
	}
	valid = true
	if err := checkGuideRegistry(t.Context(), "registry.example"); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"user:pass@registry.example", "registry.example/path", "registry.example?x=1"} {
		if err := checkGuideRegistry(t.Context(), host); err == nil {
			t.Fatal("invalid host accepted", host)
		}
	}
}

func TestGuidedImageOptionsDoNotMixNativeInference(t *testing.T) {
	g := &suiteLiftGuide{version: suitePublicationChoice{Image: true, Reference: "r@sha256:abc", Environment: "bindings"}, provider: "provider:existing"}
	opt := g.options()
	if opt.Inference != "" || opt.Environment != "bindings" {
		t.Fatal("mixed profile selectors")
	}
	raw, _ := json.Marshal(opt)
	if strings.Contains(string(raw), "provider:existing") {
		t.Fatal("native selector leaked into image operation")
	}
}

func TestGuidedImageGeneratesReviewedBindingsWithoutClusterWrites(t *testing.T) {
	t.Setenv("KMX_HOME", t.TempDir())
	a, _, _, _ := extractionFixture(t)
	root := filepath.Join(t.TempDir(), "image")
	if err := workspace.CreateExclusive(t.Context(), root, workspace.CreateOptions{Name: "guide-image", Execution: "image-http", ContextTokens: 8192, OutputTokens: 1024}, "Answer briefly."); err != nil {
		t.Fatal(err)
	}
	r, err := a.reviewSuiteWorkspace(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	a.captureCommand = func(_ context.Context, c *exec.Cmd) error {
		if !strings.Contains(strings.Join(c.Args, " "), "get namespace kube-system") {
			t.Fatalf("binding generation wrote/probed unexpected resource: %v", c.Args)
		}
		_, err := io.WriteString(c.Stdout, `{"metadata":{"uid":"cluster-uid"}}`)
		return err
	}
	g := &suiteLiftGuide{entry: suiteConsoleEntry{Name: r.name(), Root: r.root, Review: r}, env: agentTUIEnvironment{Name: "kind-test"}, namespace: "demo", model: "model-a", endpoint: "https://model.example/v1", secret: "model-key", secretKey: "api-key", authSecret: "agent-auth", pullSecret: "registry-pull"}
	if err := g.capabilityReview(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(g.capabilities, "8192") {
		t.Fatal("operator did not see exact capabilities")
	}
	if err := g.imageBindings(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	env, err := ReadSuiteEnvironment(g.version.Environment)
	if err != nil {
		t.Fatal(err)
	}
	member := env.Members["guide-image"]
	if env.Context != "kind-test" || env.ClusterUID != "cluster-uid" || member.Inference.Model != "model-a" || member.Inference.Credential.Name != "model-key" || member.Inputs["agent-auth"].SecretRef.Name != "agent-auth" || len(env.ImagePullSecrets) != 1 {
		t.Fatalf("bindings lost selections: %+v", env)
	}
	info, err := os.Stat(g.version.Environment)
	if err != nil || info.Mode().Perm()&0077 != 0 {
		t.Fatal("bindings not private")
	}
}

func TestGuidedLiftMouseAndModalDoNotDispatchThroughOverlay(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	g := &suiteLiftGuide{}
	g.pick("builder", "Choose builder", []liftGuideChoice{{label: "selected-builder", value: "selected-builder"}})
	m := &suiteConsole{input: textinput.New(), liftGuide: g, width: 100, height: 32}
	r := suiteMouseHit(t, m, "guide", 0)
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	if !strings.Contains(lines[r.y], "selected-builder") {
		t.Fatal("guide mouse mapping differs from rendered row")
	}
	suiteMouseClick(m, 1, 1)
	if g.stage != "builder" {
		t.Fatal("overlay leaked click")
	}
	suiteMouseClick(m, r.x, r.y)
	if g.stage != "publish" || g.builder != "selected-builder" || m.running {
		t.Fatal("selection mutated before publication confirmation")
	}
}

func TestRuntimeWarningPrecedesActionsAndUsesRed(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	g := &suiteLiftGuide{stage: "runtime", title: "Runtime prerequisites", note: "Installed Orka does not satisfy the packaged runtime", choices: []liftGuideChoice{{label: "Check a separate installation", value: "isolated"}}}
	m := &suiteConsole{width: 120, height: 40, liftGuide: g}
	view := m.View().Content
	plain := ansi.Strip(view)
	if strings.Index(plain, "Warning:") >= strings.Index(plain, "Check a separate installation") || !strings.Contains(view, "255;107;107") {
		t.Fatal("runtime warning is not red above actions")
	}
	t.Setenv("NO_COLOR", "1")
	plain = m.View().Content
	if plain != ansi.Strip(plain) || !strings.Contains(plain, "Warning:") {
		t.Fatal("NO_COLOR warning unavailable")
	}
}

func TestRuntimeAlertAcknowledgementDoesNotInstall(t *testing.T) {
	for _, runtime := range []string{"orka", "kagent"} {
		t.Run(runtime, func(t *testing.T) {
			g := &suiteLiftGuide{}
			g.showRuntimeAlert(SuiteRuntimeReport{Runtime: runtime, Required: runtime + " pinned version", Context: "remote", Namespace: "agents", State: "absent", Detail: "Runtime missing"})
			m := &suiteConsole{liftGuide: g}
			if len(g.choices) != 1 || g.choices[0].label != "Okay" || !strings.Contains(g.note, "Required:") {
				t.Fatal("missing prerequisite acknowledgement")
			}
			_, cmd := m.updateLiftGuide(tea.KeyPressMsg{Code: tea.KeyEnter})
			if cmd != nil || m.running || g.stage != "runtime" {
				t.Fatal("acknowledgement performed work")
			}
			install := false
			for _, c := range g.choices {
				if c.value == "install" || c.value == "isolated" {
					install = true
				}
			}
			if install != (runtime == "orka") {
				t.Fatal("unsupported runtime installation offered")
			}
		})
	}
}
