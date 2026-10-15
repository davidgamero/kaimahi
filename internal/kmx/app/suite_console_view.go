package app

import (
	"fmt"
	"os"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
)

var suiteConsoleTabs = []string{"Source", "Members", "Cases", "Artifacts", "Destinations", "Activity"}

func (m *suiteConsole) suiteActions() []agentTUIMenuAction {
	e := m.entry()
	if e == nil {
		return nil
	}
	a := []agentTUIMenuAction{{"i", "Inspect source / member"}}
	workspace := e.Review != nil
	standalone := e.Live != nil && !e.Live.SuiteOwned && !e.Live.External
	placed := e.Deployment != ""
	if workspace {
		a = append(a, agentTUIMenuAction{"d", "Edit authored source"}, agentTUIMenuAction{"e", "Prove complete suite"}, agentTUIMenuAction{"p", "Publish exact artifact"}, agentTUIMenuAction{"l", "Lift to destination"})
	}
	if e.Copilot != nil && !placed {
		a = append(a, agentTUIMenuAction{"u", "Run locally with Copilot CLI"}, agentTUIMenuAction{"c", "Chat locally with Copilot CLI"})
		a = append(a, agentTUIMenuAction{"H", "Reconnect Copilot after source review"})
	}
	if placed {
		a = append(a, agentTUIMenuAction{"s", "Revalidate placement"})
	}
	if e.Reference != "" {
		a = append(a, agentTUIMenuAction{"v", "Validate destination runtime"})
	}
	if placed {
		a = append(a, agentTUIMenuAction{"u", "Run member once"}, agentTUIMenuAction{"c", "Chat with member"})
		if !e.Image {
			a = append(a, agentTUIMenuAction{"R", "Inspect recent runs"})
		}
	}
	if standalone {
		a = append(a, agentTUIMenuAction{"c", "Chat with agent"})
		a = append(a, agentTUIMenuAction{"x", "Extract AgentSuite (keep original)"})
	}
	return a
}

func (m *suiteConsole) formCompletions() []string {
	if m.form == nil {
		return nil
	}
	var choices []string
	if m.form.action == "azure-select" {
		for _, row := range m.azure {
			if m.form.field == 0 {
				choices = append(choices, row.Context)
			} else if row.Context == m.form.values[0] {
				for _, registry := range row.Registries {
					choices = append(choices, registry.LoginServer)
				}
			}
		}
	}
	if m.form.action == "command" {
		choices = []string{"open", "create", "contexts", "suite extract", "suite show", "eval", "push", "lift", "runtime check", "runtime install", "suite status", "run", "chat", "edit", "refresh", "help", "exit"}
	} else if m.form.action == "eval" && m.form.field == 0 || m.form.action == "lift" && m.form.field == 2 {
		if e := m.entry(); e != nil && e.Root != "" {
			if d, err := config.LoadDestinations(e.Root); err == nil && d != nil {
				for name := range d.Targets {
					choices = append(choices, name)
				}
			}
		}
	}
	prefix := strings.TrimPrefix(m.input.Value(), "/")
	if m.resourceSelectionForm() {
		sort.Strings(choices)
		items := make([]chatPickerItem, 0, len(choices))
		unique := make([]string, 0, len(choices))
		for _, c := range choices {
			if len(unique) > 0 && unique[len(unique)-1] == c {
				continue
			}
			unique = append(unique, c)
			items = append(items, chatPickerItem{name: c, detail: m.resourceChoiceDetail(c)})
		}
		picker := chatPicker{items: items, query: m.input.Value(), searchEnabled: true}
		var matches []string
		for _, i := range picker.matches() {
			matches = append(matches, unique[i])
		}
		return matches
	}
	var matches []string
	for _, c := range choices {
		if strings.HasPrefix(c, prefix) {
			matches = append(matches, c)
		}
	}
	sort.Strings(matches)
	return matches
}

func (m *suiteConsole) resourceSelectionForm() bool {
	return m.form != nil && (m.form.action == "azure-select" || m.form.action == "eval" && m.form.field == 0 || m.form.action == "lift" && m.form.field == 2)
}

func (m *suiteConsole) resourceChoiceDetail(choice string) string {
	if m.form == nil || m.form.action != "azure-select" {
		return ""
	}
	for _, row := range m.azure {
		if m.form.field == 0 && row.Context == choice {
			return row.Cluster.Name + " · " + row.Cluster.ResourceGroup
		}
		if m.form.field == 1 && row.Context == m.form.values[0] {
			for _, registry := range row.Registries {
				if registry.LoginServer == choice {
					return registry.Name + " · " + registry.Pull
				}
			}
		}
	}
	return ""
}

func (m *suiteConsole) paneGeometry() (left, body, detail int) {
	body = max(8, m.height-10)
	left = m.width / 2
	detail = max(3, body-max(4, body/2))
	return
}
func (m *suiteConsole) detailWidth() int {
	left, _, _ := m.paneGeometry()
	if m.focus == 0 {
		return left
	}
	return m.width - left
}
func (m *suiteConsole) maxDetailScroll() int {
	_, _, height := m.paneGeometry()
	return max(0, len(m.suiteDetailRows(m.detailWidth(), suiteConsoleTheme{}))-max(1, height-4))
}

