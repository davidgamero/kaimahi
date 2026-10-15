package app

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/scaffold"

	tea "charm.land/bubbletea/v2"
)

type suiteCreateChoice struct {
	label, value, detail string
	source               consoleInferenceSource
	azure                consoleAzureChoice
}
type suiteCreateStep struct {
	kind, title, value string
	choices            []suiteCreateChoice
	selection          int
	query              string
}
type suiteCreateWizard struct {
	steps                                    []suiteCreateStep
	root, name, profile, model, instructions string
	subscription, group, account             string
	connection                               string
	defaultRoot                              string
	copilot                                  *suiteCopilotBinding
	foundryEndpoint                          string
	setup                                    *suiteInferenceSetup
}

func (w *suiteCreateWizard) step() *suiteCreateStep { return &w.steps[len(w.steps)-1] }
func (m *suiteConsole) createStep(step suiteCreateStep) {
	m.creation.steps = append(m.creation.steps, step)
	m.input.SetValue(step.value)
	m.input.CursorEnd()
	m.input.Focus()
}
func (m *suiteConsole) startCreation() tea.Cmd {
	if m.refreshCancel != nil {
		m.refreshCancel()
	}
	m.refreshGeneration++
	m.refreshing = false
	m.creation = &suiteCreateWizard{}
	m.createStep(suiteCreateStep{kind: "name", title: "What’s this new agent called?"})
	return nil
}

// Provider discovery reads public configuration only, never Secret values or an
// existing Agent. It is scoped to the context explicitly chosen in this wizard.
func (a *App) suiteCreateProviders(ctx context.Context, env agentTUIEnvironment, namespace string) ([]suiteCreateChoice, error) {
	raw, err := env.app(a).orkaCapture(ctx, nil, "-n", namespace, "get", consoleProviderKind, "-o", "json")
	if err != nil {
		return nil, err
	}
	var list objectList[struct {
		Metadata struct{ Name string }
		Spec     struct{ Type, DefaultModel, BaseURL string }
		Status   struct {
			Ready      *bool
			Conditions []struct{ Type, Status string }
		}
	}]
	if err := decodeConsoleList(raw, &list); err != nil {
		return nil, err
	}
	var choices []suiteCreateChoice
	for _, p := range list.Items {
		ready := p.Status.Ready != nil && *p.Status.Ready
		for _, c := range p.Status.Conditions {
			if c.Type == "Ready" && c.Status == "True" {
				ready = true
			}
		}
		if !ready || p.Spec.Type != "openai" || strings.TrimSpace(p.Spec.DefaultModel) == "" {
			continue
		}
		choices = append(choices, suiteCreateChoice{label: p.Spec.DefaultModel + " · connection: " + p.Metadata.Name, value: p.Spec.DefaultModel, detail: environmentLabel(env) + " / " + namespace + "\nEndpoint: " + p.Spec.BaseURL, source: consoleInferenceSource{Name: p.Metadata.Name, Model: p.Spec.DefaultModel, Endpoint: p.Spec.BaseURL, Namespace: namespace}})
	}
	return choices, nil
}

func (m *suiteConsole) createDiscovery(stage string, env agentTUIEnvironment) tea.Cmd {
	w := *m.creation
	namespace := valueOr(m.opt.Namespace, OrkaNamespace)
	return m.jobNamed("Discovering "+strings.ReplaceAll(stage, "-", " "), func(ctx context.Context, a *App) suiteConsoleResult {
		step := suiteCreateStep{kind: stage, title: "Choose inference"}
		var err error
		if stage == "copilot-models" {
			step.choices, err = a.suiteCopilotModels(ctx, env)
			step.title = "Choose a model from your local Copilot CLI"
		} else if stage == "providers" {
			step.choices, err = a.suiteCreateProviders(ctx, env, namespace)
			step.title = "Choose a model and its inference connection"
		} else {
			var choices []consoleAzureChoice
			choices, err = a.consoleAzureChoices(ctx, stage, w.subscription, w.group, w.account)
			if err == nil && stage == "azure-subscriptions" {
				var prefs map[string]string
				prefs, err = loadLiftPreferences()
				if err == nil {
					choices = recentSuiteSubscriptions(choices, prefs[suiteSubscriptionPreference])
				}
			}
			for _, c := range choices {
				label := c.Label
				if stage == "azure-subscriptions" {
					label += " · " + c.ID
				}
				step.choices = append(step.choices, suiteCreateChoice{label: label, detail: c.Detail, value: c.Model, azure: c})
			}
			step.title = map[string]string{"azure-subscriptions": "Find model connections · Azure subscription", "azure-accounts": "Choose a Foundry inference resource", "azure-deployments": "Choose the model deployment to use"}[stage]
		}
		if err != nil {
			return suiteConsoleResult{err: fmt.Errorf("inference discovery unavailable; Backspace chooses another source: %w", err)}
		}
		if len(step.choices) == 0 {
			return suiteConsoleResult{err: fmt.Errorf("no compatible choices found; Backspace chooses another source or manual setup")}
		}
		return suiteConsoleResult{createStep: &step}
	})
}

