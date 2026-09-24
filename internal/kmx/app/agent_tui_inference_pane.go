package app

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type consoleInferenceLoaded struct {
	pane     *consoleInferencePane
	snapshot consoleInferenceSnapshot
	err      error
}
type consoleInferenceSaved struct{ err error }
type consoleInferenceField struct {
	label string
	input textinput.Model
}
type consoleInferencePane struct {
	env                     agentTUIEnvironment
	agent                   agentTUIAgent
	snapshot                consoleInferenceSnapshot
	stage                   string
	selection, field, frame int
	source                  consoleInferenceSource
	fields                  []consoleInferenceField
	model                   string
	err                     error
	cancel                  context.CancelFunc
	cancelling              bool
}

func (m agentTUIModel) openInference() (tea.Model, tea.Cmd) {
	agent := m.selected()
	if agent == nil || agent.External {
		return m, nil
	}
	p := &consoleInferencePane{env: m.columns[m.focus].Env, agent: *agent, stage: "loading"}
	m.inference = p
	if m.opt.Demo {
		p.stage = "sources"
		p.snapshot = consoleInferenceSnapshot{Server: "demo", Version: "demo", Sources: []consoleInferenceSource{{Kind: "cluster", Name: "demo-provider", Model: agent.Model, Provider: "openai"}}}
		return m, nil
	}
	load := m.loadInference
	return m, tea.Batch(quickstartTick(), func() tea.Msg { snapshot, err := load(p.env, p.agent); return consoleInferenceLoaded{p, snapshot, err} })
}

func (p *consoleInferencePane) sourceKinds() []string {
	if p.agent.Runtime == "kagent" {
		return []string{"ollama", "apikey"}
	}
	return []string{"foundry", "ollama", "copilot", "apikey"}
}

func (p *consoleInferencePane) setFields(kind string) {
	p.source = consoleInferenceSource{Kind: kind}
	p.fields = nil
	p.field = 0
	p.stage = "fields"
	p.err = nil
	var labels, values []string
	switch kind {
	case "foundry":
		labels = []string{"Foundry HTTPS endpoint", "Deployment / model", "Tenant (optional)"}
		values = []string{"", "", ""}
	case "copilot":
		labels = []string{"Copilot model"}
		values = []string{"gpt-4.1"}
	case "ollama":
		labels = []string{"Connector name", "Ollama endpoint (reachable from cluster)", "Model"}
		values = []string{"ollama", "http://ollama.ollama.svc.cluster.local:11434", "qwen2.5:3b"}
	case "apikey":
		labels = []string{"Connector name", "Provider type (openai / anthropic)", "API endpoint", "Model", "Existing Kubernetes Secret name", "Secret key name"}
		values = []string{"", "openai", "https://api.openai.com/v1", "", "", "api-key"}
	case "cluster":
		labels = []string{"Model override (empty uses Provider default)"}
		values = []string{p.model}
	}
	for i, label := range labels {
		input := textinput.New()
		input.CharLimit = 512
		input.SetWidth(64)
		input.SetValue(values[i])
		if i == 0 {
			input.Focus()
		}
		p.fields = append(p.fields, consoleInferenceField{label, input})
	}
}

func (p *consoleInferencePane) readFields() error {
	v := func(i int) string { return strings.TrimSpace(p.fields[i].input.Value()) }
	s := p.source
	switch s.Kind {
	case "cluster":
		p.model = v(0)
		return refuseWizardCredentials(p.model)
	case "foundry":
		s.Endpoint, s.Model, s.Tenant = v(0), v(1), v(2)
		s.Name = "Foundry " + s.Model
	case "copilot":
		s.Model = v(0)
		s.Name = "Copilot " + s.Model
	case "ollama":
		s.Name, s.Endpoint, s.Model = v(0), v(1), v(2)
	case "apikey":
		s.Name, s.Provider, s.Endpoint, s.Model, s.Secret, s.SecretKey = v(0), v(1), v(2), v(3), v(4), v(5)
	}
	if err := s.validate(p.agent.Runtime); err != nil {
		return err
	}
	p.source = s
	return nil
}

