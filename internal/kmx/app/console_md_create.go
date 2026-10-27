package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type consoleMDCreate struct {
	input                                        textarea.Model
	root, name, instructions, model, path, stage string
	proposal                                     MarkdownToolProposal
	choice, scroll                               int
	selected                                     []string
	running, cancelling                          bool
	err                                          error
	cancel                                       context.CancelFunc
}

type consoleMDResult struct {
	pane     *consoleMDCreate
	agent    *MarkdownAgent
	proposal MarkdownToolProposal
	err      error
}

func (m agentTUIModel) openMDCreate(root string) (tea.Model, tea.Cmd) {
	input := textarea.New()
	input.Prompt, input.Placeholder = "", "Agent name"
	input.ShowLineNumbers = false
	input.CharLimit = 32 << 10
	input.SetWidth(max(1, min(84, m.width-8)-4))
	input.SetHeight(3)
	input.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "alt+enter"))
	m.mdCreation = &consoleMDCreate{input: input, root: root, stage: "name"}
	return m, m.mdCreation.input.Focus()
}

func (m *agentTUIModel) retainCreatedDefinition(a *MarkdownAgent) {
	row := agentTUIAgent{Name: a.Name, Runtime: "md", Ready: "created", TestStatus: "untested", Model: mdModelLabel(a.Model), Version: mdShort(a.Revision), DefinitionPath: a.Path, TestsPath: a.TestsPath, SystemPrompt: a.Instructions, PromptSource: "authored definition"}
	for _, name := range a.Tools {
		row.Tools = append(row.Tools, agentTUITool{Name: name, Kind: "unapproved declaration", Disabled: true})
	}
	if m.createdDefinitions == nil {
		m.createdDefinitions = map[string]agentTUIAgent{}
	}
	m.createdDefinitions[a.Path] = row
	for col := range m.columns {
		c := &m.columns[col]
		if c.Env.Directory != m.mdCreation.root {
			continue
		}
		for i, existing := range c.Agents {
			if existing.DefinitionPath == a.Path {
				c.Agents[i], c.Selection = row, i
				return
			}
		}
		c.Agents = append(c.Agents, row)
		c.Selection = len(c.Agents) - 1
	}
}