func (m *suiteConsole) updateCreation(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	w := m.creation
	s := w.step()
	k := key.String()
	if k == "esc" {
		m.creation = nil
		return m, nil
	}
	// Text fields keep ordinary deletion. At their start, Backspace navigates;
	// Alt+Left always goes back without changing text.
	if k == "alt+left" || k == "backspace" && (len(s.choices) > 0 && s.query == "" || len(s.choices) == 0 && m.input.Position() == 0) {
		s.value = m.input.Value()
		if len(s.choices) == 0 {
			switch s.kind {
			case "root":
				w.root = s.value
			case "name":
				w.name = s.value
			case "manual":
				w.model = s.value
			case "instructions":
				w.instructions = s.value
			}
		}
		if len(w.steps) > 1 {
			w.steps = w.steps[:len(w.steps)-1]
			m.input.SetValue(w.step().value)
			m.input.CursorEnd()
		}
		return m, nil
	}
	if len(s.choices) > 0 {
		if s.searchable() {
			switch k {
			case "up":
				s.selection = max(0, s.selection-1)
			case "down", "tab":
				s.selection = min(max(0, len(s.matches())-1), s.selection+1)
			case "enter":
				return m, m.advanceCreation()
			case "backspace":
				r := []rune(s.query)
				if len(r) > 0 {
					s.query = string(r[:len(r)-1])
					s.selection = 0
				}
			default:
				if key.Text != "" && len(s.query)+len(key.Text) <= 512 && key.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
					s.query += key.Text
					s.selection = 0
				}
			}
			return m, nil
		}
		switch k {
		case "up", "k":
			s.selection = max(0, s.selection-1)
		case "down", "j", "tab":
			s.selection = min(len(s.choices)-1, s.selection+1)
		case "enter":
			return m, m.advanceCreation()
		}
		return m, nil
	}
	if k == "enter" {
		s.value = m.input.Value()
		return m, m.advanceCreation()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(key)
	return m, cmd
}