func (m agentTUIModel) updateInference(msg tea.Msg) (tea.Model, tea.Cmd) {
	p := m.inference
	switch msg := msg.(type) {
	case consoleInferenceLoaded:
		if msg.pane != p {
			return m, nil
		}
		p.snapshot, p.err = msg.snapshot, msg.err
		p.stage = "sources"
		if msg.err != nil {
			p.stage = "done"
		}
		return m, nil
	case consoleInferenceSaved:
		p.err = msg.err
		p.stage = "done"
		if p.cancel != nil {
			p.cancel()
		}
		if !m.opt.Demo {
			return m, m.refresh()
		}
		return m, nil
	case quickstartTickMsg:
		p.frame++
		if p.stage == "loading" || p.stage == "saving" {
			return m, quickstartTick()
		}
		return m, nil
	case tea.WindowSizeMsg:
		for i := range p.fields {
			p.fields[i].input.SetWidth(max(1, min(76, m.width-14)))
		}
		return m, nil
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" || msg.Code == tea.KeyEsc {
			if p.stage == "saving" {
				p.cancelling = true
				p.cancel()
				return m, nil
			}
			m.inference = nil
			return m, nil
		}
		switch p.stage {
		case "loading", "saving":
			return m, nil
		case "done":
			if msg.Code == tea.KeyEnter {
				m.inference = nil
			}
			return m, nil
		case "fields":
			if msg.Code == tea.KeyEnter && p.field == len(p.fields)-1 {
				p.err = p.readFields()
				if p.err != nil {
					return m, nil
				}
				p.stage = "review"
				p.selection = 0
				return m, nil
			}
			if msg.Code == tea.KeyTab || msg.Code == tea.KeyEnter || msg.String() == "shift+tab" {
				p.fields[p.field].input.Blur()
				step := 1
				if msg.String() == "shift+tab" {
					step = -1
				}
				p.field = (p.field + step + len(p.fields)) % len(p.fields)
				return m, p.fields[p.field].input.Focus()
			}
			var cmd tea.Cmd
			p.fields[p.field].input, cmd = p.fields[p.field].input.Update(msg)
			return m, cmd
		default:
			count := len(p.snapshot.Sources) + 1
			if p.stage == "kinds" {
				count = len(p.sourceKinds())
			}
			if p.stage == "review" {
				count = 2
			}
			switch msg.String() {
			case "j", "down", "tab":
				p.selection = (p.selection + 1) % count
			case "k", "up":
				p.selection = (p.selection + count - 1) % count
			case "enter":
				switch p.stage {
				case "sources":
					if p.selection == len(p.snapshot.Sources) {
						p.stage = "kinds"
						p.selection = 0
						return m, nil
					}
					selected := p.snapshot.Sources[p.selection]
					p.source = selected
					if selected.Kind == "cluster" && p.agent.Runtime == "orka" {
						p.model, _ = p.snapshot.Model["name"].(string)
						p.setFields("cluster")
						p.source = selected
						return m, p.fields[0].input.Focus()
					}
					p.stage = "review"
					p.selection = 0
				case "kinds":
					p.setFields(p.sourceKinds()[p.selection])
					return m, p.fields[0].input.Focus()
				case "review":
					if p.selection == 0 {
						m.inference = nil
						return m, nil
					}
					if m.opt.Demo {
						p.stage = "done"
						return m, nil
					}
					p.stage = "saving"
					p.err = nil
					result, cancel := m.startInference(p.env, p.agent, p.snapshot, p.source, p.model)
					p.cancel = cancel
					return m, tea.Batch(quickstartTick(), func() tea.Msg { return <-result })
				}
			}
			return m, nil
		}
	}
	if p.stage == "fields" {
		var cmd tea.Cmd
		p.fields[p.field].input, cmd = p.fields[p.field].input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m agentTUIModel) inferenceView() string {
	p := m.inference
	width := min(88, m.width-8)
	inner := width - 4
	height := min(28, m.height-2)
	fit := func(s string) string { return ansi.Truncate(tuiOneLine(s), inner, "…") }
	rows := []string{fit("INFERENCE · " + p.agent.Name), fit(p.env.Name + " · " + p.agent.Namespace)}
	footer := "j/k ↑/↓ choose · <enter> select · <esc> close"
	var choices []string
	switch p.stage {
	case "sources":
		for _, s := range p.snapshot.Sources {
			choices = append(choices, s.Name+" · "+s.Kind+" · "+s.Model)
		}
		choices = append(choices, "Add inference source…")
	case "kinds":
		for _, kind := range p.sourceKinds() {
			switch kind {
			case "foundry":
				choices = append(choices, "Foundry · host Azure login connector")
			case "copilot":
				choices = append(choices, "Copilot · host GitHub login connector")
			case "ollama":
				choices = append(choices, "Ollama · cluster model endpoint")
			case "apikey":
				choices = append(choices, "API key · cluster Secret-backed connector")
			}
		}
	case "fields":
		rows = append(rows, fit("Add / edit "+p.source.Kind))
		if p.source.Kind == "foundry" {
			rows = append(rows, fit("Uses az login on this host; model calls run from console chat."))
		}
		if p.source.Kind == "copilot" {
			rows = append(rows, fit("Uses installed Copilot CLI and copilot login on this host."))
		}
		rows = append(rows, fit(fmt.Sprintf("%d/%d · %s", p.field+1, len(p.fields), p.fields[p.field].label)))
		input := p.fields[p.field].input
		input.SetWidth(max(1, inner-2))
		rows = append(rows, input.View())
		footer = "<tab> next · shift+tab back · <enter> continue · <esc> cancel"
	case "review":
		rows = append(rows, fit("Source: "+p.source.Name+" · "+p.source.Kind), fit("Model: "+valueOr(p.model, valueOr(p.source.Model, "Provider default"))))
		if p.source.Endpoint != "" {
			rows = append(rows, fit("Endpoint: "+p.source.Endpoint))
		}
		if p.source.Kind == "foundry" || p.source.Kind == "copilot" {
			rows = append(rows, fit("Host chat override; cluster agent stays unchanged."), fit("Verify login and save routing; no inference call."))
		} else {
			rows = append(rows, fit("Server: "+p.snapshot.Server), fit("Save agent configuration; new connectors are create-only."))
		}
		if p.source.Secret != "" {
			rows = append(rows, fit("Secret: "+p.source.Secret+" / "+p.source.SecretKey))
		}
		choices = []string{"Cancel", "Save inference"}
	case "loading", "saving":
		text := "Loading inference sources…"
		if p.stage == "saving" {
			text = "Setting up inference; waiting for readiness…"
		}
		if p.cancelling {
			text = "Cancelling; waiting for work to stop…"
		}
		rows = append(rows, fit([]string{"⠋", "⠙", "⠹", "⠸"}[p.frame%4]+" "+text))
		footer = "<esc> cancel"
	case "done":
		text := "Inference saved"
		if m.opt.Demo {
			text = "DEMO · nothing saved"
		}
		if p.err != nil {
			text = "Inference setup stopped; completed resources are retained."
		}
		rows = append(rows, fit(text))
		footer = "<enter> / <esc> close"
	}
	if p.err != nil {
		rows = append(rows, agentTUIIndentedText(p.err.Error(), inner, 0)...)
	}
	if len(choices) > 0 {
		budget := max(1, height-3-len(rows))
		start := max(0, p.selection-budget+1)
		for i := start; i < min(len(choices), start+budget); i++ {
			text := fit("  " + choices[i])
			if i == p.selection {
				text = pickerSelectedStyle().Width(inner).Render(fit("› " + choices[i]))
			}
			rows = append(rows, text)
		}
	}
	rows = rows[:min(len(rows), height-3)]
	rows = append(rows, fit(footer))
	return lipgloss.NewStyle().Width(width).Padding(0, 1).Background(lipgloss.Color("#18232D")).Foreground(lipgloss.Color("#E6EDF3")).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Cyan).BorderBackground(lipgloss.Color("#18232D")).Render(strings.Join(rows, "\n"))
}
