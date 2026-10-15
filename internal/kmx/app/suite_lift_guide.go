package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	spec "github.com/kaimahi-agents/kaimahi/agentsuite"
	core "github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite"
	suiteoras "github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite/oras"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/imagelift"
	agentruntime "github.com/kaimahi-agents/kaimahi/internal/kmx/runtime"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/scaffold"
	"oras.land/oras-go/v2/registry"
)

type liftGuideChoice struct {
	label, detail, value string
	env                  agentTUIEnvironment
	provider             consoleInferenceSource
	endpoint, group      string
	acr                  suiteAzureRegistry
	aks                  suiteAzureCluster
}

// This UI owns copied selections, never deployment authority. All artifact,
// evaluation and placement effects are dispatched through shared operations.
type suiteLiftGuide struct {
	runtimeReport                                           *SuiteRuntimeReport
	entry                                                   suiteConsoleEntry
	version                                                 suitePublicationChoice
	stage, title, query, note                               string
	choices                                                 []liftGuideChoice
	selection                                               int
	value, confirm                                          string
	env                                                     agentTUIEnvironment
	namespace, provider, endpoint, model, secret, secretKey string
	registry, repository, builder, authSecret, pullSecret   string
	acr                                                     suiteAzureRegistry
	aks                                                     suiteAzureCluster
	server                                                  string
	work                                                    string
	judgeURL, judgeKeyEnv                                   string
	subscription, group, account, keyEnv                    string
	promote                                                 bool
	capabilities                                            string
}

func (g *suiteLiftGuide) pick(stage, title string, choices []liftGuideChoice) {
	g.stage, g.title, g.choices = stage, title, choices
	g.query = ""
	g.selection = 0
	g.value = ""
	g.confirm = ""
}
func (g *suiteLiftGuide) input(stage, title, value, confirm string) {
	g.pick(stage, title, nil)
	g.value = value
	g.confirm = confirm
}
func (g *suiteLiftGuide) matches() []int {
	items := make([]chatPickerItem, len(g.choices))
	for i, c := range g.choices {
		items[i] = chatPickerItem{name: c.label, detail: c.detail}
	}
	return (chatPicker{items: items, query: g.query, searchEnabled: true}).matches()
}

func (m *suiteConsole) startLiftGuide(e suiteConsoleEntry, v suitePublicationChoice) tea.Cmd {
	if m.refreshCancel != nil {
		m.refreshCancel()
	}
	m.refreshGeneration++
	m.refreshing = false
	m.publications = nil
	m.liftAfterPublish = ""
	g := &suiteLiftGuide{entry: e, version: v, namespace: valueOr(m.opt.Namespace, OrkaNamespace)}
	g.pick("destination", "Choose where to Lift", nil)
	m.liftGuide = g
	return m.guideJob(func(ctx context.Context, a *App, g *suiteLiftGuide) error { return g.destinations(ctx, a) })
}

func (m *suiteConsole) guideJob(fn func(context.Context, *App, *suiteLiftGuide) error) tea.Cmd {
	m.guideRetry = fn
	g := *m.liftGuide
	g.choices = append([]liftGuideChoice(nil), g.choices...)
	return m.job(func(ctx context.Context, a *App) suiteConsoleResult {
		err := fn(ctx, a, &g)
		if err != nil {
			stage := g.stage
			choices := []liftGuideChoice{{label: "Choose destination again", value: "destination"}, {label: "Advanced / inspect existing outcomes", value: "advanced"}}
			switch stage {
			case "destination", "aks", "namespace", "provider", "registry", "registry-host", "registry-access", "repository", "builder", "foundry-subscriptions", "foundry-accounts", "foundry-deployments", "inference-key", "auth", "pull":
				choices = append([]liftGuideChoice{{label: "Retry discovery / check", value: "retry"}}, choices...)
			}
			g.pick("error", "Step could not complete", choices)
			g.note = err.Error() + ". Completed writes may remain; sends and ambiguous creates are never retried automatically."
		}
		return suiteConsoleResult{guide: &g, err: err}
	})
}