// Review geometry is shared by rendering and paging so clipped source or plans
// cannot be accepted with an Enter that assumes a taller unframed viewport.
func (m *suiteConsole) reviewGeometry() (width, height, page int) {
	width = max(12, min(88, m.width-6))
	height = max(8, m.height-4)
	page = max(1, height-8)
	return
}
func (m *suiteConsole) reviewText() (string, []string) {
	w, _, _ := m.reviewGeometry()
	var title, text string
	switch {
	case m.extraction != nil:
		s := m.extraction.Summary
		title = "EXTRACT AGENTSUITE"
		text = "Original: " + s.Context + " / " + s.Namespace + " / " + s.Agent + "\nNew member: " + s.Member + "\nWorkspace: " + s.Workspace + "\nModel: " + s.Model + "\n\nINSTRUCTIONS (future published content)\n" + s.Instructions + "\n\nSave new source only. The original remains unchanged.\nGenerated cases are examples and require author review."
	case m.plan != nil:
		s := m.plan.Summary()
		title = "LIFT / EXACT IDENTITY REVIEW"
		text = s.Suite + " / " + s.Member + "\nArtifact: " + s.SuiteDigest + "\nDestination: " + s.Target.Context + " / " + s.Target.Namespace + "\nCluster UID: " + s.Target.ClusterUID + "\nNamespace UID: " + s.Target.NamespaceUID + "\nRuntime: " + s.Runtime + "\nModel: " + s.Model + "\nInference: " + s.Inference + "\nGate: " + s.Gate + "\nEvidence: " + s.EvidenceRunID + "\nReview digest: " + s.PlanDigest + "\n"
		for _, r := range s.Resources {
			text += "\n" + r.Kind + "/" + r.Name + ": " + r.Action + "\n  UID: " + valueOr(r.UID, "absent")
		}
		text += "\n\nSource evaluation is not destination evaluation.\nApply is single-use; partial resources may remain."
	case m.review != nil:
		title = "SOURCE / CASE REVIEW"
		if m.page == 0 {
			return title, m.review.sourceLines(w - 4)
		}
		return fmt.Sprintf("CASE REVIEW %d/%d", m.page, len(m.review.inventory)), m.review.caseLines(m.page-1, w-4)
	}
	return title, strings.Split(ansi.Hardwrap(safeTerminal(text), max(1, w-4), true), "\n")
}
func (m *suiteConsole) scrollReview(key string) bool {
	_, rows := m.reviewText()
	_, _, page := m.reviewGeometry()
	end := max(0, len(rows)-page)
	switch key {
	case "down", "j":
		m.scroll = min(end, m.scroll+1)
	case "up", "k":
		m.scroll = max(0, m.scroll-1)
	case "pgdown":
		m.scroll = min(end, m.scroll+page)
	case "pgup":
		m.scroll = max(0, m.scroll-page)
	case "home":
		m.scroll = 0
	case "end":
		m.scroll = end
	default:
		return false
	}
	return true
}
func (m *suiteConsole) advanceReview() bool {
	_, rows := m.reviewText()
	_, _, page := m.reviewGeometry()
	end := max(0, len(rows)-page)
	if m.scroll < end {
		m.scroll = min(end, m.scroll+page)
		return true
	}
	return false
}

type suiteConsoleTheme struct{ accent, muted, text, selected, success, warning, shortcut, field, danger lipgloss.Style }

func newSuiteConsoleTheme() suiteConsoleTheme {
	t := suiteConsoleTheme{}
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return t
	}
	t.accent = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Cyan)
	t.muted = lipgloss.NewStyle().Foreground(lipgloss.Color("#8293A7"))
	t.text = lipgloss.NewStyle().Foreground(lipgloss.Color("#E6EDF3"))
	t.selected = lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("#24465A")).Foreground(lipgloss.Color("#D7F5FF"))
	t.success = lipgloss.NewStyle().Foreground(lipgloss.Green)
	t.warning = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	t.shortcut = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF9D3D"))
	t.field = lipgloss.NewStyle().Foreground(lipgloss.Color("#79C0FF")).Bold(true)
	t.danger = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF6B6B")).Bold(true)
	return t
}

// Only trusted key-hint text is passed here; never style authored content as keys.
func (t suiteConsoleTheme) property(label, value string) string {
	return t.field.Render(label+":") + " " + t.text.Render(safeTerminal(value))
}

func (t suiteConsoleTheme) actionRow(key, label string, width int) string {
	keyStyle := t.shortcut
	if os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" {
		keyStyle = keyStyle.Background(lipgloss.Color("#24465A"))
	}
	prefix := "› "
	text := ansi.Truncate("  "+safeTerminal(label), max(0, width-lipgloss.Width(prefix+key)), "")
	padding := strings.Repeat(" ", max(0, width-lipgloss.Width(prefix+key+text)))
	return t.selected.Render(prefix) + keyStyle.Render(key) + t.selected.Render(text+padding)
}

func suiteShortcutHints(text string, t suiteConsoleTheme) string {
	parts := strings.Split(text, " · ")
	for i, p := range parts {
		key, rest, ok := strings.Cut(p, " ")
		if ok {
			parts[i] = t.shortcut.Render(key) + " " + rest
		} else {
			parts[i] = t.shortcut.Render(p)
		}
	}
	return strings.Join(parts, " · ")
}