func (m agentTUIModel) updateMDCreate(msg tea.Msg) (tea.Model, tea.Cmd) {
	p := m.mdCreation
	switch event := msg.(type) {
	case consoleMDResult:
		if event.pane != p || !p.running {
			return m, nil
		}
		p.running = false
		if p.cancel != nil {
			p.cancel()
			p.cancel = nil
		}
		p.err = event.err
		if event.agent != nil {
			p.path = event.agent.Path
			m.retainCreatedDefinition(event.agent)
		}
		if p.cancelling || event.err != nil {
			p.stage = "done"
			return m, nil
		}
		switch p.stage {
		case "creating":
			p.stage = "offer"
		case "suggesting":
			p.proposal, p.choice, p.scroll = event.proposal, 0, 0
			p.stage = "choices"
			if len(p.proposal.CLIChoices) == 0 {
				p.stage = "done"
			}
		case "saving":
			p.stage = "done"
		}
		return m, nil
	case tea.WindowSizeMsg:
		p.input.SetWidth(max(1, min(84, m.width-8)-4))
		return m, nil
	case tea.KeyPressMsg:
		if event.Code == tea.KeyEsc || event.String() == "ctrl+c" {
			if p.running {
				p.cancelling = true
				if p.cancel != nil {
					p.cancel()
				}
				return m, nil
			}
			m.mdCreation = nil
			m.status = "Creation closed"
			if p.path != "" {
				m.status = "Created " + p.name + " · not running · i inspect / Enter actions"
			}
			return m, nil
		}
		if p.running {
			return m, nil
		}
		if p.stage == "choices" {
			rows := m.mdChoiceLines()
			page := max(1, min(24, m.height-4)-6)
			end := max(0, len(rows)-page)
			switch event.Code {
			case tea.KeyDown:
				p.scroll = min(end, p.scroll+1)
			case tea.KeyUp:
				p.scroll = max(0, p.scroll-1)
			case tea.KeyPgDown:
				p.scroll = min(end, p.scroll+page)
			case tea.KeyPgUp:
				p.scroll = max(0, p.scroll-page)
			case tea.KeyEnd:
				p.scroll = end
			case 'y', 'n':
				choice := p.proposal.CLIChoices[p.choice]
				if event.Code == 'y' {
					if p.scroll < end {
						p.err = fmt.Errorf("read the complete declaration before accepting (End to bottom)")
						return m, nil
					}
					if choice.Tool == nil || len(choice.MissingScope) > 0 {
						p.err = fmt.Errorf("supply missing scope in the definition and suggest again")
						return m, nil
					}
					p.selected = append(p.selected, choice.Tool.Name)
				}
				p.err, p.scroll = nil, 0
				p.choice++
				if p.choice == len(p.proposal.CLIChoices) {
					p.stage = "accept"
				}
			}
			return m, nil
		}
		if p.stage == "offer" {
			if event.Code == 'n' || event.Code == tea.KeyEnter {
				p.stage = "done"
				return m, nil
			}
			if event.Code == 'y' {
				path, model := p.path, p.model
				return m.mdCreateJob("suggesting", func(ctx context.Context) consoleMDResult {
					proposal, err := SuggestCLICommandTools(ctx, path, model)
					return consoleMDResult{proposal: proposal, err: err}
				})
			}
			return m, nil
		}
		if p.stage == "done" {
			if event.Code == tea.KeyEnter {
				m.mdCreation = nil
				m.status = "Creation closed; no completed creation reported"
				if p.path != "" {
					m.status = "Created " + p.name + " · not running · Enter actions to edit/test/chat"
				}
			}
			return m, nil
		}
		if event.Code == tea.KeyEnter && event.Mod == 0 {
			value := strings.TrimSpace(p.input.Value())
			p.err = nil
			switch p.stage {
			case "name":
				if !markdownAgentName.MatchString(value) {
					p.err = fmt.Errorf("use a safe 1-63 character name")
					return m, nil
				}
				p.name, p.stage = value, "instructions"
				p.input.Reset()
				p.input.Placeholder = "What should this agent do? Include exact project/resource scope."
			case "instructions":
				if value == "" {
					p.err = fmt.Errorf("instructions are required")
					return m, nil
				}
				p.instructions, p.stage = p.input.Value(), "model"
				p.input.Reset()
				p.input.Placeholder = "Optional Copilot model ID; Enter uses host default"
			case "model":
				if value != "" && !localModelName.MatchString(value) {
					p.err = fmt.Errorf("use a valid model ID or leave empty")
					return m, nil
				}
				p.model, p.stage = value, "confirm"
			case "confirm":
				if m.opt.Demo {
					p.stage = "done"
					p.err = fmt.Errorf("demo: no files created")
					return m, nil
				}
				root, name, instructions, model := p.root, p.name, p.instructions, p.model
				return m.mdCreateJob("creating", func(ctx context.Context) consoleMDResult {
					if err := ctx.Err(); err != nil {
						return consoleMDResult{err: err}
					}
					if err := CreateMarkdownDraft(root, name, instructions, model); err != nil {
						return consoleMDResult{err: fmt.Errorf("creation stopped; inspect destination files before retrying: %w", err)}
					}
					a, err := ReadMarkdownAgent(filepath.Join(root, ".github", "agents", name+".agent.md"))
					return consoleMDResult{agent: a, err: err}
				})
			case "accept":
				if len(p.selected) == 0 {
					p.stage = "done"
					return m, nil
				}
				proposal, names := p.proposal, append([]string(nil), p.selected...)
				return m.mdCreateJob("saving", func(ctx context.Context) consoleMDResult {
					if err := AddSuggestedCLITools(ctx, proposal, names); err != nil {
						return consoleMDResult{err: err}
					}
					a, err := ReadMarkdownAgent(proposal.Source)
					return consoleMDResult{agent: a, err: err}
				})
			}
			return m, nil
		}
	}
	if p.stage == "name" || p.stage == "instructions" || p.stage == "model" {
		var cmd tea.Cmd
		p.input, cmd = p.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m agentTUIModel) mdCreateJob(stage string, work func(context.Context) consoleMDResult) (tea.Model, tea.Cmd) {
	if m.startMDJob == nil {
		m.mdCreation.err = fmt.Errorf("creation worker unavailable")
		return m, nil
	}
	m.mdCreation.stage, m.mdCreation.running = stage, true
	return m, m.startMDJob(m.mdCreation, work)
}

func (m agentTUIModel) mdChoiceLines() []string {
	p := m.mdCreation
	var text strings.Builder
	_ = RenderCLIChoice(&text, p.proposal.CLIChoices[p.choice])
	return strings.Split(ansi.Hardwrap(safeTerminal(text.String()), max(1, min(84, m.width-8)-4), true), "\n")
}

func (m agentTUIModel) mdCreationView() string {
	p := m.mdCreation
	width, height := max(12, min(84, m.width-8)), max(8, min(24, m.height-4))
	rows := []string{"CREATE LOCAL MARKDOWN AGENT", "Directory: " + safeTerminal(p.root)}
	switch p.stage {
	case "name", "instructions", "model":
		rows = append(rows, strings.ToUpper(p.stage), p.input.View(), "Enter next · Shift+Enter newline · Esc cancel")
	case "confirm":
		rows = append(rows, "Name: "+p.name+" · Model: "+mdModelLabel(p.model), "Creates definition + empty tests; no execution.", "Enter create · Esc cancel")
	case "offer":
		rows = append(rows, "Created · not running · no tests yet", "Suggest tools? Sends definition and executable-name", "PATH availability to Copilot. No candidate commands run.", "y suggest · n / Enter skip · Esc close")
	case "choices":
		body := m.mdChoiceLines()
		page := max(1, height-6)
		start := min(p.scroll, max(0, len(body)-page))
		rows = append(rows, fmt.Sprintf("Tool %d/%d", p.choice+1, len(p.proposal.CLIChoices)))
		rows = append(rows, body[start:min(len(body), start+page)]...)
		rows = append(rows, "↑↓/PgUp/PgDn read · y select · n skip · Esc discard")
	case "accept":
		rows = append(rows, "Selected: "+strings.Join(p.selected, ", "), "Enter saves declarations only; no grants or probes.", "Esc discards selections; created files remain.")
	case "done":
		if p.path != "" {
			rows = append(rows, "Created definition: "+safeTerminal(p.path), "Not running. Add reviewed tests with Edit tests.")
		} else {
			rows = append(rows, "No completed creation reported.")
		}
		for _, gap := range p.proposal.Gaps {
			rows = append(rows, "Suggestion gap: "+safeTerminal(gap))
		}
		rows = append(rows, "CLI tools need: kmx tools approve FILE --apply", "Enter closes and selects the definition · Esc close")
	default:
		rows = append(rows, p.stage+"…", "Esc cancels; waiting for the operation to finish")
	}
	if p.err != nil {
		rows = append(rows[:2], append([]string{"Error: " + safeTerminal(p.err.Error())}, rows[2:]...)...)
	}
	var lines []string
	for _, row := range rows {
		for _, line := range strings.Split(row, "\n") {
			lines = append(lines, ansi.Truncate(line, width-4, "…"))
		}
	}
	lines = lines[:min(len(lines), height-2)]
	return lipgloss.NewStyle().Width(width).Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Cyan).Render(strings.Join(lines, "\n"))
}