func (g *suiteLiftGuide) destinations(ctx context.Context, a *App) error {
	envs, err := a.agentTUIEnvironments(ctx)
	if err != nil && len(envs) == 0 {
		return err
	}
	choices := []liftGuideChoice{}
	for _, env := range envs {
		choices = append(choices, liftGuideChoice{label: environmentLabel(env), detail: env.Server, value: "context", env: env})
	}
	choices = a.enrichLiftDestinations(ctx, choices)
	choices = append(choices, liftGuideChoice{label: "Discover AKS clusters", value: "aks", detail: "Active Azure subscription; confirm private access import"}, liftGuideChoice{label: "Advanced: offline layout / explicit bindings", value: "advanced"})
	g.pick("destination", "Choose where to Lift", choices)
	if err != nil {
		g.note = err.Error()
	}
	return nil
}

func (g *suiteLiftGuide) namespaces(ctx context.Context, a *App) error {
	raw, err := g.env.app(a).orkaCapture(ctx, nil, "get", "namespaces", "-o", "json")
	if err != nil {
		return err
	}
	var list objectList[struct {
		Metadata struct {
			Name              string
			DeletionTimestamp *time.Time
		}
	}]
	if err := decodeConsoleList(raw, &list); err != nil {
		return err
	}
	var choices []liftGuideChoice
	for _, ns := range list.Items {
		if ns.Metadata.DeletionTimestamp == nil {
			choices = append(choices, liftGuideChoice{label: ns.Metadata.Name, value: ns.Metadata.Name})
		}
	}
	if len(choices) == 0 {
		return errors.New("no active namespaces visible; create a destination namespace with your cluster tooling, then retry")
	}
	sort.SliceStable(choices, func(i, j int) bool {
		if choices[i].value == g.namespace && choices[j].value != g.namespace {
			return true
		}
		if choices[j].value == g.namespace {
			return false
		}
		return choices[i].label < choices[j].label
	})
	g.pick("namespace", "Choose destination namespace", choices)
	g.note = "Existing namespaces only. Inference and image Secrets must be in this namespace."
	return nil
}

func (g *suiteLiftGuide) runtimeAndProviders(ctx context.Context, a *App) error {
	if !g.image() {
		opt := g.options()
		if opt.Reference == "" && g.entry.Review != nil {
			opt.Reference = "workspace@" + g.entry.Review.digest()
			opt.FromLayout = g.entry.Review.prepared.Path
		}
		report, err := g.env.app(a).CheckSuiteRuntime(ctx, opt)
		if err != nil {
			return err
		}
		if report.State != "ready" {
			g.showRuntimeAlert(report)
			return nil
		}
	}
	return g.providers(ctx, a)
}

func (g *suiteLiftGuide) showRuntimeAlert(report SuiteRuntimeReport) {
	g.runtimeReport = &report
	g.pick("runtime-alert", "Runtime setup required", []liftGuideChoice{{label: "Okay", value: "okay", detail: "Acknowledge requirements and see setup options; no changes yet"}})
	g.note = "Required: " + report.Required + "\nDestination: " + report.Context + " / " + report.Namespace + "\n" + report.Detail
	if report.Runtime == "orka" {
		g.note += "\nInstall or prepare the pinned Orka controller and worker configuration. An incompatible existing installation requires operator resolution or supported isolated setup."
	}
	if report.Runtime == "kagent" {
		g.note += "\nPrepare exact kagent v0.10.3 Go/linux-amd64, unsecure authentication and author-owned bindings. KMX has no kagent installer; use the root eval/lift --bindings workflow."
	}
	if report.Next != "" {
		g.note += "\nNext: " + report.Next
	}
}

