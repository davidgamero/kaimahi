package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/scaffold"
	"oras.land/oras-go/v2/registry"
)

func (m *suiteConsole) guideInput(stage, title, value, confirm string) {
	m.liftGuide.input(stage, title, value, confirm)
	m.input.SetValue(value)
	m.input.CursorEnd()
	m.input.Focus()
}
func (m *suiteConsole) updateLiftGuide(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	g := m.liftGuide
	switch key.String() {
	case "esc":
		m.liftGuide = nil
		return m, nil
	case "alt+left":
		return m, m.guideJob(func(ctx context.Context, a *App, g *suiteLiftGuide) error { return g.destinations(ctx, a) })
	}
	if len(g.choices) > 0 {
		switch key.String() {
		case "up":
			g.selection = max(0, g.selection-1)
		case "down", "tab":
			g.selection = min(max(0, len(g.matches())-1), g.selection+1)
		case "backspace":
			r := []rune(g.query)
			if len(r) > 0 {
				g.query = string(r[:len(r)-1])
				g.selection = 0
			}
		case "enter":
			matches := g.matches()
			if len(matches) > 0 {
				return m, m.advanceLiftGuide(g.choices[matches[min(g.selection, len(matches)-1)]])
			}
		default:
			if key.Text != "" && key.Mod&(tea.ModCtrl|tea.ModAlt) == 0 && len(g.query)+len(key.Text) <= 512 {
				g.query += key.Text
				g.selection = 0
			}
		}
		return m, nil
	}
	if key.String() == "enter" {
		g.value = m.input.Value()
		if g.confirm != "" && g.value != g.confirm {
			m.status = "Type " + g.confirm + " to confirm this stage"
			return m, nil
		}
		return m, m.advanceLiftGuide(liftGuideChoice{})
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(key)
	return m, cmd
}

func (m *suiteConsole) advanceLiftGuide(c liftGuideChoice) tea.Cmd {
	g := m.liftGuide
	job := m.guideJob
	advanced := func() tea.Cmd { v := g.version; m.liftGuide = nil; return m.publicationForm(v, true) }
	switch g.stage {
	case "runtime-alert":
		if c.value == "okay" {
			g.runtimeActions()
		}
	case "error":
		if c.value == "advanced" {
			return advanced()
		}
		if c.value == "retry" && m.guideRetry != nil {
			return job(m.guideRetry)
		}
		return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error { return g.destinations(ctx, a) })
	case "destination":
		if c.value == "advanced" {
			return advanced()
		}
		if c.value == "aks" {
			return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error {
				raw, err := a.liftDiscovery(ctx, "az", "aks", "list", "-o", "json", "--only-show-errors")
				if err != nil {
					return err
				}
				var clusters []suiteAzureCluster
				if json.Unmarshal(raw, &clusters) != nil || len(clusters) > 128 {
					return errors.New("invalid AKS inventory")
				}
				var choices []liftGuideChoice
				for _, cluster := range clusters {
					choices = append(choices, liftGuideChoice{label: cluster.Name, detail: cluster.ResourceGroup + " · " + cluster.ID, aks: cluster})
				}
				if len(choices) == 0 {
					return errors.New("no AKS clusters visible in active subscription; choose another subscription using az account set")
				}
				g.pick("aks", "Choose AKS cluster", choices)
				return nil
			})
		}
		g.env = c.env
		g.server = c.env.Server
		return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error { return g.namespaces(ctx, a) })
	case "aks":
		g.aks = c.aks
		m.guideInput("aks-import", "Fetch private access? Type AKS cluster name: "+c.aks.Name, "", c.aks.Name)
	case "aks-import":
		return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error {
			env, err := a.prepareConsoleContext(ctx, "", &g.aks, false)
			if err != nil {
				return err
			}
			g.env = env
			g.server = env.Server
			return g.namespaces(ctx, a)
		})
	case "namespace":
		g.namespace = c.value
		return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error { return g.runtimeAndProviders(ctx, a) })
	case "runtime":
		if c.value == "isolated" {
			m.guideInput("isolated-namespace", "New namespace for a separate KMX-managed Orka", "kmx-orka", "")
			return nil
		}
		if c.value == "destination" {
			return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error { return g.destinations(ctx, a) })
		}
		if c.value == "install" {
			m.guideInput("runtime-install", "Install pinned Orka? Type context: "+g.env.Name, "", g.env.Name)
			return nil
		}
		return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error { return g.runtimeAndProviders(ctx, a) })
	case "runtime-install":
		return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error {
			if err := g.checkTarget(ctx, a); err != nil {
				return err
			}
			if err := g.env.app(a).PrepareConsoleRuntime(ctx, g.env.Name, g.env.Name); err != nil {
				return err
			}
			return g.runtimeAndProviders(ctx, a)
		})
	case "isolated-namespace":
		g.namespace = strings.TrimSpace(g.value)
		return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error {
			w := g.env.app(a).withRunContext(ctx)
			chart, err := w.fetchOrkaInstaller()
			if err != nil {
				return err
			}
			if err := w.checkIsolatedOrka(ctx, g.namespace, chart); err != nil {
				g.pick("coexistence", "Separate installation cannot proceed", []liftGuideChoice{{label: "Choose another destination", value: "destination"}, {label: "Try another new namespace", value: "namespace"}})
				g.note = err.Error()
				return nil
			}
			g.input("isolated-install", "Install isolated Orka into "+g.namespace+"? Type "+g.env.Name, "", g.env.Name)
			g.note = "New Helm release and runtime namespace; existing CRDs remain unchanged. No automatic rollback or cleanup."
			return nil
		})
	case "coexistence":
		if c.value == "namespace" {
			m.guideInput("isolated-namespace", "New isolated Orka namespace", "kmx-orka", "")
			return nil
		}
		return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error { return g.destinations(ctx, a) })
	case "isolated-install":
		return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error {
			if err := g.checkTarget(ctx, a); err != nil {
				return err
			}
			w := g.env.app(a).withRunContext(ctx)
			w.Cfg.Confirm = g.env.Name
			if err := w.prepareIsolatedOrka(ctx, g.namespace, g.env.Name); err != nil {
				return err
			}
			return g.runtimeAndProviders(ctx, a)
		})
	case "provider":
		if c.value == "retry" {
			return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error { return g.providers(ctx, a) })
		}
		if c.value == "foundry" {
			return m.guideFoundry("azure-subscriptions", "", "", "")
		}
		if c.value == "manual" {
			m.guideInput("endpoint", "OpenAI-compatible endpoint (advanced)", "https://", "")
			return nil
		}
		g.provider = "provider:" + c.provider.Name
		g.endpoint = c.provider.Endpoint
		g.model = c.provider.Model
		return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error {
			if g.image() {
				raw, err := g.env.app(a).orkaCapture(ctx, nil, "-n", g.namespace, "get", consoleProviderKind, strings.TrimPrefix(g.provider, "provider:"), "-o", "json")
				if err != nil {
					return err
				}
				var p struct {
					Spec struct {
						Type, BaseURL, DefaultModel string
						SecretRef                   struct{ Name, Key string }
					}
				}
				if json.Unmarshal(raw, &p) != nil || p.Spec.Type != "openai" || p.Spec.BaseURL != g.endpoint || p.Spec.DefaultModel != g.model {
					return errors.New("inference changed during selection")
				}
				g.secret, g.secretKey = p.Spec.SecretRef.Name, p.Spec.SecretRef.Key
				return g.authChoices(ctx, a)
			}
			return g.afterInference(ctx, a)
		})
	case "endpoint":
		g.endpoint = strings.TrimSpace(g.value)
		m.guideInput("model", "Exact model/deployment name", g.model, "")
	case "model":
		g.model = strings.TrimSpace(g.value)
		return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error {
			choices, err := guideSecretChoices(ctx, a, g.env, g.namespace, "api-key", false)
			if err != nil {
				return err
			}
			choices = append(choices, liftGuideChoice{label: "Keyless endpoint", value: "", detail: "Use only if the endpoint does not require authentication"})
			g.pick("inference-key", "Choose model credential reference", choices)
			return nil
		})
	case "inference-key":
		g.secret = c.value
		g.secretKey = "api-key"
		return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error { return g.authChoices(ctx, a) })
	case "foundry-subscriptions":
		g.subscription = c.value
		if err := saveLiftPreference(suiteSubscriptionPreference, c.value); err != nil {
			m.status = err.Error()
			return nil
		}
		return m.guideFoundry("azure-accounts", g.subscription, "", "")
	case "foundry-accounts":
		g.group, g.account, g.endpoint = c.group, c.value, c.endpoint
		return m.guideFoundry("azure-deployments", g.subscription, g.group, g.account)
	case "foundry-deployments":
		g.model = c.value
		if !g.image() && g.entry.Review != nil && g.model != g.entry.Review.model() {
			m.status = "Deployment must match packaged model " + g.entry.Review.model()
			return nil
		}
		g.provider = "provider:" + slugAgentName("foundry-"+g.model)
		g.secret = strings.TrimPrefix(g.provider, "provider:") + "-key"
		g.secretKey = "api-key"
		return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error {
			s, err := a.discoverSuiteInferenceBinding(ctx, g.setup())
			if err != nil {
				return err
			}
			g.provider = "provider:" + s.Provider
			g.secret = s.Secret
			state, err := a.inspectSuiteInference(ctx, s)
			if err != nil {
				return err
			}
			g.note = state
			g.input("foundry-key-env", "Key environment variable (blank reuses existing Secret)", "", "")
			if strings.HasPrefix(state, "Secret absent") {
				g.value = "AZURE_OPENAI_API_KEY"
			}
			return nil
		})
	case "foundry-key-env":
		g.keyEnv = g.value
		m.guideInput("foundry-setup", "Confirm create-only Provider/Secret setup: type "+g.env.Name, "", g.env.Name)
	case "foundry-setup":
		return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error {
			if err := g.checkTarget(ctx, a); err != nil {
				return err
			}
			if err := a.setupSuiteInference(ctx, g.setup(), g.env.Name, g.keyEnv); err != nil {
				return err
			}
			g.note = "Connection configured; refresh until Provider is Ready, then select it."
			return g.providers(ctx, a)
		})
	case "auth":
		if c.value == "create-auth" {
			g.authSecret = slugAgentName(g.entry.Name + "-auth")
			m.guideInput("auth-name", "New agent authentication Secret name", g.authSecret, "")
			return nil
		}
		g.authSecret = c.value
		return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error { return g.pullChoices(ctx, a) })
	case "auth-name":
		g.authSecret = strings.TrimSpace(g.value)
		if err := scaffold.ValidateName(g.authSecret); err != nil {
			m.status = err.Error()
			return nil
		}
		m.guideInput("auth-create", "Create a random authentication Secret? Type "+g.env.Name, "", g.env.Name)
	case "auth-create":
		return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error { return g.createAuth(ctx, a) })
	case "pull":
		g.pullSecret = c.value
		return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error { return g.capabilityReview(ctx, a) })
	case "capabilities":
		if c.value == "advanced" {
			return advanced()
		}
		return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error {
			if err := g.imageBindings(ctx, a); err != nil {
				return err
			}
			return g.afterInference(ctx, a)
		})
	case "registry":
		if c.value == "manual" {
			m.guideInput("registry-host", "Registry hostname (HTTPS)", "", "")
			return nil
		}
		g.acr = c.acr
		g.registry = c.acr.LoginServer
		return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error { return g.checkRegistry(ctx) })
	case "registry-host":
		g.registry = strings.TrimSpace(g.value)
		g.acr = suiteAzureRegistry{}
		return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error { return g.checkRegistry(ctx) })
	case "registry-access":
		if c.value == "registry" {
			return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error { return g.registryChoices(ctx, a) })
		}
		if c.value == "login" {
			g.pick("acr-login", "Log in to "+g.acr.Name+"?", []liftGuideChoice{
				{label: "Confirm login", value: "confirm", detail: "Use your Azure login to update local registry credentials for " + g.acr.LoginServer},
				{label: "Cancel", value: "cancel", detail: "Return to registry access options"},
			})
			return nil
		}
		label, value := g.publicationRepositoryDefault()
		m.guideInput("repository", label, value, "")
	case "acr-login":
		if c.value != "confirm" {
			g.pick("registry-access", "Registry access", []liftGuideChoice{
				{label: "Log in to selected ACR", value: "login"},
				{label: "Continue to publication", value: "continue"},
				{label: "Choose another registry", value: "registry"},
			})
			return nil
		}
		return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error {
			args := []string{"acr", "login", "--name", g.acr.Name, "--only-show-errors"}
			parts := strings.Split(strings.Trim(g.acr.ID, "/"), "/")
			if len(parts) > 1 && strings.EqualFold(parts[0], "subscriptions") {
				args = append(args, "--subscription", parts[1])
			}
			if _, err := a.azureCapture(ctx, 60*time.Second, "az", args, "ACR login failed; check Azure login and publisher permissions"); err != nil {
				return err
			}
			label, value := g.publicationRepositoryDefault()
			g.input("repository", label, value, "")
			g.note = "Host registry login completed. Repository push and node pull remain independently checked."
			return nil
		})
	case "repository":
		g.repository = strings.TrimSpace(g.value)
		parsed, err := registry.ParseReference(g.registry + "/" + g.repository)
		if err != nil {
			m.status = "Enter repository:version"
			return nil
		}
		if !g.image() {
			if parsed.Reference != "" {
				m.status = "Native workspace Lift uses repository only; its version tag is derived from the exact suite digest"
				return nil
			}
			g.repository = parsed.Repository
			if g.needsJudge() {
				m.guideInput("pipeline-judge", "Judge endpoint required by authored cases", "", "")
				return nil
			}
			return m.startNativePipeline()
		}
		if err = parsed.ValidateReferenceAsTag(); err != nil {
			m.status = "A repository and version tag are required"
			return nil
		}
		if g.image() {
			return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error { return g.builders(ctx, a) })
		}
		m.guideInput("publish", "Publish authored source to "+g.registry+"/"+g.repository+"? Type publish", "", "publish")
	case "builder":
		g.builder = c.value
		m.guideInput("publish", "Build image and publish source/image to "+g.registry+"/"+g.repository+"? Type publish", "", "publish")
	case "publish":
		if g.entry.Review == nil {
			m.status = "Open source before publication"
			return nil
		}
		if !g.entry.Reviewed {
			g.stage = "review-publish"
			m.showReview(g.entry.Review)
			return nil
		}
		return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error { return g.publish(ctx, a) })
	case "evidence":
		switch c.value {
		case "advanced":
			return advanced()
		case "proof":
			g.promote = false
			return m.guidePlan()
		case "import":
			m.guideInput("import-proof", "Private evidence file (advanced)", g.version.Evidence, "")
		case "evaluate":
			if g.entry.Review == nil {
				m.status = "Open source for evaluation"
				return nil
			}
			m.showReview(g.entry.Review)
			g.stage = "review-eval"
		}
	case "import-proof":
		g.version.Evidence = g.value
		g.promote = false
		return m.guidePlan()
	case "judge-endpoint":
		g.judgeURL = g.value
		m.guideInput("judge-env", "Judge key environment name (optional)", "", "")
	case "judge-env":
		g.judgeKeyEnv = g.value
		m.guideInput("evaluate", "Execute all reviewed cases on "+g.env.Name+"? Type context to authorize placement and model calls", "", g.env.Name)
	case "pipeline-judge":
		g.judgeURL = g.value
		m.guideInput("pipeline-judge-env", "Judge key environment variable (optional)", "", "")
	case "pipeline-judge-env":
		g.judgeKeyEnv = g.value
		return m.startNativePipeline()
	case "evaluate":
		return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error { return g.evaluate(ctx, a) })
	case "proof-refused":
		switch c.value {
		case "promote":
			m.guideInput("promote", "Source-only promotion: this is not destination evaluation. Type promote", "", "promote")
		case "retry":
			g.promote = false
			return m.guidePlan()
		case "evaluate":
			g.evidenceChoices()
		case "destination":
			return job(func(ctx context.Context, a *App, g *suiteLiftGuide) error { return g.destinations(ctx, a) })
		case "advanced":
			return advanced()
		}
	case "promote":
		g.promote = true
		return m.guidePlan()
	}
	return nil
}

