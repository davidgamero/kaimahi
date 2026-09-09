package app

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/scaffold"
)

type createWizardStep uint8

const (
	createDescription createWizardStep = iota
	createName
	createConfirm
	createDone
)

type createWizardKeys struct {
	Next   key.Binding
	Select key.Binding
	Cancel key.Binding
}

func (k createWizardKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Next, k.Select, k.Cancel}
}

func (k createWizardKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Next, k.Select, k.Cancel}}
}

type createWizardModel struct {
	opt       CreateOptions
	input     textinput.Model
	help      help.Model
	keys      createWizardKeys
	step      createWizardStep
	selection int
	err       error
	cancelled bool
}

func newCreateWizardModel(opt CreateOptions) (createWizardModel, error) {
	if _, err := scaffold.ParseTools(opt.Tools); err != nil {
		return createWizardModel{}, err
	}
	if opt.Name != "" {
		if err := scaffold.ValidateName(opt.Name); err != nil {
			return createWizardModel{}, err
		}
	}

	input := textinput.New()
	input.Prompt = "> "
	input.CharLimit = 0
	input.SetWidth(60)
	styles := input.Styles()
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Cyan).Bold(true)
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)
	styles.Blurred.Placeholder = styles.Focused.Placeholder
	styles.Cursor.Color = lipgloss.Cyan
	input.SetStyles(styles)

	m := createWizardModel{
		opt:   opt,
		input: input,
		help:  help.New(),
		keys: createWizardKeys{
			Next:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "continue")),
			Select: key.NewBinding(key.WithKeys("left", "right", "up", "down", "tab"), key.WithHelp("arrows", "select")),
			Cancel: key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "cancel")),
		},
	}
	m.keys.Select.SetEnabled(false)
	m.startMissingStep()
	return m, nil
}

func (m *createWizardModel) startMissingStep() {
	switch {
	case strings.TrimSpace(m.opt.Description) == "":
		m.step = createDescription
		m.input.Placeholder = "What should this agent do?"
		m.input.Validate = requiredDescription
		m.input.CharLimit = 0
		m.input.SetValue("")
		m.input.Focus()
	case strings.TrimSpace(m.opt.Name) == "":
		m.step = createName
		m.input.Placeholder = "agent-name"
		m.input.Validate = validateAgentName
		m.input.CharLimit = 63
		m.input.SetValue(slugAgentName(m.opt.Description))
		m.input.Focus()
	default:
		m.finishFields()
	}
}

func requiredDescription(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("a description is required")
	}
	return nil
}

func validateAgentName(value string) error {
	return scaffold.ValidateName(strings.TrimSpace(value))
}

func (m *createWizardModel) finishFields() {
	m.input.Blur()
	if m.opt.Instructions == "" && m.opt.Image == "" {
		m.opt.InstructionText = "You are " + m.opt.Name + ". Your purpose is: " + m.opt.Description + "\nAnswer briefly and say plainly when you do not know something."
	}
	if m.opt.Namespace == "" {
		m.opt.Namespace = config.DefaultNamespace
	}
	if m.opt.Out == "" {
		m.opt.Out = filepath.Join("agents", m.opt.Name+".yaml")
	}
	if m.opt.Out == "-" || m.opt.NoApply {
		m.opt.NoApply = true
		m.step = createDone
		return
	}
	if m.opt.DryRun {
		m.step = createDone
		return
	}
	m.step = createConfirm
	m.selection = 0 // Enter intentionally applies by default.
	m.keys.Next.SetHelp("enter", "choose")
	m.keys.Select.SetEnabled(true)
}

func (m createWizardModel) Init() tea.Cmd {
	if m.step == createDescription || m.step == createName {
		return textinput.Blink
	}
	return nil
}

func (m createWizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.help.SetWidth(msg.Width)
		m.input.SetWidth(max(20, min(60, msg.Width-4)))
	case tea.KeyPressMsg:
		if key.Matches(msg, m.keys.Cancel) {
			m.cancelled = true
			m.step = createDone
			return m, tea.Quit
		}
		if m.step == createConfirm {
			switch {
			case key.Matches(msg, m.keys.Select):
				m.selection = 1 - m.selection
				return m, nil
			case key.Matches(msg, m.keys.Next):
				if m.selection == 1 {
					m.cancelled = true
				}
				m.step = createDone
				return m, tea.Quit
			}
		}
		if key.Matches(msg, m.keys.Next) {
			value := strings.TrimSpace(m.input.Value())
			if m.input.Validate != nil {
				m.err = m.input.Validate(value)
			}
			if m.err != nil {
				return m, nil
			}
			switch m.step {
			case createDescription:
				m.opt.Description = value
			case createName:
				m.opt.Name = value
			}
			m.err = nil
			m.startMissingStep()
			if m.step == createDone {
				return m, tea.Quit
			}
			return m, textinput.Blink
		}
	}

	if m.step == createDescription || m.step == createName {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.err = m.input.Err
		return m, cmd
	}
	return m, nil
}

func (m createWizardModel) View() tea.View {
	if m.step == createDone {
		return tea.NewView("")
	}

	var body strings.Builder
	body.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Cyan).Render("Create an agent") + "\n\n")
	switch m.step {
	case createDescription:
		body.WriteString("Description\n")
		body.WriteString(m.input.View())
	case createName:
		body.WriteString("Description: " + displayWizardValue(m.opt.Description) + "\n\nAgent name\n")
		body.WriteString(m.input.View())
	case createConfirm:
		fmt.Fprintf(&body, "Name:        %s\nDescription: %s\nNamespace:   %s\nOutput:      %s\n\nCreate and apply this agent?\n", displayWizardValue(m.opt.Name), displayWizardValue(m.opt.Description), displayWizardValue(m.opt.Namespace), displayWizardValue(m.opt.Out))
		choices := []string{"Apply", "Cancel"}
		for i, choice := range choices {
			marker := "  "
			if i == m.selection {
				marker = "> "
				choice = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Magenta).Render(choice)
			}
			body.WriteString(marker + choice + "  ")
		}
	}
	if m.err != nil {
		body.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Red).Render(m.err.Error()))
	}
	body.WriteString("\n\n" + m.help.View(m.keys) + "\n")
	return tea.NewView(body.String())
}

func runCreateWizard(in io.Reader, out io.Writer, opt CreateOptions) (CreateOptions, error) {
	m, err := newCreateWizardModel(opt)
	if err != nil {
		return opt, err
	}
	if m.step == createDone {
		return m.opt, nil
	}
	result, err := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out)).Run()
	if err != nil {
		if errors.Is(err, tea.ErrInterrupted) || errors.Is(err, tea.ErrProgramKilled) {
			return opt, errCreateCancelled
		}
		return opt, err
	}
	completed, ok := result.(createWizardModel)
	if !ok {
		return opt, fmt.Errorf("agent-create wizard returned unexpected model %T", result)
	}
	if completed.cancelled {
		return opt, errCreateCancelled
	}
	return completed.opt, nil
}

func displayWizardValue(value string) string {
	return strings.Join(strings.Fields(ansi.Strip(value)), " ")
}