func (g *suiteLiftGuide) runtimeActions() {
	r := g.runtimeReport
	if r == nil {
		return
	}
	choices := []liftGuideChoice{{label: "Check runtime again", value: "retry"}, {label: "Choose another destination", value: "destination"}}
	if r.Runtime == "orka" {
		choices = append(choices, liftGuideChoice{label: "Check a separate KMX-managed Orka installation", value: "isolated", detail: "New namespace; compatible CRDs and disjoint pinned controllers required"})
		if r.State == "absent" {
			choices = append([]liftGuideChoice{{label: "Set up pinned Orka runtime", value: "install", detail: "Separate guarded chart/CRD/result-reader installation"}}, choices...)
		}
	}
	g.pick("runtime", "Runtime prerequisite: "+r.State, choices)
	g.note = r.Detail
}

func (g *suiteLiftGuide) image() bool {
	return g.version.Image || g.entry.Image || g.entry.Review != nil && g.entry.Review.image != nil
}
func (g *suiteLiftGuide) options() ReleaseLiftOptions {
	opt := ReleaseLiftOptions{Reference: g.version.Reference, FromLayout: g.version.Layout, PlainHTTP: g.version.PlainHTTP, ToContext: g.env.Name, ToNamespace: g.namespace, Inference: g.provider, Evidence: g.version.Evidence, Environment: g.version.Environment, Promote: g.promote}
	if g.image() {
		opt.Inference = ""
	} else {
		opt.Environment = ""
	}
	return opt
}

func (g *suiteLiftGuide) providers(ctx context.Context, a *App) error {
	choices, err := a.suiteCreateProviders(ctx, g.env, g.namespace)
	var picks []liftGuideChoice
	if err == nil {
		for _, c := range choices {
			if !g.image() && g.entry.Review != nil && c.source.Model != g.entry.Review.model() {
				continue
			}
			picks = append(picks, liftGuideChoice{label: c.label, detail: c.detail, value: "provider", provider: c.source})
		}
	}
	picks = append(picks, liftGuideChoice{label: "Connect Foundry inference", value: "foundry", detail: "Discover deployment; explicitly set up Provider/Secret if needed"}, liftGuideChoice{label: "Refresh available Providers", value: "retry"})
	if g.image() {
		picks = append(picks, liftGuideChoice{label: "Advanced: enter OpenAI-compatible connection", value: "manual", detail: "For image workloads without Orka Providers"})
	}
	g.pick("provider", "Choose model / inference connection", picks)
	g.note = "Environment: " + environmentLabel(g.env) + " / " + g.namespace + ". Connections supply the endpoint and credentials used for model calls. Native model must match the packaged model."
	if err != nil {
		g.note = "Provider discovery unavailable. Connect Foundry (requires Orka) or use image connection entry; this is not absence."
	}
	return nil
}

func (g *suiteLiftGuide) registryChoices(ctx context.Context, a *App) error {
	// Azure discovery can fail without blocking non-Azure registries or an
	// already published artifact. Never infer pull access from registry names.
	raw, err := a.liftDiscovery(ctx, "az", "acr", "list", "-o", "json", "--only-show-errors")
	var registries []suiteAzureRegistry
	if err == nil && (json.Unmarshal(raw, &registries) != nil || len(registries) > 256) {
		err = errors.New("invalid registry inventory")
	}
	if err == nil {
		clusters, clusterErr := a.liftDiscovery(ctx, "az", "aks", "list", "-o", "json", "--only-show-errors")
		var aks []suiteAzureCluster
		if clusterErr == nil && json.Unmarshal(clusters, &aks) == nil && len(aks) <= 128 {
			u, _ := url.Parse(g.env.Server)
			for _, cluster := range aks {
				if u != nil && u.Hostname() != "" && (strings.EqualFold(u.Hostname(), cluster.FQDN) || strings.EqualFold(u.Hostname(), cluster.PrivateFQDN)) {
					observed, e := a.connectedSuiteACRs(ctx, suiteAzureSelection{Cluster: cluster, Context: g.env.Name, Registries: registries})
					if e == nil {
						registries = observed.Registries
					} else {
						g.note = "ACR role discovery unavailable; actual node pull remains unproven"
					}
					break
				}
			}
		}
	}
	sort.SliceStable(registries, func(i, j int) bool {
		return strings.HasPrefix(registries[i].Pull, "role observed") && !strings.HasPrefix(registries[j].Pull, "role observed")
	})
	var choices []liftGuideChoice
	for _, acr := range registries {
		choices = append(choices, liftGuideChoice{label: acr.LoginServer, detail: acr.Name + " · " + valueOr(acr.Pull, "pull access unproven"), value: "acr", acr: acr})
	}
	choices = append(choices, liftGuideChoice{label: "Other registry (advanced)", value: "manual", detail: "HTTPS OCI registry hostname; existing Docker credential-store login"})
	g.pick("registry", "Choose publication registry", choices)
	if err != nil {
		g.note = "Azure registry discovery unavailable; check az login or choose another registry."
	} else {
		g.note = "Pull-role candidates first. Roles do not prove publisher authorization or AKS network access."
	}
	return nil
}