func (m *suiteConsole) advanceCreation() tea.Cmd {
	w := m.creation
	s := w.step()
	c := suiteCreateChoice{}
	if len(s.choices) > 0 {
		matches := s.matches()
		if len(matches) == 0 {
			m.status = "No matching choices; edit the search"
			return nil
		}
		c = s.choices[matches[min(s.selection, len(matches)-1)]]
	}
	switch s.kind {
	case "name":
		name := strings.TrimSpace(s.value)
		if err := scaffold.ValidateName(name); err != nil {
			m.status = err.Error()
			return nil
		}
		w.name = name
		if w.root == "" || w.root == w.defaultRoot {
			w.root = filepath.Join(".", name)
		}
		w.defaultRoot = filepath.Join(".", name)
		if w.profile == "" {
			w.profile = "native"
		}
		return m.chooseCreationExecution(w.profile)
	case "root":
		if strings.TrimSpace(s.value) == "" {
			m.status = "Choose a folder to save your agent"
			return nil
		}
		w.root = s.value
		w.steps = w.steps[:len(w.steps)-1]
		w.step().selection = 0
		m.input.SetValue(w.step().value)
	case "profile":
		if c.value == w.profile {
			w.steps = w.steps[:len(w.steps)-1]
			return nil
		}
		return m.chooseCreationExecution(c.value)
	case "source":
		if c.value == "foundry" {
			return m.createDiscovery("azure-subscriptions", agentTUIEnvironment{})
		}
		if c.value == "manual" {
			w.connection = "Manual: configure a Ready openai Provider and its Secret before evaluation. For in-cluster Ollama, use orka install --provider local after preparing the model service."
			m.createStep(suiteCreateStep{kind: "manual", title: "Exact model or deployment name", value: w.model})
			break
		}
		for _, col := range m.columns {
			if col.Env.Name == c.value {
				return m.createDiscovery("providers", col.Env)
			}
		}
	case "azure-subscriptions":
		w.subscription = c.azure.ID
		if err := saveLiftPreference(suiteSubscriptionPreference, c.azure.ID); err != nil {
			m.status = "Could not save subscription preference: " + err.Error()
			return nil
		}
		return m.createDiscovery("azure-accounts", agentTUIEnvironment{})
	case "azure-accounts":
		w.setup = nil
		w.group, w.account = c.azure.ResourceGroup, c.azure.ID
		w.connection = "Not checked — choose Check / set up inference connection."
		w.foundryEndpoint = c.azure.Endpoint
		return m.createDiscovery("azure-deployments", agentTUIEnvironment{})
	case "providers":
		w.model = c.value
		w.connection = "Existing Provider: " + c.source.Name + " · " + c.detail
		m.createStep(suiteCreateStep{kind: "instructions", title: "Instructions text (or @path to a file)", value: w.instructions})
	case "azure-deployments":
		w.model = c.value
		m.createStep(suiteCreateStep{kind: "instructions", title: "Instructions text (or @path to a file)", value: w.instructions})
	case "copilot-models":
		w.model = c.value
		m.createStep(suiteCreateStep{kind: "instructions", title: "Instructions text (or @path to a file)", value: w.instructions})
	case "manual":
		if strings.TrimSpace(s.value) == "" {
			m.status = "An exact model is required for native source"
			return nil
		}
		w.model = s.value
		m.createStep(suiteCreateStep{kind: "instructions", title: "Instructions text (or @path to a file)", value: w.instructions})
	case "instructions":
		if strings.TrimSpace(s.value) == "" {
			m.status = "Instructions are required"
			return nil
		}
		w.instructions = s.value
		m.createConfirmation()
	case "confirm":
		if c.value == "execution" {
			choices := []suiteCreateChoice{{label: "Orka AgentSuite (default)", value: "native", detail: "Native Orka with the selected exact model."}, {label: "Container image", value: "image-http", detail: "Build and publish an image; bind inference at evaluation."}}
			if m.columns[0].Env.Name != "" && m.columns[0].Env.Local {
				choices = append(choices, suiteCreateChoice{label: "Local Copilot CLI", value: "copilot", detail: "Host CLI/login, not cluster execution or evaluation proof."})
			}
			m.createStep(suiteCreateStep{kind: "profile", title: "Change execution", choices: choices})
			return nil
		}
		if c.value == "inference" {
			var choices []suiteCreateChoice
			for _, col := range m.columns {
				if col.Env.Name != "" {
					choices = append(choices, suiteCreateChoice{label: environmentLabel(col.Env), value: col.Env.Name, detail: "Configure cluster inference credentials here · " + col.Env.Server})
				}
			}
			if len(choices) == 0 {
				m.status = "Select a context with t before configuring cluster inference"
				return nil
			}
			m.createStep(suiteCreateStep{kind: "inference-target", title: "Choose environment for the inference connection", choices: choices})
			return nil
		}
		if c.value == "location" {
			m.createStep(suiteCreateStep{kind: "root", title: "Where should we save this agent?", value: w.root})
			return nil
		}
		if c.value == "cancel" {
			m.creation = nil
			return nil
		}
		f := suiteConsoleForm{action: "create", values: []string{w.root, w.name, w.model, w.instructions}, execution: w.profile, copilot: w.copilot}
		m.creation = nil
		return m.submit(f)
	case "inference-target":
		for _, col := range m.columns {
			if col.Env.Name == c.value {
				w.setup = &suiteInferenceSetup{Environment: col.Env, Namespace: valueOr(m.opt.Namespace, OrkaNamespace), Endpoint: w.foundryEndpoint, Model: w.model, Provider: slugAgentName("foundry-" + w.model), Key: "api-key"}
			}
		}
		setup := *w.setup
		setup.Secret = setup.Provider + "-key"
		return m.job(func(ctx context.Context, a *App) suiteConsoleResult {
			found, err := a.discoverSuiteInferenceBinding(ctx, setup)
			if err != nil {
				return suiteConsoleResult{err: err}
			}
			step := suiteCreateStep{kind: "inference-provider", title: "Provider name (detected matching or new)", value: found.Provider}
			return suiteConsoleResult{createStep: &step, inferenceSetup: &found}
		})
	case "inference-provider":
		previous := w.setup.Provider
		w.setup.Provider = strings.TrimSpace(s.value)
		if err := scaffold.ValidateName(w.setup.Provider); err != nil {
			m.status = err.Error()
			return nil
		}
		if w.setup.Provider != previous {
			w.setup.Secret = w.setup.Provider + "-key"
		}
		m.createStep(suiteCreateStep{kind: "inference-secret", title: "Secret name (existing or new; api-key entry)", value: w.setup.Secret})
	case "inference-secret":
		w.setup.Secret = strings.TrimSpace(s.value)
		setup := *w.setup
		if err := validateSuiteInference(setup); err != nil {
			m.status = err.Error()
			return nil
		}
		return m.job(func(ctx context.Context, a *App) suiteConsoleResult {
			state, err := a.inspectSuiteInference(ctx, setup)
			if err != nil {
				return suiteConsoleResult{err: err}
			}
			step := suiteCreateStep{kind: "inference-key-env", title: "API-key environment variable (blank reuses existing Secret)", value: ""}
			if strings.HasPrefix(state, "Secret absent") {
				step.value = "AZURE_OPENAI_API_KEY"
			}
			return suiteConsoleResult{createStep: &step, text: state}
		})
	case "inference-key-env":
		m.createStep(suiteCreateStep{kind: "inference-apply", title: "Type context to authorize Provider/Secret setup: " + w.setup.Environment.Name})
	case "inference-apply":
		setup := *w.setup
		typed := s.value
		keyEnv := w.steps[len(w.steps)-2].value
		return m.job(func(ctx context.Context, a *App) suiteConsoleResult {
			err := a.setupSuiteInference(ctx, setup, typed, keyEnv)
			if err != nil {
				return suiteConsoleResult{err: err}
			}
			return suiteConsoleResult{text: "Inference configured. Readiness and model access will be checked during evaluation.", inferenceConfigured: true}
		})
	}
	return nil
}