// Input is sanitized before styling. ANSI-aware clipping afterward preserves
// trusted pane/selection styles without allowing content to redraw the terminal.
func suitePane(title string, rows []string, width, height int, active bool, t suiteConsoleTheme) string {
	inner := max(1, width-4)
	contentHeight := max(1, height-2)
	lines := []string{t.accent.Render(ansi.Truncate(title, inner, "…"))}
	for _, row := range rows {
		for _, line := range strings.Split(row, "\n") {
			lines = append(lines, ansi.Truncate(line, inner, "…"))
		}
	}
	lines = lines[:min(len(lines), contentHeight)]
	for len(lines) < contentHeight {
		lines = append(lines, "")
	}
	style := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Width(width)
	if os.Getenv("NO_COLOR") == "" {
		if active {
			style = style.BorderForeground(lipgloss.Cyan)
		} else {
			style = style.BorderForeground(lipgloss.Color("#405265"))
		}
	}
	return style.Render(strings.Join(lines, "\n"))
}

func (m *suiteConsole) suiteListRows(width, height int, t suiteConsoleTheme) []string {
	rows := []string{t.muted.Render(fmt.Sprintf("%d entries · ↑↓ select", len(m.entries))), ""}
	if len(m.entries) == 0 {
		return append(rows, "No suite selected.", "o  Open workspace", "n  Create one-member suite", "t  Browse cluster agents")
	}
	count := max(1, (height-5)/4)
	start := max(0, m.selected-count+1)
	for i := start; i < min(len(m.entries), start+count); i++ {
		e := m.entries[i]
		member := e.Name
		if e.Review != nil {
			member = e.Review.member()
		}
		if e.Live != nil {
			member = e.Live.Name
		}
		prefix := "  "
		if i == m.selected {
			prefix = "› "
		}
		name := ansi.Truncate(prefix+tuiOneLine(e.Name), max(1, width-4), "…")
		if i == m.selected {
			name = t.selected.Width(max(1, width-4)).Render(name)
		} else {
			name = t.text.Render(name)
		}
		rows = append(rows, name, t.muted.Render(ansi.Truncate("  "+tuiOneLine(e.Origin), max(1, width-4), "…")), "  └ "+tuiOneLine(member), "")
	}
	return rows
}