func checkGuideRegistry(ctx context.Context, host string) error {
	u, err := url.Parse("https://" + host + "/v2/")
	if err != nil || u.Host != host || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/v2/" {
		return errors.New("enter only an HTTPS registry hostname (optional port)")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return errors.New("registry HTTPS connection failed; check DNS, TLS and network access")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 && response.StatusCode != 401 {
		return fmt.Errorf("registry returned HTTP %d; connectivity check failed", response.StatusCode)
	}
	if response.Header.Get("Docker-Distribution-Api-Version") == "" && !strings.Contains(response.Header.Get("WWW-Authenticate"), "Bearer") {
		return errors.New("endpoint did not identify an OCI registry")
	}
	return nil
}

func (g *suiteLiftGuide) checkRegistry(ctx context.Context) error {
	if err := checkGuideRegistry(ctx, g.registry); err != nil {
		return err
	}
	choices := []liftGuideChoice{{label: "Continue to publication", value: "continue", detail: "Push authorization checked by the real publication; node pull still unproven"}, {label: "Choose another registry", value: "registry"}}
	if g.acr.Name != "" {
		choices = append([]liftGuideChoice{{label: "Log in to selected ACR", value: "login", detail: "Explicit az acr login updates the host Docker credential store"}}, choices...)
	}
	g.pick("registry-access", "Registry HTTPS reachable", choices)
	g.note = "Reachability verified, not write authorization. Use an existing publisher login or log in explicitly. AKS pull is verified only by actual workload observation."
	return nil
}

func (g *suiteLiftGuide) builders(ctx context.Context, a *App) error {
	raw, err := a.azureCapture(ctx, 20*time.Second, "docker", []string{"buildx", "ls", "--format", "{{json .}}"}, "Docker buildx discovery unavailable; start Docker and configure a builder")
	if err != nil {
		return err
	}
	var choices []liftGuideChoice
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var b struct {
			Name, Driver string
			Nodes        []struct{ Status string }
		}
		if json.Unmarshal([]byte(line), &b) != nil || b.Name == "" || seen[b.Name] {
			continue
		}
		if b.Driver != "docker-container" && b.Driver != "remote" && b.Driver != "kubernetes" {
			continue
		}
		ready := false
		for _, node := range b.Nodes {
			if node.Status == "running" {
				ready = true
			}
		}
		if ready {
			seen[b.Name] = true
			choices = append(choices, liftGuideChoice{label: strings.TrimSuffix(b.Name, "*"), detail: b.Driver + " · running", value: strings.TrimSuffix(b.Name, "*")})
		}
	}
	if len(choices) == 0 {
		return errors.New("no running OCI-export-capable buildx builder; create/start a docker-container or remote builder, then retry discovery")
	}
	g.pick("builder", "Choose image builder", choices)
	return nil
}

func (g *suiteLiftGuide) privateWork() error {
	if g.work != "" {
		return nil
	}
	state, err := config.StateDir()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(state, 0700); err != nil {
		return err
	}
	g.work, err = os.MkdirTemp(state, "guided-lift-")
	return err
}