func (m *suiteConsole) createConfirmation() {
	choices := []suiteCreateChoice{{label: "Create agent", value: "create", detail: "Save editable agent files. Review cases before evaluation or Lift."}, {label: "Change location", value: "location", detail: "Choose a different folder for this agent’s files."}}
	choices = append(choices, suiteCreateChoice{label: "Change execution", value: "execution", detail: "Orka is the default; optionally choose image or local Copilot execution."})
	if m.creation.foundryEndpoint != "" {
		choices = append(choices, suiteCreateChoice{label: "Check / set up inference connection", value: "inference", detail: "Detect an existing Provider/Secret, or explicitly create them using a host environment variable."})
	}
	choices = append(choices, suiteCreateChoice{label: "Cancel", value: "cancel"})
	m.createStep(suiteCreateStep{kind: "confirm", title: "Create your agent", choices: choices})
}

func (m *suiteConsole) chooseCreationExecution(profile string) tea.Cmd {
	w := m.creation
	if w.profile != profile {
		w.model = ""
		w.connection = ""
		w.copilot = nil
		w.foundryEndpoint = ""
		w.setup = nil
	}
	w.profile = profile
	if profile == "copilot" {
		env := m.columns[0].Env
		if env.Name == "" || !env.Local {
			m.status = "Select a local kind context before choosing Copilot"
			return nil
		}
		w.copilot = &suiteCopilotBinding{Environment: env}
		w.connection = "Local Copilot CLI with your host login; no cluster Tasks or evaluation proof."
		return m.createDiscovery("copilot-models", env)
	}
	if profile == "image-http" {
		m.createStep(suiteCreateStep{kind: "instructions", title: "Instructions text (or @path to a file)", value: w.instructions})
		return nil
	}
	var choices []suiteCreateChoice
	for _, col := range m.columns {
		if col.Env.Name != "" {
			choices = append(choices, suiteCreateChoice{label: environmentLabel(col.Env) + " · available model connections", value: col.Env.Name, detail: "Browse configured OpenAI-compatible inference connections and their models. This does not choose where to deploy your agent."})
		}
	}
	choices = append(choices, suiteCreateChoice{label: "REMOTE · Azure Foundry model deployments", value: "foundry", detail: "Browse hosted models using Azure login; configure cluster credentials separately."}, suiteCreateChoice{label: "Enter model name manually (advanced)", value: "manual", detail: "Use when discovery is unavailable. Authoring does not provision inference."})
	m.createStep(suiteCreateStep{kind: "source", title: "Connect inference · where should we find your model?", choices: choices})
	return nil
}

const suiteSubscriptionPreference = "console-create/azure-subscription"

func (s *suiteCreateStep) searchable() bool {
	switch s.kind {
	case "azure-subscriptions", "azure-accounts", "azure-deployments", "providers", "source", "copilot-models", "inference-target":
		return true
	}
	return false
}

func recentSuiteSubscriptions(choices []consoleAzureChoice, last string) []consoleAzureChoice {
	result := append([]consoleAzureChoice(nil), choices...)
	for i, c := range result {
		if last != "" && c.ID == last {
			copy(result[1:i+1], result[:i])
			c.Detail = "last selected · " + c.Detail
			result[0] = c
			break
		}
	}
	return result
}

func (s *suiteCreateStep) matches() []int {
	type match struct{ index, score int }
	var matches []match
	for i, c := range s.choices {
		if score, ok := pickerFuzzyScore(c.label+" "+c.detail, s.query); ok {
			matches = append(matches, match{i, score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score < matches[j].score })
	indices := make([]int, len(matches))
	for i, m := range matches {
		indices[i] = m.index
	}
	return indices
}