func (m *suiteConsole) liveColumnView(side, width, height int, t suiteConsoleTheme) string {
	x := 0
	if side == 1 {
		x = m.width / 2
	}
	m.mouseRegion(suiteMouseRegion{x, 2, width, height, "side", side})
	m.mouseRegion(suiteMouseRegion{x + 2, 4, width - 4, 1, "context", side})
	column := m.columns[side]
	label := "LOCAL"
	if side == 1 {
		label = "REMOTE"
	}
	if side == 0 && m.authoringView {
		label = "LOCAL SOURCE FILES"
	}
	if m.bootLoading && (m.refreshing || m.refreshGeneration == 0) {
		barWidth := max(4, min(28, width-8))
		cells := make([]string, barWidth)
		for i := range cells {
			cells[i] = "─"
		}
		for i := 0; i < 4; i++ {
			cells[(m.bootFrame+i)%barWidth] = "━"
		}
		rows := []string{"Discovering environments…", "", t.accent.Render("[" + strings.Join(cells, "") + "]"), "Loading contexts and agents…"}
		return suitePane(label+" · LOADING", rows, width, height, side == m.focus, t)
	}
	rows := []string{t.muted.Render(tuiOneLine(valueOr(column.Env.Name, "No context selected")))}
	if side == 0 && m.authoringView {
		rows[0] = t.muted.Render("Authoring files · not deployed to kind")
	}
	if column.Error != "" {
		rows = append(rows, t.warning.Render(tuiOneLine(column.Error)))
	} else if !column.Observed.IsZero() {
		rows = append(rows, t.muted.Render("Observed "+column.Observed.Format("15:04:05")))
	} else {
		m.mouseRegion(suiteMouseRegion{x + 2, 5, 18, 1, "context", side})
		m.mouseRegion(suiteMouseRegion{x + 23, 5, max(0, width-25), 1, "refresh", side})
		rows = append(rows, suiteShortcutHints("t selects contexts · r refreshes", t))
	}
	indices := m.sideIndices(side)
	total := m.sideTotal(side)
	count := fmt.Sprintf("%d total", total)
	if m.listQuery[side] != "" {
		count = fmt.Sprintf("%d / %d matching", len(indices), total)
	}
	m.mouseRegion(suiteMouseRegion{x + 2, 4 + len(rows), width - 4, 1, "sort", side})
	rows = append(rows, t.muted.Render(count+" · ")+t.shortcut.Render("S")+" "+suiteListSorts[m.listSort[side]])
	m.mouseRegion(suiteMouseRegion{x + 2, 4 + len(rows), width - 4, 1, "search", side})
	query := m.listQuery[side]
	if m.listSearching && m.focus == side {
		query += "▏"
	}
	rows = append(rows, t.shortcut.Render("f")+" Find agents: "+safeTerminal(query))
	if m.listSearching && m.focus == side {
		rows[len(rows)-1] = t.selected.Width(max(1, width-4)).Render("› Find agents: " + safeTerminal(query))
	}
	if side == 0 {
		m.mouseRegion(suiteMouseRegion{x + 2, 4 + len(rows), width - 4, 1, "authoring", 0})
		hint := "w view source files"
		if m.authoringView {
			hint = "w return to running kind agents"
		}
		rows = append(rows, suiteShortcutHints(hint, t))
	}
	pos := min(m.columnSelection[side], max(0, len(indices)-1))
	visible := max(1, (height/2-3)/3)
	if m.tab == 0 {
		visible = max(1, (height-len(rows)-3)/3)
	}
	start := max(0, pos-visible+1)
	if len(indices) == 0 {
		message := "No agents or suites"
		if side == 0 && !m.authoringView {
			message = "No Ready agents observed in this kind context"
		}
		if total > 0 {
			message = "No matching agents or suites"
		}
		if column.Error != "" {
			message = "Agents and suites unavailable"
		}
		rows = append(rows, "", message)
		if total == 0 {
			m.mouseRegion(suiteMouseRegion{x + 2, 4 + len(rows), width - 4, 1, "new", side})
		}
		newAgent := "  New agent"
		if side == m.focus {
			newAgent = t.selected.Render("› New agent")
		}
		if total == 0 {
			rows = append(rows, newAgent)
		}
		if side == 0 {
			m.mouseRegion(suiteMouseRegion{x + 2, 4 + len(rows), width - 4, 1, "new", side})
			m.mouseRegion(suiteMouseRegion{x + 35, 4 + len(rows), max(0, width-37), 1, "open", side})
			rows = append(rows, suiteShortcutHints("Enter new agent · n new agent · o open source", t))
			if column.Env.Name == "" {
				m.mouseRegion(suiteMouseRegion{x + 2, 4 + len(rows), width - 4, 1, "context", side})
				rows = append(rows, suiteShortcutHints("t select or initialize local environment", t))
			}
		} else {
			m.mouseRegion(suiteMouseRegion{x + 2, 4 + len(rows), width - 4, 1, "context", side})
			rows = append(rows, suiteShortcutHints("Enter new agent · t select remote context", t))
		}
	}
	for j := start; j < min(len(indices), start+visible); j++ {
		m.mouseRegion(suiteMouseRegion{x + 2, 4 + len(rows), width - 4, 3, "entry", indices[j]})
		e := m.entries[indices[j]]
		member := e.Name
		if e.Live != nil {
			member = e.Live.Name
		} else if e.Review != nil {
			member = e.Review.member()
		}
		prefix := "  "
		if j == pos {
			prefix = "› "
		}
		name := ansi.Truncate(fmt.Sprintf("%s%d. %s · %s", prefix, j+1, tuiOneLine(e.Name), tuiOneLine(e.inferenceLabel())), max(1, width-4), "…")
		if j == pos && side == m.focus && !m.listSearching {
			name = t.selected.Width(max(1, width-4)).Render(name)
		}
		origin := e.Origin
		if e.Deployment == "" && e.Review != nil {
			origin = "Local source · " + e.Root
		}
		rows = append(rows, name, "  └ "+tuiOneLine(member), t.muted.Render(ansi.Truncate("    └ "+tuiOneLine(origin), max(1, width-4), "…")))
	}
	if m.tab != 0 {
		rows = append(rows, t.muted.Render(strings.Repeat("─", max(1, width-4))), t.accent.Render(strings.ToUpper(suiteConsoleTabs[m.tab])))
		projection := *m
		projection.focus = side
		projection.selected = -1
		if len(indices) > 0 {
			projection.selected = indices[pos]
		}
		details := projection.suiteDetailRows(width, t)
		available := max(1, height-len(rows)-4)
		scroll := 0
		if side == m.focus {
			scroll = min(m.detailScroll, max(0, len(details)-available))
		}
		rows = append(rows, details[scroll:min(len(details), scroll+available)]...)
	}
	if side == 0 && m.authoringView {
		return suitePane(label, rows, width, height, side == m.focus, t)
	}
	return suitePane(label+" · SUITES / MEMBERS", rows, width, height, side == m.focus, t)
}