func (g *suiteLiftGuide) imageBindings(ctx context.Context, a *App) error {
	if err := g.privateWork(); err != nil {
		return err
	}
	if g.entry.Review == nil || g.entry.Review.image == nil {
		return errors.New("open matching image source to generate bindings, or use Advanced for external artifacts")
	}
	artifact, err := suiteoras.ReadArtifactLayout(ctx, g.entry.Review.prepared.Path, g.entry.Review.digest())
	if err != nil {
		return err
	}
	files := artifact.Files()
	var suite spec.Suite
	var agent spec.Agent
	if spec.Unmarshal(files["agentsuite.json"], &suite) != nil || len(suite.Agents) != 1 || spec.Unmarshal(files[suite.Agents[0].Path], &agent) != nil || agent.Model.Capabilities == nil {
		return errors.New("image capability declaration unavailable")
	}
	worker := g.env.app(a)
	raw, err := worker.orkaCapture(ctx, nil, "get", "namespace", "kube-system", "-o", "json")
	if err != nil {
		return err
	}
	var cluster struct{ Metadata struct{ UID string } }
	if json.Unmarshal(raw, &cluster) != nil || cluster.Metadata.UID == "" {
		return errors.New("cluster identity unavailable")
	}
	listen := "0.0.0.0"
	inference := &imagelift.InferenceBinding{Model: g.model, Endpoint: g.endpoint, Capabilities: *agent.Model.Capabilities, Evidence: "operator-declared"}
	if g.secret != "" {
		inference.Credential = &spec.SecretKeyRef{Name: g.secret, Key: g.secretKey}
	}
	env := imagelift.Environment{APIVersion: "kaimahi.dev/lift/v1alpha1", Name: g.entry.Review.member(), Context: g.env.Name, ClusterUID: cluster.Metadata.UID, Namespace: g.namespace, Adapter: "http-v1", Platform: core.Platform{OS: "linux", Architecture: "amd64"}, Members: map[string]imagelift.MemberEnvironment{g.entry.Review.member(): {Name: g.entry.Review.member(), Inference: inference, Inputs: map[string]imagelift.InputBinding{"agent-auth": {SecretRef: &spec.SecretKeyRef{Name: g.authSecret, Key: "token"}}, "listen": {Value: &listen}}}}}
	if g.pullSecret != "" {
		env.ImagePullSecrets = []string{g.pullSecret}
	}
	if err := env.Validate(); err != nil {
		return err
	}
	raw, err = json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	g.version.Environment = filepath.Join(g.work, "environment.json")
	return writePrivateAgentFile(g.version.Environment, raw)
}

func (g *suiteLiftGuide) capabilityReview(ctx context.Context, a *App) error {
	if g.entry.Review == nil {
		return errors.New("open image source to review capability declarations or use Advanced bindings")
	}
	artifact, err := suiteoras.ReadArtifactLayout(ctx, g.entry.Review.prepared.Path, g.entry.Review.digest())
	if err != nil {
		return err
	}
	files := artifact.Files()
	var suite spec.Suite
	var member spec.Agent
	if spec.Unmarshal(files["agentsuite.json"], &suite) != nil || len(suite.Agents) != 1 || spec.Unmarshal(files[suite.Agents[0].Path], &member) != nil || member.Model.Capabilities == nil {
		return errors.New("image capabilities unavailable")
	}
	cap := member.Model.Capabilities
	g.capabilities = fmt.Sprintf("API %s · context %d · output %d · streaming %t · tool calling %t", cap.API, cap.ContextTokens, cap.OutputTokens, cap.Streaming, cap.ToolCalling)
	g.pick("capabilities", "Confirm image inference requirements", []liftGuideChoice{{label: "Declare selected endpoint supports these capabilities", value: "confirm", detail: g.capabilities}, {label: "Advanced bindings", value: "advanced"}})
	g.note = "Model: " + g.model + " · " + g.endpoint + ". This is an operator declaration, not model-access proof."
	return nil
}

