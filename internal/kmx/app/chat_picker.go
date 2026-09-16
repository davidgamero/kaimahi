package app

import (
	"context"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type chatPickerItem struct {
	name, detail    string
	enabled, locked bool
}

type chatPicker struct {
	title, query                               string
	items                                      []chatPickerItem
	selection                                  int
	multiple, accepted, cancelled, interrupted bool
	width                                      int
}

func (m chatPicker) matches() []int {
	var found []int
	for i, item := range m.items {
		if strings.Contains(strings.ToLower(item.name+" "+item.detail), strings.ToLower(m.query)) {
			found = append(found, i)
		}
	}
	return found
}

func (m chatPicker) Init() tea.Cmd { return nil }
func (m chatPicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = size.Width
		return m, nil
	}
	if _, ok := msg.(createWizardCancelMsg); ok {
		m.cancelled, m.interrupted = true, true
		return m, tea.Quit
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	if key.String() == "ctrl+c" {
		m.cancelled, m.interrupted = true, true
		return m, tea.Quit
	}
	found := m.matches()
	switch key.Code {
	case tea.KeyEsc:
		m.cancelled = true
		return m, tea.Quit
	case tea.KeyUp:
		if len(found) > 0 {
			m.selection = (m.selection + len(found) - 1) % len(found)
		}
	case tea.KeyDown, tea.KeyTab:
		if len(found) > 0 {
			m.selection = (m.selection + 1) % len(found)
		}
	case tea.KeyEnter:
		if m.multiple || len(found) > 0 {
			m.accepted = true
			return m, tea.Quit
		}
	case tea.KeySpace:
		if m.multiple && len(found) > 0 {
			item := &m.items[found[m.selection]]
			if !item.locked {
				item.enabled = !item.enabled
			}
		} else {
			m.query += " "
			m.selection = 0
		}
	case tea.KeyBackspace:
		_, size := utf8.DecodeLastRuneInString(m.query)
		m.query = m.query[:len(m.query)-size]
		m.selection = 0
	default:
		if key.Text != "" && len(m.query) < 200 {
			m.query += key.Text
			m.selection = 0
		}
	}
	return m, nil
}

func (m chatPicker) View() tea.View {
	var body strings.Builder
	fmt.Fprintf(&body, "%s\n\nSearch: %s\n\n", m.title, safeTerminal(m.query))
	found := m.matches()
	start := max(0, m.selection-7)
	for pos := start; pos < min(len(found), start+10); pos++ {
		item := m.items[found[pos]]
		cursor, check := "  ", ""
		if pos == m.selection {
			cursor = "› "
		}
		if m.multiple {
			check = "[ ] "
			if item.enabled {
				check = "[x] "
			}
		}
		row := cursor + check + safeTerminal(item.name) + "  " + strings.Join(strings.Fields(safeTerminal(item.detail)), " ")
		if m.width > 0 {
			row = ansi.Truncate(row, max(1, m.width-1), "…")
		}
		fmt.Fprintln(&body, row)
	}
	if len(found) == 0 {
		body.WriteString("No matches\n")
	}
	if m.multiple {
		body.WriteString("\nspace toggle · enter save")
	} else {
		body.WriteString("\nenter connect (resets chat)")
	}
	fmt.Fprintf(&body, " · arrows select · type to search · esc back · ctrl+c exit\n%d matches\n", len(found))
	return tea.NewView(body.String())
}

func runChatPicker(ctx context.Context, in io.Reader, out io.Writer, picker chatPicker) (chatPicker, error) {
	filter := func(model tea.Model, msg tea.Msg) tea.Msg {
		switch msg.(type) {
		case tea.InterruptMsg:
			return createWizardCancelMsg{}
		case tea.QuitMsg:
			m := model.(chatPicker)
			if !m.accepted && !m.cancelled {
				return createWizardCancelMsg{}
			}
		}
		return msg
	}
	result, err := tea.NewProgram(picker, tea.WithInput(in), tea.WithOutput(out), tea.WithContext(ctx), tea.WithFilter(filter)).Run()
	if ctx.Err() != nil {
		return picker, ctx.Err()
	}
	if err != nil {
		return picker, err
	}
	m := result.(chatPicker)
	if m.interrupted {
		return m, context.Canceled
	}
	return m, nil
}