func (m *suiteConsole) suiteDetailRows(width int, t suiteConsoleTheme) []string {
	e := m.entry()
	if e == nil {
		return []string{}
	}
	var text string
	{
		switch m.tab {
		case 0:
			text = "Workspace: " + valueOr(e.Root, "not available")
			if e.Review != nil {
				text += "\n\nMember: " + e.Review.member() + "\nProfile: " + e.Review.profile() + "\nModel: " + e.Review.model() + "\n\nINSTRUCTIONS\n" + suiteReviewPreview(string(e.Review.instructions())) + "\n\nInspect reviews source and every case; d edits files."
			} else if e.Live != nil {
				text += "\n\n" + e.Origin + "\n" + e.Live.SystemPrompt + "\n\nx extracts new source; the original is preserved."
			}
		case 1:
			text = "Member: " + e.Name
			if e.Review != nil {
				text = "Member: " + e.Review.member() + "\nProfile: " + e.Review.profile() + "\nModel: " + e.Review.model()
			}
			if e.Live != nil {
				text = "Member: " + e.Live.Name + "\nRuntime: " + e.Live.Runtime + "\nObserved ready: " + e.Live.Ready
			}
			if e.Deployment != "" {
				text += "\n\nRun · Chat · Recent runs\nPlacement must be verified before suite member sends."
			} else {
				text += "\n\nExtract AgentSuite into new source to enter the suite lifecycle."
			}
		case 2:
			text = "AUTHORED CASES\n"
			if e.Review != nil {
				for _, c := range e.Review.inventory {
					state := "not run in this view"
					icon := "○"
					if e.Root != "" && e.Root == m.progressRoot {
						for _, event := range m.progress {
							if event.Stage == "case" && event.Object == c.Case.ID {
								state = event.State
								icon = "●"
								if state == "pass" {
									icon = "✓"
								}
								if state == "fail" {
									icon = "✗"
								}
							}
						}
					}
					text += "\n" + icon + " " + c.Case.ID + " · " + state + "\n  " + c.Metadata.CaseDigest
				}
			} else {
				text += "\nNo source case inventory selected."
			}
			text += "\n\nEvidence: " + valueOr(e.Evidence, "none selected") + "\nRechecked on consumption; presence is not current proof."
		case 3:
			text = "ARTIFACT\n" + valueOr(e.Reference, "not published") + "\n\nLocal layout: " + valueOr(e.Layout, "none") + "\n\nNative Orka requires no member image.\nPublication and placement are separate operations."
		case 4:
			text = "NAMED DESTINATIONS\n"
			if e.Root != "" {
				text += m.destinationHints(e.Root)
			} else {
				text += "No authored workspace selected."
			}
			text += "\n\nPLACEMENT\n" + valueOr(e.Deployment, "none retained") + "\nContext: " + valueOr(e.Context, "not selected") + "\n\ns revalidates status. Ready is not an answer."
		case 5:
			text = "ACTIVITY\n\nR opens recent member runs.\nHistorical identity is separate from current placement."
			for _, e := range m.progress {
				text += "\n" + e.At.Format("15:04:05") + "  " + e.Stage + " · " + e.Object + " · " + e.State
			}
			if m.detail != "" {
				text += "\n\nLAST RESULT\n" + m.detail
			}
		}
	}
	var rows []string
	for _, line := range strings.Split(safeTerminal(text), "\n") {
		for _, part := range strings.Split(ansi.Hardwrap(line, max(1, width-4), true), "\n") {
			style := t.text
			if strings.HasPrefix(part, "✓") {
				style = t.success
			} else if strings.HasPrefix(part, "○") {
				style = t.muted
			}
			rows = append(rows, style.Render(part))
		}
	}
	return rows
}

func (m *suiteConsole) View() tea.View {
	if m.runs != nil {
		return m.runs.View()
	}
	w, h := max(1, m.width), max(1, m.height)
	if w < 54 || h < 16 {
		v := suiteConsoleRowsView([]string{"KMX · AGENTSUITES", "Resize to at least 54×16", "Ctrl+C quits"}, w, h, "")
		v.AltScreen = true
		return v
	}
	t := newSuiteConsoleTheme()
	title := "KMX · AGENTSUITES"
	header := t.accent.Render(title) + "  " + t.muted.Render("Create → Prove → Lift")
	var tabs []string
	tabX := 0
	for i, name := range suiteConsoleTabs {
		label := " " + name + " "
		if i == m.tab {
			label = t.selected.Render(label)
		} else {
			label = t.muted.Render(label)
		}
		tabs = append(tabs, label)
		m.mouseRegion(suiteMouseRegion{tabX, 1, lipgloss.Width(label), 1, "tab", i})
		tabX += lipgloss.Width(label) + 1
	}
	tabLine := strings.Join(tabs, " ")
	if lipgloss.Width(tabLine) > w {
		if m.mouseRegions != nil {
			*m.mouseRegions = nil
		}
		tabLine = t.selected.Render(" "+suiteConsoleTabs[m.tab]+" ") + t.muted.Render("  Tab changes view · ←/→ local/remote")
	}
	progressHeight := 5
	left, bodyHeight, _ := m.paneGeometry()
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.liveColumnView(0, left, bodyHeight, t), m.liveColumnView(1, w-left, bodyHeight, t))
	activity := []string{t.muted.Render("No operation running · choose an action to begin")}
	if len(m.progress) > 0 {
		activity = nil
		for _, p := range m.progress[max(0, len(m.progress)-2):] {
			style := t.text
			icon := "○"
			switch p.State {
			case "pass", "ready", "exported", "published", "created", "reused", "answered":
				style = t.success
				icon = "✓"
			case "running", "judging", "reading":
				style = t.warning
				icon = "●"
			case "fail", "unknown":
				style = t.warning
				icon = "!"
			}
			activity = append(activity, style.Render(tuiOneLine(icon+" "+p.At.Format("15:04:05")+"  "+p.Stage+" · "+p.Object+" · "+p.State)))
		}
	}
	if m.running {
		activity = append([]string{t.warning.Render("● WORKING · Esc cancels; completed resources may remain")}, activity...)
	}
	status := m.status
	if m.discoveryError != "" {
		status = m.discoveryError
	}
	if m.refreshing {
		status = "Refreshing live inventory · " + status
	}
	content := header + "\n" + tabLine + "\n" + body + "\n" + suitePane("OPERATION PROGRESS", activity, w, progressHeight, false, t) + "\n" + t.muted.Render(ansi.Truncate(tuiOneLine(status), w, "…")) + "\n" + ansi.Truncate(suiteShortcutHints("←/→ local/remote · ↑↓ select · Tab views · PgUp/Dn scroll · Enter actions · t contexts · ? help", t), w, "…")
	var overlayRegions []suiteMouseRegion
	overlay := *m
	overlay.mouseRegions = &overlayRegions
	if panel := overlay.suiteOverlay(t); panel != "" {
		pw, ph := lipgloss.Width(panel), lipgloss.Height(panel)
		if m.mouseRegions != nil {
			*m.mouseRegions = []suiteMouseRegion{{0, 0, w, h, "block", 0}}
		}
		for _, r := range overlayRegions {
			r.x += (w - pw) / 2
			r.y += (h - ph) / 2
			m.mouseRegion(r)
		}
		content = lipgloss.NewCompositor(lipgloss.NewLayer(content), lipgloss.NewLayer(panel).X((w-pw)/2).Y((h-ph)/2).Z(1)).Render()
	}
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], w, "")
	}
	lines = lines[:min(h, len(lines))]
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	if m.review != nil {
		v.MouseMode = tea.MouseModeNone
	}
	return v
}