func (g *suiteLiftGuide) publish(ctx context.Context, a *App) error {
	if g.entry.Review == nil {
		return errors.New("source required for publication")
	}
	if err := g.entry.Review.checkCurrent(ctx); err != nil {
		return err
	}
	ref := g.registry + "/" + g.repository
	parsed, err := registry.ParseReference(ref)
	if err != nil {
		return err
	}
	if err = parsed.ValidateReferenceAsTag(); err != nil {
		return err
	}
	if g.image() {
		pub, err := a.PublishSuiteWorkspace(ctx, g.entry.Root, g.entry.Name, ref, false, SuiteBuildOptions{Builder: g.builder})
		if err != nil {
			return err
		}
		g.version.Reference = pub.Release
		g.version.Image = true
	} else {
		pub, err := a.PushSuiteWorkspace(ctx, g.entry.Root, ref, "", false, false, SuitePushProvenance{})
		if err != nil {
			return err
		}
		g.version.Reference = parsed.Registry + "/" + parsed.Repository + "@" + pub.Descriptor.Digest.String()
	}
	g.version.Layout = ""
	g.version.PlainHTTP = false
	g.version.PublishedAt = time.Now().UTC()
	g.version.SuiteDigest = g.entry.Review.digest()
	g.entry.Reference = g.version.Reference
	g.entry.Image = g.image()
	g.entry.Layout = ""
	g.entry.Published = &g.version
	if err := rememberSuiteConsoleEntry(g.entry); err != nil {
		return fmt.Errorf("published %s but history could not be saved: %w", g.version.Reference, err)
	}
	g.entry.Published = nil
	g.evidenceChoices()
	return nil
}

func (g *suiteLiftGuide) evidenceChoices() {
	var choices []liftGuideChoice
	if g.version.Evidence != "" {
		choices = append(choices, liftGuideChoice{label: "Check saved evaluation proof", detail: g.version.Evidence, value: "proof"})
	}
	if g.entry.Review != nil {
		choices = append(choices, liftGuideChoice{label: "Evaluate this version now", value: "evaluate", detail: "Review all cases, confirm placement/model calls, then export new private proof"})
	}
	choices = append(choices, liftGuideChoice{label: "Import evidence file (advanced)", value: "import"}, liftGuideChoice{label: "Advanced bindings / offline Lift", value: "advanced"})
	g.pick("evidence", "Prove before Lift", choices)
	g.note = "Publication alone is not proof. Existing proof is checked against the exact artifact and destination."
}

func (g *suiteLiftGuide) evaluate(ctx context.Context, a *App) error {
	if err := g.checkTarget(ctx, a); err != nil {
		return err
	}
	r := g.entry.Review
	if r == nil {
		return errors.New("source review required")
	}
	if err := r.checkCurrent(ctx); err != nil {
		return err
	}
	if err := g.privateWork(); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(g.work, "evaluation-")
	if err != nil {
		return err
	}
	out := filepath.Join(dir, "passing.json")
	w := g.env.app(a).withRunContext(ctx)
	w.Cfg.Confirm = g.env.Name
	if g.image() {
		candidate, err := w.prepareImageCandidate(ctx, g.options())
		if err != nil {
			return err
		}
		if candidate.suite.Digest != r.digest() {
			return errors.New("selected published version differs from reviewed source; open its source or import matching proof")
		}
		if err := w.EvaluateImageRelease(ctx, g.options(), out); err != nil {
			return err
		}
	} else {
		_, digest, _ := strings.Cut(g.version.Reference, "@")
		if digest != r.digest() {
			return errors.New("selected version differs from reviewed source; cannot evaluate different source as this artifact")
		}
		p, err := PrepareSuiteWorkspaceEvaluation(ctx, SuiteWorkspaceEvaluationOptions{Workspace: g.entry.Root, ToContext: g.env.Name, ToNamespace: g.namespace, Inference: g.provider, Out: out, JudgeBaseURL: g.judgeURL, JudgeKeyEnv: g.judgeKeyEnv})
		if err != nil {
			return err
		}
		defer p.Close()
		if p.prepared.Fingerprint != r.prepared.Fingerprint {
			return errors.New("reviewed source changed")
		}
		if err := w.EvaluateSuiteWorkspace(ctx, p); err != nil {
			return err
		}
	}
	g.version.Evidence = out
	g.entry.Evidence = out
	g.entry.Reference = g.version.Reference
	g.entry.EnvironmentFile = g.version.Environment
	if err := rememberSuiteConsoleEntry(g.entry); err != nil {
		return err
	}
	g.promote = false
	g.evidenceChoices()
	g.note = "Full evaluation passed. Check the saved proof to review Lift."
	return nil
}