func (g *suiteLiftGuide) afterInference(ctx context.Context, a *App) error {
	if g.version.Reference != "" {
		g.evidenceChoices()
		return nil
	}
	return g.registryChoices(ctx, a)
}

func (g *suiteLiftGuide) publicationRepositoryDefault() (string, string) {
	if !g.image() {
		return "Publication repository (version derived from exact suite digest)", slugAgentName(g.entry.Name)
	}
	return "Repository and version (suggested from agent)", slugAgentName(g.entry.Name) + ":v" + time.Now().UTC().Format("20060102-150405")
}
func (g *suiteLiftGuide) setup() suiteInferenceSetup {
	return suiteInferenceSetup{Environment: g.env, Namespace: g.namespace, Provider: strings.TrimPrefix(g.provider, "provider:"), Endpoint: g.endpoint, Model: g.model, Secret: g.secret, Key: "api-key"}
}

func (m *suiteConsole) guideFoundry(stage, sub, group, account string) tea.Cmd {
	return m.guideJob(func(ctx context.Context, a *App, g *suiteLiftGuide) error {
		choices, err := a.consoleAzureChoices(ctx, stage, sub, group, account)
		if err != nil {
			return err
		}
		var picks []liftGuideChoice
		if stage == "azure-subscriptions" {
			prefs, err := loadLiftPreferences()
			if err != nil {
				return err
			}
			choices = recentSuiteSubscriptions(choices, prefs[suiteSubscriptionPreference])
		}
		for _, c := range choices {
			label := c.Label
			value := c.ID
			if stage == "azure-subscriptions" {
				label += " · " + c.ID
			}
			if stage == "azure-deployments" {
				value = c.Model
			}
			picks = append(picks, liftGuideChoice{label: label, detail: c.Detail, value: value, endpoint: c.Endpoint, group: c.ResourceGroup})
		}
		if len(picks) == 0 {
			return fmt.Errorf("no Foundry choices available; choose another inference source")
		}
		g.pick(strings.Replace(stage, "azure-", "foundry-", 1), "Connect Foundry · "+strings.TrimPrefix(stage, "azure-"), picks)
		return nil
	})
}

func (m *suiteConsole) guidePlan() tea.Cmd {
	g := *m.liftGuide
	return m.job(func(ctx context.Context, a *App) suiteConsoleResult {
		if err := g.checkTarget(ctx, a); err != nil {
			return suiteConsoleResult{guide: &g, err: err}
		}
		p, err := g.env.app(a).PrepareReleaseLift(ctx, g.options())
		if err != nil {
			g.pick("proof-refused", "Proof / target check needs attention", []liftGuideChoice{{label: "Try explicit source-only promotion", value: "promote", detail: "Only succeeds for runtime/package/model-compatible absent destinations"}, {label: "Review evaluation options", value: "evaluate"}, {label: "Check proof again", value: "retry"}, {label: "Choose another destination", value: "destination"}, {label: "Advanced bindings", value: "advanced"}})
			g.note = err.Error()
			return suiteConsoleResult{guide: &g}
		}
		return suiteConsoleResult{plan: p, guideDone: true, guide: &g}
	})
}