func (m *suiteConsole) suiteOverlay(t suiteConsoleTheme) string {
	w, h, page := m.reviewGeometry()
	var title string
	var rows []string
	rowHits := map[int]suiteMouseRegion{}
	mark := func(action string, index int) { rowHits[len(rows)] = suiteMouseRegion{action: action, index: index} }
	switch {
	case m.running:
		title = "WORKING"
		rows = append(rows, reviewIndented(t.field.Render(safeTerminal(m.loadingLabel)), w-4, 0)...)
		rows = append(rows, "", t.accent.Render(consoleLoadingBar(m.loadingFrame, w-8)), "")
		for _, event := range m.progress[max(0, len(m.progress)-5):] {
			rows = append(rows, reviewIndented(t.text.Render(safeTerminal(event.Stage+" · "+event.Object+" · "+event.State)), w-4, 2)...)
		}
		if m.cancelling {
			rows = append(rows, t.warning.Render("Cancelling; completed resources may remain…"))
		} else {
			rows = append(rows, suiteShortcutHints("Esc cancel operation", t))
		}
	case m.feedback != nil:
		title = m.feedback.title
		all := strings.Split(ansi.Hardwrap(safeTerminal(m.feedback.text), w-4, true), "\n")
		start := min(m.feedback.scroll, max(0, len(all)-page))
		rows = append(rows, all[start:min(len(all), start+page)]...)
		rows = append(rows, suiteShortcutHints("↑↓ scroll · Enter close · Esc close", t))
	case m.liftGuide != nil && m.review == nil && m.plan == nil:
		g := m.liftGuide
		title = "LIFT · " + safeTerminal(g.title)
		if g.env.Name != "" {
			rows = append(rows, t.property("Target", g.env.Name+" / "+g.namespace))
		}
		if g.model != "" {
			rows = append(rows, t.property("Inference", g.model+" · "+g.endpoint))
		}
		if g.version.Reference != "" {
			rows = append(rows, t.property("Artifact", g.version.Reference))
		}
		rows = append(rows, "")
		warning := g.stage == "runtime" || g.stage == "runtime-alert" || g.stage == "error" || g.stage == "proof-refused" || g.stage == "coexistence"
		if warning && g.note != "" {
			rows = append(rows, reviewIndented(t.danger.Render("Warning: "+safeTerminal(g.note)), w-4, 0)...)
			rows = append(rows, "")
		}
		if len(g.choices) > 0 {
			rows = append(rows, "Search: "+safeTerminal(g.query)+"▏", "")
			matches := g.matches()
			start := max(0, g.selection-1)
			for i := start; i < min(len(matches), start+4); i++ {
				c := g.choices[matches[i]]
				line := suiteChoiceWrapped(c.label, w-4, i == g.selection, t)
				mark("guide", i)
				rows = append(rows, line)
				if c.detail != "" {
					rows = append(rows, reviewIndented(t.muted.Render(safeTerminal(c.detail)), w-4, 4)...)
				}
			}
			if len(matches) == 0 {
				rows = append(rows, "No matching choices")
			}
		} else {
			rows = append(rows, reviewIndented(t.field.Render(safeTerminal(g.title)), w-4, 0)...)
			if g.confirm != "" {
				rows = append(rows, t.property("Type to confirm", g.confirm))
			}
			rows = append(rows, safeTerminal(ansi.Strip(m.input.View())))
			mark("guide-next", 0)
			rows = append(rows, t.shortcut.Render("[ Continue ]"))
		}
		if !warning {
			rows = append(rows, "", safeTerminal(g.note))
		}
		rows = append(rows, safeTerminal(m.status), suiteShortcutHints("↑↓ select · Enter continue · Alt+← change destination · Esc cancel", t))
	case m.sortOpen:
		title = "SORT AGENTS / SUITES"
		for i, label := range suiteListSorts {
			line := "  " + label
			if i == m.sortSelection {
				line = t.selected.Render("› " + label)
			}
			mark("sort-choice", i)
			rows = append(rows, line)
		}
		rows = append(rows, "", suiteShortcutHints("↑↓ select · Enter apply · Esc cancel", t))
	case m.publications != nil:
		p := m.publications
		title = p.heading()
		rows = append(rows, "Search: "+safeTerminal(p.query)+"▏", "")
		if len(p.versions) == 0 {
			rows = append(rows, "No published versions known. Publish first or enter an external digest.", "")
		}
		items, matches := p.items(), p.matches()
		start := max(0, p.selection-1)
		for i := start; i < min(len(matches), start+3); i++ {
			item := items[matches[i]]
			label := "  " + safeTerminal(item.name)
			if i == p.selection {
				label = t.selected.Render("› " + safeTerminal(item.name))
			}
			mark("publication", i)
			rows = append(rows, label, safeTerminal(item.detail))
		}
		if len(matches) == 0 {
			rows = append(rows, "No matching versions")
		}
		rows = append(rows, "", suiteShortcutHints("↑↓ select · Enter choose · Esc cancel", t))
	case m.contexts != nil:
		p := m.contexts
		title = "LOCAL CONTEXT"
		if p.side == 1 {
			title = "REMOTE CONTEXT"
		}
		if p.aks {
			title = "SELECT AKS CLUSTER"
		}
		rows = append(rows, "Search: "+safeTerminal(p.query)+"▏", "")
		matches := p.matches()
		if len(matches) == 0 {
			rows = append(rows, "No matching contexts or clusters")
		}
		start := max(0, p.selection-2)
		for i := start; i < min(len(matches), start+5); i++ {
			c := p.choices[matches[i]]
			line := "  " + safeTerminal(c.label)
			if i == p.selection {
				line = t.selected.Render("› " + safeTerminal(c.label))
			}
			mark("context-choice", i)
			rows = append(rows, line, safeTerminal(c.detail))
		}
		rows = append(rows, "", suiteShortcutHints("↑↓ select · Enter connect · Esc back", t))
		if p.side == 1 && !p.aks {
			mark("aks", 0)
			rows = append(rows, suiteShortcutHints("n discover AKS clusters", t))
		}
	case m.creation != nil:
		title = "CREATE AGENT"
		s := m.creation.step()
		rows = append(rows, safeTerminal(s.title), "")
		if s.kind == "confirm" {
			execution := m.creation.profile
			if execution == "native" {
				execution = "Orka AgentSuite (default)"
			}
			rows = append(rows, t.property("Agent", m.creation.name), t.property("Save to", m.creation.root), t.property("Execution", execution))
			if m.creation.profile != "image-http" {
				rows = append(rows, t.property("Model", m.creation.model))
				if m.creation.foundryEndpoint != "" {
					rows = append(rows, t.property("Foundry endpoint", m.creation.foundryEndpoint))
					if m.creation.setup != nil {
						rows = append(rows, t.property("Context", m.creation.setup.Environment.Name), t.property("Provider", m.creation.setup.Provider), t.property("Secret", m.creation.setup.Secret+" / api-key"))
					}
					rows = append(rows, t.property("Connection", m.creation.connection))
				} else {
					rows = append(rows, safeTerminal(m.creation.connection))
				}
			} else {
				rows = append(rows, "Inference: capabilities 8192 context / 1024 output tokens.", "Concrete model and connection selected with environment bindings at evaluation.")
			}
		}
		if len(s.choices) > 0 {
			matches := s.matches()
			if s.searchable() {
				rows = append(rows, "Search: "+safeTerminal(s.query)+"▏", "Type to fuzzy-search names or details · Backspace edits search", "")
			}
			start := max(0, s.selection-2)
			for i := start; i < min(len(matches), start+4); i++ {
				line := suiteChoiceWrapped(s.choices[matches[i]].label, w-4, i == s.selection, t)
				mark("creation", i)
				rows = append(rows, line)
			}
			if len(matches) > 0 {
				rows = append(rows, "", safeTerminal(s.choices[matches[min(s.selection, len(matches)-1)]].detail))
			} else {
				rows = append(rows, "No matching choices")
			}
		} else {
			rows = append(rows, safeTerminal(ansi.Strip(m.input.View())))
		}
		rows = append(rows, "", t.shortcut.Render("↑↓")+" choose · "+t.shortcut.Render("Enter")+" next · "+t.shortcut.Render("Backspace")+" back", t.shortcut.Render("Alt+←")+" back from text · "+t.shortcut.Render("Esc")+" cancel", safeTerminal(m.status))
	case m.help:
		title = "KEYS & COMMANDS"
		rows = []string{"←/→        focus LOCAL / REMOTE", "↑↓ j/k      select suites/members on the focused side", "Tab / [ ]   switch Source, Members, Cases, Artifacts, Destinations, Activity", "PgUp/PgDn   scroll details · Home/End first/last", "Enter       context-aware action menu · i inspect source/cases", "o open · n create · t context selection · x extract new source", "e prove · p publish · l lift · s status · u run · c chat", "v validate runtime · I separate confirmed Orka installation", "d edit authored files · R inspect member runs · r refresh", "/ command palette · Tab completes · Enter selects/submits", "Read-only live refresh every 5s; paused during operations/reviews", "Esc back/cancel · Ctrl+C quit (waits for active cancellation)", "", "Ready means placement. Extraction preserves the original.", "Esc / Enter close help"}
		for i := 0; i < 10; i++ {
			rows[i] = suiteShortcutHints(rows[i], t)
		}
		rows = append(rows, suiteShortcutHints("Backspace previous creation step (text: at start) · Alt+← previous step", t))
		rows = append(rows, "Mouse: click tabs, rows and buttons; wheel scrolls lists/details.", "Shift+drag (terminal dependent) selects text instead of sending mouse events.")
		rows = append(rows, suiteShortcutHints("f fuzzy-find agents in focused pane · S select list sort order", t), "Search: Enter keeps filter; Esc clears it. Counts show matching / total.")
		rows = append(rows, suiteShortcutHints("w switch kind inventory / local source files", t))
	case m.actionsOpen:
		title = "ACTIONS · " + tuiOneLine(m.entry().Name)
		for i, a := range m.suiteActions() {
			line := "  " + t.shortcut.Render(a.key) + "  " + a.label
			if i == m.actionSelection {
				line = t.actionRow(a.key, a.label, w-4)
			}
			mark("action", i)
			rows = append(rows, line)
		}
		rows = append(rows, "", suiteShortcutHints("↑↓ choose · Enter open · Esc back", t))
	case m.form != nil:
		title = strings.ToUpper(m.form.action)
		if m.form.action == "command" {
			title = "COMMAND PALETTE"
			rows = append(rows, "Command: open / create / suite show / lift …")
		} else {
			rows = append(rows, fmt.Sprintf("STEP %d / %d", m.form.field+1, len(m.form.labels)))
			for i := max(0, m.form.field-2); i < m.form.field; i++ {
				value := m.form.values[i]
				if m.form.action == "eval" && i >= 2 {
					value = "configured (private)"
				}
				rows = append(rows, t.muted.Render(tuiOneLine(fmt.Sprintf("✓ %s: %s", m.form.labels[i], value))))
			}
		}
		rows = append(rows, "", safeTerminal(m.form.labels[m.form.field]), "")
		// Textinput owns its cursor styling; its contents are still terminal-sanitized.
		rows = append(rows, t.selected.Width(w-4).Render("  "+safeTerminal(ansi.Strip(m.input.View()))), "")
		choices := m.formCompletions()
		if m.resourceSelectionForm() {
			rows = append(rows, "Type to fuzzy-search · Enter selects highlighted result")
			if len(choices) == 0 {
				rows = append(rows, "No matching choices")
			}
		}
		begin := max(0, m.completionSelection-2)
		for i := begin; i < min(len(choices), begin+4); i++ {
			label := choices[i]
			if detail := m.resourceChoiceDetail(choices[i]); detail != "" {
				label += " · " + detail
			}
			line := "  " + safeTerminal(label)
			if i == m.completionSelection {
				line = t.accent.Render("› " + safeTerminal(label))
			}
			mark("completion", i)
			rows = append(rows, line)
		}
		rows = append(rows, suiteShortcutHints("↑↓ suggestions · Tab complete · Enter next/confirm · Esc cancel", t), tuiOneLine(m.status))
	case m.extraction != nil || m.plan != nil || m.review != nil:
		var all []string
		title, all = m.reviewText()
		start := min(m.scroll, max(0, len(all)-page))
		rows = append(rows, all[start:min(len(all), start+page)]...)
		rows = append(rows, fmt.Sprintf("Lines %d–%d/%d", start+1, min(len(all), start+page), len(all)), suiteShortcutHints("↑↓ scroll · Enter next/confirm · Esc cancel", t))
		if m.review != nil {
			rows = append(rows, "Drag to select text; use terminal Copy. Keyboard navigates review.")
		}
	default:
		return ""
	}
	if !m.help && m.review == nil && !m.running {
		if m.form != nil || m.review != nil || m.plan != nil || m.extraction != nil || m.creation != nil && len(m.creation.step().choices) == 0 {
			mark("next", 0)
			rows = append(rows, t.shortcut.Render("[ Continue ]"))
		}
		if m.creation != nil || m.contexts != nil {
			mark("back", 0)
			rows = append(rows, t.shortcut.Render("[ Back ]"))
		}
		mark("cancel", 0)
		rows = append(rows, t.shortcut.Render("[ Cancel / Close ]"))
	}
	// Styled menu rows are trusted; dynamic values were sanitized above. Wrap
	// plain guidance before constructing an opaque, framed modal.
	var wrapped []string
	var hits []suiteMouseRegion
	for i, row := range rows {
		lines := strings.Split(ansi.Hardwrap(row, w-4, true), "\n")
		if r, ok := rowHits[i]; ok {
			r.x = 2
			r.y = 2 + len(wrapped)
			r.width = w - 4
			r.height = len(lines)
			hits = append(hits, r)
		}
		wrapped = append(wrapped, lines...)
	}
	if m.help {
		start := min(m.scroll, max(0, len(wrapped)-(h-4)))
		wrapped = append(wrapped[start:min(len(wrapped), start+h-4)], "↑↓ scroll · Enter / Esc close")
	}
	if m.review == nil && m.plan == nil && m.extraction == nil {
		h = min(h, max(8, len(wrapped)+3))
	}
	panel := suitePane(title, wrapped, w, h, true, t)
	for _, r := range hits {
		r.height = min(r.height, h-1-r.y)
		if r.height > 0 {
			m.mouseRegion(r)
		}
	}
	if os.Getenv("NO_COLOR") == "" {
		panel = lipgloss.NewStyle().Background(lipgloss.Color("#18232D")).Render(panel)
	}
	return panel
}

func suiteChoiceWrapped(label string, width int, selected bool, t suiteConsoleTheme) string {
	lines := strings.Split(ansi.Hardwrap(safeTerminal(label), max(1, width-2), true), "\n")
	for i := range lines {
		prefix := "  "
		if selected && i == 0 {
			prefix = "› "
		}
		lines[i] = prefix + lines[i]
		if selected {
			lines[i] = t.selected.Render(lines[i])
		}
	}
	return strings.Join(lines, "\n")
}