func guideSecretChoices(ctx context.Context, a *App, env agentTUIEnvironment, namespace, key string, pull bool) ([]liftGuideChoice, error) {
	// Only names, type and key names are printed by kubectl, never data values.
	raw, err := env.app(a).orkaCapture(ctx, nil, "-n", namespace, "get", "secrets", "-o", `go-template={{range .items}}{{.metadata.name}}|{{.type}}|{{range $k,$v := .data}}{{$k}};{{end}}{{"\n"}}{{end}}`)
	if err != nil {
		return nil, err
	}
	var choices []liftGuideChoice
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		parts := strings.Split(line, "|")
		if len(parts) != 3 {
			continue
		}
		if pull && parts[1] != "kubernetes.io/dockerconfigjson" {
			continue
		}
		if !pull && !strings.Contains(";"+parts[2], ";"+key+";") {
			continue
		}
		choices = append(choices, liftGuideChoice{label: parts[0], detail: "existing Secret · " + parts[1], value: parts[0]})
	}
	return choices, nil
}

func (g *suiteLiftGuide) authChoices(ctx context.Context, a *App) error {
	choices, err := guideSecretChoices(ctx, a, g.env, g.namespace, "token", false)
	if err != nil {
		return err
	}
	choices = append(choices, liftGuideChoice{label: "Create a new agent authentication Secret", value: "create-auth", detail: "Random token generated locally and sent via stdin; never printed"})
	g.pick("auth", "Choose image agent authentication", choices)
	return nil
}
func (g *suiteLiftGuide) pullChoices(ctx context.Context, a *App) error {
	choices, err := guideSecretChoices(ctx, a, g.env, g.namespace, "", true)
	if err != nil {
		return err
	}
	choices = append([]liftGuideChoice{{label: "Use node / AKS registry identity", value: "", detail: "No imagePullSecret; real workload pull may still fail"}}, choices...)
	g.pick("pull", "Choose image-pull authentication", choices)
	return nil
}

func (g *suiteLiftGuide) createAuth(ctx context.Context, a *App) error {
	if err := g.checkTarget(ctx, a); err != nil {
		return err
	}
	if err := scaffold.ValidateName(g.authSecret); err != nil {
		return err
	}
	w := g.env.app(a).withRunContext(ctx)
	w.Cfg.Confirm = g.env.Name
	if err := w.guardOrkaMutation(ctx, CreateOptions{Name: g.authSecret, Namespace: g.namespace}, "create new image-agent authentication Secret"); err != nil {
		return err
	}
	key, err := randomHex(32)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]string{"name": g.authSecret, "namespace": g.namespace}, "type": "Opaque", "stringData": map[string]string{"token": key}})
	if _, err = w.orkaCapture(ctx, raw, "create", "-f", "-", "-o", "name"); err != nil {
		return errors.New("agent auth Secret creation failed or outcome unknown; inspect target before retrying")
	}
	return g.pullChoices(ctx, a)
}

func (g *suiteLiftGuide) checkTarget(ctx context.Context, a *App) error {
	server, err := a.consoleCreateTarget(ctx, g.env)
	if err != nil || server != g.server {
		return errors.New("destination access changed since selection; select the destination again")
	}
	return nil
}

func (g *suiteLiftGuide) needsJudge() bool {
	if g.entry.Review == nil {
		return false
	}
	for _, row := range g.entry.Review.inventory {
		for _, assertion := range agentruntime.EvaluationCaseAssertions(row.Case) {
			if assertion.Type == "llmJudge" {
				return true
			}
		}
	}
	return false
}
