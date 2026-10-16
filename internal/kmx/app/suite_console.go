package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	suiteoras "github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite/oras"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite/workspace"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/runview"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/scaffold"
)

type suiteConsoleEntry struct {
	ObservedReady                                               bool
	Inference                                                   string
	Modified                                                    time.Time
	Published                                                   *suitePublicationChoice
	Copilot                                                     *suiteCopilotBinding
	Name, Origin, Root, Deployment, Evidence, Reference, Layout string
	Review                                                      *suiteWorkspaceReview
	Live                                                        *agentTUIAgent
	Context                                                     string
	Reviewed                                                    bool
	Side                                                        int
	Environment                                                 agentTUIEnvironment
	Observation                                                 string
	Image                                                       bool
	EnvironmentFile                                             string
}
type suiteConsoleForm struct {
	copilot        *suiteCopilotBinding
	execution      string
	action         string
	labels, values []string
	field          int
	destination    *config.Destination
	server         string
}
type suiteConsoleResult struct {
	pipelineDone        bool
	feedback            *suiteFeedback
	guide               *suiteLiftGuide
	guideDone           bool
	inferenceConfigured bool
	inferenceSetup      *suiteInferenceSetup
	contexts            *suiteContextPicker
	connected           *agentTUIEnvironment
	createStep          *suiteCreateStep
	op                  int
	entry               *suiteConsoleEntry
	entries             []suiteConsoleEntry
	extract             *PreparedSuiteExtraction
	plan                *PreparedReleaseLift
	result              ReleaseLiftResult
	text                string
	err                 error
	columns             *[2]suiteConsoleColumn
	azure               []suiteAzureSelection
}
type suiteConsoleTick struct{}
type suiteConsoleBootTick struct{}
type suiteConsoleProgress struct {
	op    int
	event SuiteProgress
}

func suiteConsoleRowsView(rows []string, width, height int, footer string) tea.View {
	width = max(1, width)
	height = max(1, height)
	var out []string
	for _, row := range rows {
		for _, line := range strings.Split(row, "\n") {
			out = append(out, ansi.Truncate(safeTerminal(line), width, "…"))
		}
	}
	if len(out) > height {
		if height > 2 {
			out = append(out[:height-2], ansi.Truncate(safeTerminal(footer), width, "…"), ansi.Truncate("? help · Esc back/cancel", width, "…"))
		} else {
			out = out[:height]
		}
	}
	return tea.NewView(newSuiteConsoleTheme().text.Render(strings.Join(out, "\n")))
}

type suiteConsole struct {
	pipelineRequests                                                      chan suitePipelineConsent
	pipelineConsent                                                       *suitePipelineConsent
	feedback                                                              *suiteFeedback
	loadingLabel                                                          string
	operationAction                                                       string
	loadingFrame                                                          int
	bootLoading                                                           bool
	bootFrame                                                             int
	liftGuide                                                             *suiteLiftGuide
	guideRetry                                                            func(context.Context, *App, *suiteLiftGuide) error
	liftAfterPublish                                                      string
	authoringView                                                         bool
	listQuery                                                             [2]string
	listSort                                                              [2]int
	listSearching, sortOpen                                               bool
	sortSelection                                                         int
	publications                                                          *suitePublicationPicker
	standaloneNamespace                                                   string
	standaloneEnvironment                                                 agentTUIEnvironment
	mouseRegions                                                          *[]suiteMouseRegion
	contexts                                                              *suiteContextPicker
	pendingAKS                                                            suiteAzureCluster
	a                                                                     *App
	opt                                                                   AgentTUIOptions
	ctx                                                                   context.Context
	workers                                                               *sync.WaitGroup
	entries                                                               []suiteConsoleEntry
	selected, width, height, scroll, page, op                             int
	tab                                                                   int
	focus, detailScroll, actionSelection, completionSelection             int
	actionsOpen                                                           bool
	columns                                                               [2]suiteConsoleColumn
	columnSelection                                                       [2]int
	refreshing                                                            bool
	refreshGeneration                                                     int
	refreshCancel                                                         context.CancelFunc
	discoveryError                                                        string
	input                                                                 textinput.Model
	form                                                                  *suiteConsoleForm
	creation                                                              *suiteCreateWizard
	hostChat                                                              *suiteConsoleEntry
	review                                                                *suiteWorkspaceReview
	extraction                                                            *PreparedSuiteExtraction
	plan                                                                  *PreparedReleaseLift
	status, detail, chatID, chatMember, standaloneName, standaloneContext string
	running, cancelling, help                                             bool
	cancel                                                                context.CancelFunc
	events                                                                chan suiteConsoleProgress
	progress                                                              []SuiteProgress
	progressRoot                                                          string
	publication                                                           suiteConsoleForm
	runs                                                                  *agentTUIModel
	chatConfirm                                                           string
	azure                                                                 []suiteAzureSelection
}

// AgentTUI presents suites as the lifecycle object while independent agents stay
// available as explicitly unowned, one-member views. All effects use shared ops.
func (a *App) AgentTUI(opt AgentTUIOptions) error {
	if a.Stdin == nil || !isInteractiveTerminal(a.Stdin) || !isInteractiveTerminal(a.Out) || os.Getenv("TERM") == "dumb" {
		return errors.New("kmx console requires an interactive terminal; use suite show or orka list for reports")
	}
	if opt.Namespace == "" {
		opt.Namespace = OrkaNamespace
	}
	if err := scaffold.ValidateNamespace(opt.Namespace); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(a.operationContext())
	defer cancel()
	var workers sync.WaitGroup
	in := textinput.New()
	in.Prompt = "> "
	in.CharLimit = 4096
	m := suiteConsole{a: a, opt: opt, ctx: ctx, workers: &workers, input: in, width: 100, height: 32, status: "Open or create a suite · ? help", events: make(chan suiteConsoleProgress, 256)}
	m.authoringView = opt.Workspace != ""
	m.bootLoading = true
	for {
		out, err := tea.NewProgram(&m, tea.WithInput(a.Stdin), tea.WithOutput(a.Out), tea.WithContext(ctx), tea.WithFilter(func(_ tea.Model, msg tea.Msg) tea.Msg {
			if _, ok := msg.(tea.InterruptMsg); ok {
				return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
			}
			return msg
		})).Run()
		if err != nil {
			cancel()
			workers.Wait()
			return err
		}
		m = *out.(*suiteConsole)
		if m.chatID == "" && m.standaloneName == "" && m.hostChat == nil {
			cancel()
			workers.Wait()
			return nil
		}
		if m.hostChat != nil {
			err = m.contextApp().runInteractiveChatBackend(&suiteCopilotChat{app: m.contextApp(), entry: *m.hostChat})
			m.hostChat = nil
		} else if m.chatID != "" {
			w := *m.contextApp()
			cfg := *a.Cfg
			cfg.Confirm = m.chatConfirm
			w.Cfg = &cfg
			err = w.ChatSuiteMember(m.chatID, m.chatMember, "")
		} else {
			w := m.contextApp().releaseLiftWorker(ctx, ReleaseLiftOptions{ToContext: m.standaloneContext})
			if m.standaloneEnvironment.Name != "" {
				w = m.standaloneEnvironment.app(a).withRunContext(ctx)
			}
			w.Cfg.Confirm = m.chatConfirm
			err = w.ChatWithOptions(ChatOptions{Agent: m.standaloneName, Namespace: valueOr(m.standaloneNamespace, opt.Namespace), Runtime: "orka"})
		}
		m.chatID, m.standaloneName = "", ""
		m.refreshing = false
		m.status = "Returned from chat"
		if err != nil {
			m.status = safeTerminal(err.Error())
			m.feedback = &suiteFeedback{title: "CHAT COULD NOT CONNECT", text: safeTerminal(err.Error()) + "\n\nThe agent was not extracted or redeployed. Check the selected context, inference and result access; no automatic retry was attempted."}
		}
	}
}

func (m *suiteConsole) Init() tea.Cmd {
	cmd := m.refreshLive()
	if m.bootLoading {
		return tea.Batch(cmd, tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return suiteConsoleBootTick{} }))
	}
	return cmd
}
func (m *suiteConsole) job(fn func(context.Context, *App) suiteConsoleResult) tea.Cmd {
	if m.refreshCancel != nil {
		m.refreshCancel()
	}
	m.refreshGeneration++
	m.refreshing = false
	m.progress = nil
	m.progressRoot = ""
	if e := m.entry(); e != nil {
		m.progressRoot = e.Root
	}
	m.op++
	op := m.op
	m.running = true
	m.loadingLabel = m.operationLabel()
	m.loadingFrame = 0
	m.cancelling = false
	ctx, stop := context.WithCancel(m.ctx)
	m.cancel = stop
	w := *m.contextApp().withRunContext(ctx)
	cfg := *w.Cfg
	w.Cfg = &cfg
	w.InvocationCommand = ""
	w.Out, w.Err = io.Discard, io.Discard
	runner := *w.Run
	runner.Stdout, runner.Stderr = io.Discard, io.Discard
	w.Run = &runner
	w.suiteEvents = func(e SuiteProgress) {
		select {
		case m.events <- suiteConsoleProgress{op, e}:
		default:
		}
	}
	w.operationProgress = func(stage, state string, _ error) { w.suiteProgress(stage, "", state) }
	m.workers.Add(1)
	result := make(chan suiteConsoleResult, 1)
	go func() { defer m.workers.Done(); r := fn(ctx, &w); r.op = op; result <- r }()
	return tea.Batch(func() tea.Msg { return <-result }, tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return suiteConsoleTick{} }))
}

func loadSuiteConsoleSources(ctx context.Context, a *App, opt AgentTUIOptions) suiteConsoleResult {
	rows := []suiteConsoleEntry{}
	hints, err := readSuiteCatalog()
	var catalogErr error
	if err != nil {
		catalogErr = err
		hints = nil
	}
	seen := map[string]bool{}
	if opt.Workspace != "" {
		r, err := a.reviewSuiteWorkspace(ctx, opt.Workspace)
		if err != nil {
			return suiteConsoleResult{err: err}
		}
		entry := suiteConsoleEntry{Name: r.name(), Origin: "workspace", Root: r.root, Review: r}
		for _, h := range hints {
			if h.Root == r.root {
				entry.Evidence, entry.Reference, entry.Layout = h.Evidence, h.Reference, h.Layout
				entry.EnvironmentFile = h.EnvironmentFile
				entry.Copilot = h.Copilot
			}
		}
		rows = append(rows, entry)
		seen[r.root] = true
	}
	unavailableSources := 0
	for _, h := range hints {
		if seen[h.Root] {
			continue
		}
		row, err := loadCatalogWorkspace(ctx, a, h)
		if err != nil {
			unavailableSources++
			continue
		}
		rows = append(rows, row)
	}
	if unavailableSources > 0 {
		catalogErr = errors.Join(catalogErr, fmt.Errorf("%d saved source folders unavailable; hidden from agent list (catalog hints retained)", unavailableSources))
	}
	dir, err := deploymentDirectory()
	if err != nil {
		return suiteConsoleResult{entries: rows, err: errors.Join(catalogErr, err)}
	}
	files, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return suiteConsoleResult{err: err}
	}
	if len(files) > 256 {
		return suiteConsoleResult{err: errors.New("deployment catalog exceeds 256 records")}
	}
	foundDeployment := opt.Deployment == ""
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(f.Name(), ".json")
		if opt.Deployment != "" && opt.Deployment != id {
			continue
		}
		d, err := ReadSuiteDeployment(id)
		if err != nil {
			catalogErr = errors.Join(catalogErr, fmt.Errorf("retained placement %s unavailable: %w", id, err))
			continue
		}
		foundDeployment = true
		rows = append(rows, suiteConsoleEntry{Name: d.Summary.Suite, Origin: "retained placement (not rechecked)", Deployment: id, Reference: d.Options.Reference, Layout: d.Options.FromLayout, Evidence: d.Options.Evidence, Context: d.Summary.Target.Context, Inference: d.Summary.Model})
		if info, err := f.Info(); err == nil {
			rows[len(rows)-1].Modified = info.ModTime()
		}
	}
	imageRows, imageErr := readImageConsoleEntries(opt.Deployment)
	if imageErr != nil {
		catalogErr = errors.Join(catalogErr, imageErr)
	}
	rows = append(rows, imageRows...)
	if len(imageRows) > 0 {
		foundDeployment = true
	}
	if !foundDeployment {
		return suiteConsoleResult{entries: rows, err: errors.New("selected deployment record not found")}
	}
	return suiteConsoleResult{entries: rows, err: catalogErr, text: "Catalog loaded; retained placements require live revalidation"}
}
func (m *suiteConsole) entry() *suiteConsoleEntry {
	if m.selected < 0 || m.selected >= len(m.entries) {
		return nil
	}
	if m.entries[m.selected].Side != m.focus {
		return nil
	}
	return &m.entries[m.selected]
}
func (m *suiteConsole) startForm(action string, labels, values []string) tea.Cmd {
	m.completionSelection = 0
	m.form = &suiteConsoleForm{action: action, labels: labels, values: values}
	m.input.SetValue(values[0])
	m.input.Focus()
	return textinput.Blink
}
func (m *suiteConsole) destinationHints(root string) string {
	d, err := config.LoadDestinations(root)
	if err != nil {
		return err.Error()
	}
	if d == nil {
		return "No kmx.yaml destinations; edit it with d"
	}
	var names []string
	for name := range d.Targets {
		names = append(names, name)
	}
	sort.Strings(names)
	var rows []string
	for _, name := range names {
		v := d.Targets[name]
		rows = append(rows, name+" = "+v.Context+" / "+v.Namespace+" / "+v.Inference)
	}
	return strings.Join(rows, " · ")
}
func (m *suiteConsole) showReview(r *suiteWorkspaceReview) { m.review = r; m.page = 0; m.scroll = 0 }

func (m *suiteConsole) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if mouse, ok := msg.(tea.MouseMsg); ok {
		return m.updateMouse(mouse)
	}
	switch v := msg.(type) {
	case suiteConsoleBootTick:
		if !m.bootLoading || !m.refreshing {
			return m, nil
		}
		m.bootFrame++
		return m, tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return suiteConsoleBootTick{} })
	case suiteConsoleRefreshTick:
		if m.form != nil || m.plan != nil || m.extraction != nil || m.review != nil || m.actionsOpen || m.runs != nil || m.running {
			return m, tea.Tick(5*time.Second, func(time.Time) tea.Msg { return suiteConsoleRefreshTick{} })
		}
		return m, m.refreshLive()
	case suiteConsoleRefresh:
		if v.generation != m.refreshGeneration {
			return m, nil
		}
		m.refreshing = false
		m.bootLoading = false
		if m.form == nil && m.plan == nil && m.extraction == nil && m.review == nil && m.runs == nil && m.actionsOpen == false {
			m.acceptRefresh(v.result)
		}
		return m, tea.Tick(5*time.Second, func(time.Time) tea.Msg { return suiteConsoleRefreshTick{} })
	}
	if m.runs != nil {
		if size, ok := msg.(tea.WindowSizeMsg); ok {
			m.width, m.height = size.Width, size.Height
		}
		if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "ctrl+c" {
			m.runs.runs.stop()
			return m, tea.Quit
		}
		next, cmd := m.runs.Update(msg)
		r := next.(agentTUIModel)
		m.runs = &r
		if r.runs == nil {
			m.runs = nil
		}
		return m, cmd
	}
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
		m.input.SetWidth(max(1, min(88, v.Width-6)-8))
		return m, nil
	case suiteConsoleTick:
		m.loadingFrame++
		select {
		case request := <-m.pipelineRequests:
			m.pipelineConsent = &request
			m.input.SetValue("")
			m.input.Focus()
		default:
		}
		for {
			select {
			case e := <-m.events:
				if e.op != m.op {
					continue
				}
				m.progress = append(m.progress, e.event)
				if len(m.progress) > 128 {
					m.progress = m.progress[len(m.progress)-128:]
				}
			default:
				if m.running {
					return m, tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return suiteConsoleTick{} })
				}
				return m, nil
			}
		}
	case suiteConsoleResult:
		if v.op != m.op {
			return m, nil
		}
		if v.feedback != nil && !m.cancelling {
			m.feedback = v.feedback
		}
		if v.guide != nil && !m.cancelling {
			m.liftGuide = v.guide
			m.input.SetValue(v.guide.value)
			m.input.CursorEnd()
			m.input.Focus()
			if v.guideDone {
				m.liftGuide = nil
				// Keep target access and source/evidence hints attached to the selected
				// entry while the common review/apply path owns the prepared plan.
				e := v.guide.entry
				e.Environment = v.guide.env
				e.Evidence = v.guide.version.Evidence
				e.Reference = v.guide.version.Reference
				e.EnvironmentFile = v.guide.version.Environment
				v.entry = &e
			}
		}
		var continueLift *suitePublicationChoice
		if m.liftAfterPublish != "" {
			if v.err != nil || m.cancelling {
				m.liftAfterPublish = ""
			} else if v.entry != nil && v.entry.Published != nil && v.entry.Root == m.liftAfterPublish {
				copy := *v.entry.Published
				continueLift = &copy
				m.liftAfterPublish = ""
			}
		}
		if v.contexts != nil && !m.cancelling {
			m.contexts = v.contexts
		}
		if v.createStep != nil && m.creation != nil && !m.cancelling {
			m.createStep(*v.createStep)
		}
		if v.inferenceSetup != nil && m.creation != nil && !m.cancelling {
			m.creation.setup = v.inferenceSetup
		}
		if v.inferenceConfigured && m.creation != nil && !m.cancelling {
			m.creation.connection = "Provider: " + m.creation.setup.Provider + " · Secret: " + m.creation.setup.Secret + " (configured; not evaluated)"
			m.createConfirmation()
		}
		if v.azure != nil {
			m.azure = v.azure
		}
		m.running = false
		if v.pipelineDone {
			m.liftGuide = nil
		}
		m.pipelineConsent = nil
		if m.cancel != nil {
			m.cancel()
			m.cancel = nil
		}
		m.status = v.text
		if v.entries != nil {
			m.entries = v.entries
			m.selected = min(m.selected, max(0, len(m.entries)-1))
		}
		if v.entry != nil {
			if v.entry.Deployment == "" && v.entry.Root != "" {
				m.authoringView = true
			}
			if v.entry.Deployment != "" && v.entry.Side == 0 {
				m.authoringView = false
			}
			found := -1
			for i, e := range m.entries {
				if v.entry.Deployment != "" && e.Deployment == v.entry.Deployment || v.entry.Deployment == "" && e.Deployment == "" && v.entry.Root != "" && e.Root == v.entry.Root {
					found = i
					break
				}
			}
			if err := rememberSuiteConsoleEntry(*v.entry); err != nil {
				m.status += " · catalog not saved: " + err.Error()
			}
			v.entry.Published = nil
			if found >= 0 {
				m.entries[found] = *v.entry
				m.selected = found
			} else {
				m.entries = append(m.entries, *v.entry)
				m.selected = len(m.entries) - 1
			}
			m.focus = v.entry.Side
			for pos, i := range m.sideIndices(m.focus) {
				if i == m.selected {
					m.columnSelection[m.focus] = pos
				}
			}
		}
		m.extraction, m.plan = v.extract, v.plan
		m.scroll = 0
		if v.err != nil {
			m.status = "Stopped: " + safeTerminal(v.err.Error())
		}
		if v.result.State != "" {
			m.detail = suiteWorkspaceLiftOutcomeSummary(v.result)
		}
		if v.text != "" {
			m.detail = v.text
			m.detailScroll = 0
		}
		if m.cancelling {
			m.status = "Cancelled; acknowledged resources and authored files may remain"
			m.extraction, m.plan = nil, nil
			m.liftGuide = nil
		}
		if v.connected != nil && v.err == nil && !m.cancelling {
			side := 1
			if v.connected.Local {
				side = 0
			}
			return m, m.selectContext(*v.connected, side)
		}
		if continueLift != nil {
			return m, m.publicationForm(*continueLift, false)
		}
		return m, tea.Tick(5*time.Second, func(time.Time) tea.Msg { return suiteConsoleRefreshTick{} })
	case tea.KeyPressMsg:
		key := v.String()
		if m.pipelineConsent != nil {
			return m.updatePipelineConsent(v)
		}
		if m.running {
			if key == "esc" || key == "ctrl+c" {
				m.cancelling = true
				m.cancel()
			}
			return m, nil
		}
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		if m.feedback != nil {
			switch key {
			case "esc", "enter":
				m.feedback = nil
			case "down", "j":
				m.feedback.scroll++
			case "up", "k":
				m.feedback.scroll = max(0, m.feedback.scroll-1)
			case "pgdown":
				m.feedback.scroll += max(1, m.height-10)
			case "pgup":
				m.feedback.scroll = max(0, m.feedback.scroll-max(1, m.height-10))
			}
			return m, nil
		}
		if m.contexts != nil {
			return m.updateContexts(v)
		}
		if m.publications != nil {
			return m.updatePublications(v)
		}
		if m.creation != nil {
			return m.updateCreation(v)
		}
		if m.liftGuide != nil && m.review == nil && m.plan == nil {
			return m.updateLiftGuide(v)
		}
		if m.sortOpen || m.listSearching {
			return m.updateListControls(v)
		}
		if m.help {
			switch key {
			case "down", "j":
				m.scroll++
			case "up", "k":
				m.scroll = max(0, m.scroll-1)
			case "pgdown":
				m.scroll += 5
			case "pgup":
				m.scroll = max(0, m.scroll-5)
			default:
				m.help = false
				m.scroll = 0
			}
			return m, nil
		}
		if m.actionsOpen {
			actions := m.suiteActions()
			switch key {
			case "esc":
				m.actionsOpen = false
			case "up", "k":
				m.actionSelection = max(0, m.actionSelection-1)
			case "down", "j":
				m.actionSelection = min(len(actions)-1, m.actionSelection+1)
			case "enter":
				if len(actions) > 0 {
					a := actions[min(m.actionSelection, len(actions)-1)]
					m.actionsOpen = false
					return m.Update(tea.KeyPressMsg{Code: []rune(a.key)[0]})
				}
			default:
				for _, a := range actions {
					if a.key == key {
						m.actionsOpen = false
						return m.Update(v)
					}
				}
			}
			return m, nil
		}
		if m.form != nil {
			choices := m.formCompletions()
			if len(choices) > 0 {
				switch key {
				case "up":
					m.completionSelection = max(0, m.completionSelection-1)
					return m, nil
				case "down":
					m.completionSelection = min(len(choices)-1, m.completionSelection+1)
					return m, nil
				case "tab":
					m.input.SetValue(choices[min(m.completionSelection, len(choices)-1)])
					m.input.CursorEnd()
					return m, nil
				}
			}
			if key == "esc" {
				m.form = nil
				m.liftAfterPublish = ""
				return m, nil
			}
			if key == "enter" {
				if m.resourceSelectionForm() {
					if len(choices) == 0 {
						m.status = "No matching choices; edit the search"
						return m, nil
					}
					m.input.SetValue(choices[min(m.completionSelection, len(choices)-1)])
				}
				if m.form.action == "command" && len(choices) > 0 && m.input.Value() != choices[min(m.completionSelection, len(choices)-1)] {
					m.input.SetValue(choices[min(m.completionSelection, len(choices)-1)])
					m.input.CursorEnd()
					return m, nil
				}
				if m.form.action == "eval" && m.form.field == 0 {
					e := m.entry()
					if e == nil {
						return m, nil
					}
					d, err := consoleDestination(e.Root, m.input.Value())
					if err != nil {
						m.status = err.Error()
						return m, nil
					}
					m.form.destination = &d
					server, err := m.a.consoleCreateTarget(m.ctx, agentTUIEnvironment{Name: d.Context})
					if err != nil {
						m.status = "Cannot resolve evaluation server"
						return m, nil
					}
					m.form.server = server
					m.status = "Evaluation target: " + d.Context + " / " + d.Namespace + " · " + d.Inference
					m.form.labels[1] = "Type context " + d.Context + " to authorize all cases and placement at " + server
				}
				m.form.values[m.form.field] = m.input.Value()
				m.form.field++
				if m.form.field < len(m.form.labels) {
					m.input.SetValue(m.form.values[m.form.field])
					m.completionSelection = 0
					return m, nil
				}
				f := *m.form
				m.form = nil
				return m, m.submit(f)
			}
			var c tea.Cmd
			m.completionSelection = 0
			m.input, c = m.input.Update(msg)
			return m, c
		}
		if m.extraction != nil {
			if m.scrollReview(key) {
				return m, nil
			}
			if key == "esc" {
				m.extraction = nil
				return m, nil
			}
			if key == "enter" {
				if m.advanceReview() {
					return m, nil
				}
				p := m.extraction
				m.extraction = nil
				return m, m.job(func(ctx context.Context, w *App) suiteConsoleResult {
					w = w.releaseLiftWorker(ctx, ReleaseLiftOptions{ToContext: p.Summary.Context})
					if err := w.ExtractSuite(ctx, p); err != nil {
						return suiteConsoleResult{err: err}
					}
					r, err := w.reviewSuiteWorkspace(ctx, p.options.Workspace)
					if err != nil {
						return suiteConsoleResult{err: err}
					}
					return suiteConsoleResult{entry: &suiteConsoleEntry{Name: r.name(), Origin: "extracted workspace", Root: r.root, Review: r}, text: "Original unchanged. Review/edit example cases before Prove."}
				})
			}
			return m, nil
		}
		if m.plan != nil {
			if m.scrollReview(key) {
				return m, nil
			}
			if key == "esc" {
				m.plan = nil
				return m, nil
			}
			if key == "enter" {
				if m.advanceReview() {
					return m, nil
				}
				return m, m.startForm("apply", []string{"Type destination context to authorize reviewed Apply/Verify"}, []string{""})
			}
			return m, nil
		}
		if m.review != nil {
			if m.scrollReview(key) {
				return m, nil
			}
			switch key {
			case "esc":
				m.review = nil
				if m.liftGuide != nil {
					m.liftGuide.evidenceChoices()
				}
			case "down", "j":
				m.scroll++
			case "up", "k":
				m.scroll = max(0, m.scroll-1)
			case "enter", "right":
				if m.advanceReview() {
					return m, nil
				}
				m.page++
				m.scroll = 0
				if m.page > len(m.review.inventory) {
					if e := m.entry(); e != nil {
						e.Reviewed = true
					}
					m.review = nil
					if m.liftGuide != nil && m.liftGuide.stage == "review-pipeline" {
						m.liftGuide.entry.Reviewed = true
						return m, m.startNativePipeline()
					}
					if m.liftGuide != nil && m.liftGuide.stage == "review-publish" {
						g := m.liftGuide
						g.entry.Reviewed = true
						m.guideInput("publish", "Publish reviewed source to "+g.registry+"/"+g.repository+"? Type publish", "", "publish")
					}
					if m.liftGuide != nil && m.liftGuide.stage == "review-eval" {
						g := m.liftGuide
						if g.needsJudge() {
							m.guideInput("judge-endpoint", "Judge endpoint required by authored cases (HTTPS; keyless loopback allowed)", "", "")
						} else {
							m.guideInput("evaluate", "Execute all reviewed cases on "+g.env.Name+"? Type context to authorize placement and model calls", "", g.env.Name)
						}
					}
				}
			case "left":
				m.page = max(0, m.page-1)
				m.scroll = 0
			}
			return m, nil
		}
		e := m.entry()
		switch key {
		case "q", "esc":
			return m, tea.Quit
		case "?":
			m.help = true
			m.scroll = 0
		case "tab", "]":
			m.tab = (m.tab + 1) % 6
			m.detailScroll = 0
		case "shift+tab", "[":
			m.tab = (m.tab + 5) % 6
			m.detailScroll = 0
		case "right":
			m.selectSide(1)
		case "left", "h":
			m.selectSide(0)
		case "/":
			return m, m.startForm("command", []string{"Command: open, create, extract, show, eval, push, lift, status, run, chat, edit, refresh, help, exit"}, []string{""})
		case "j", "down":
			m.moveSide(1)
		case "k", "up":
			m.moveSide(-1)
		case "pgdown":
			m.detailScroll = min(m.maxDetailScroll(), m.detailScroll+max(1, m.height/2))
		case "pgup":
			m.detailScroll = max(0, m.detailScroll-max(1, m.height/2))
		case "home", "g":
			m.columnSelection[m.focus] = 0
			m.selectSide(m.focus)
		case "end", "G":
			m.columnSelection[m.focus] = max(0, len(m.sideIndices(m.focus))-1)
			m.selectSide(m.focus)
		case "r":
			return m, m.refreshLive()
		case "o":
			return m, m.startForm("open", []string{"Workspace root"}, []string{m.opt.Workspace})
		case "w":
			m.authoringView = !m.authoringView
			m.columnSelection[0] = 0
			m.selectSide(0)
			return m, nil
		case "n":
			return m, m.startCreation()
		case "a":
			if len(m.azure) > 0 {
				return m, m.startForm("azure-select", []string{"AKS context from discovered endpoint matches", "ACR login server to select"}, []string{m.columns[1].Env.Name, ""})
			}
			return m, m.jobNamed("Discovering AKS clusters and ACR access", func(ctx context.Context, w *App) suiteConsoleResult {
				rows, err := w.discoverSuiteAKS(ctx)
				if err == nil {
					for i := range rows {
						rows[i], err = w.connectedSuiteACRs(ctx, rows[i])
						if err != nil {
							break
						}
					}
				}
				return suiteConsoleResult{azure: rows, text: suiteAzureDescription(rows) + "\nPress a to select context and registry.", err: err}
			})
		case "t":
			return m, m.openContexts(m.focus)
		case "f":
			m.listSearching = true
			return m, nil
		case "S":
			m.sortOpen = true
			m.sortSelection = m.listSort[m.focus]
			return m, nil
		case "v":
			if e != nil && e.Reference != "" {
				return m, m.startForm("runtime-check", []string{"Exact suite reference@digest", "OCI layout (blank for registry)", "Destination context", "Namespace"}, []string{e.Reference, e.Layout, m.columns[m.focus].Env.Name, m.opt.Namespace})
			}
			m.status = "Publish/select an exact suite artifact before runtime validation"
		case "I":
			return m, m.startForm("runtime-install", []string{"Context for separate Orka setup", "Type context to authorize chart/CRD/controller and result-access installation"}, []string{m.columns[m.focus].Env.Name, ""})
		case "x":
			if e != nil && e.Deployment == "" && e.Live != nil && !e.Live.SuiteOwned && !e.Live.External {
				parent, err := os.Getwd()
				if err != nil {
					m.status = err.Error()
					return m, nil
				}
				path, name, err := suggestExtractionWorkspace(parent, e.Live.Name)
				if err != nil {
					m.status = err.Error()
					return m, nil
				}
				m.status = "Suggested folder is available (not reserved). Save to: " + path
				return m, m.startForm("extract", []string{"Save new agent files to (available folder; editable)", "New member name"}, []string{path, name})
			}
			m.status = "Extraction requires an independent native Orka Agent"
		case "enter":
			if m.sideTotal(m.focus) == 0 {
				return m, m.startCreation()
			}
			if e != nil {
				m.actionsOpen = true
				m.actionSelection = 0
			}
		case "i":
			if e != nil && e.Review != nil {
				m.showReview(e.Review)
			} else if e != nil && e.Live != nil {
				entry := *e
				return m, m.jobNamed("Inspecting live agent · "+e.Live.Name, func(ctx context.Context, a *App) suiteConsoleResult {
					text, err := a.inspectConsoleAgent(ctx, entry)
					if err != nil {
						return suiteConsoleResult{err: err, feedback: &suiteFeedback{title: "INSPECTION UNAVAILABLE", text: err.Error()}}
					}
					return suiteConsoleResult{feedback: &suiteFeedback{title: "AGENT INSPECTION", text: text}}
				})
			} else if e != nil {
				m.feedback = &suiteFeedback{title: "PLACEMENT INSPECTION", text: fmt.Sprintf("%s\n%s\nContext: %s\nDeployment: %s\nArtifact: %s\nUse status to revalidate the placement.", e.Name, e.Origin, e.Context, e.Deployment, e.Reference)}
			}
		case "e":
			if e != nil && e.Review != nil {
				if !e.Reviewed {
					m.showReview(e.Review)
					m.status = "Review complete source and every case before Prove"
					return m, nil
				}
				if e.Review.image != nil {
					return m, m.startForm("image-eval", []string{"Published exact image release reference", "Environment bindings file", "Type evaluation context to authorize workload placement and all cases"}, []string{e.Reference, e.EnvironmentFile, ""})
				}
				d, _ := config.LoadDestinations(e.Root)
				target := ""
				if d != nil {
					target = d.EvalTarget
				}
				m.status = m.destinationHints(e.Root)
				return m, m.startForm("eval", []string{"Named evaluation destination (kmx.yaml)", "Type its context to authorize eval placement and all cases", "Judge base URL (blank for text-only)", "Judge credential environment name (optional)"}, []string{target, "", "", ""})
			}
		case "p":
			if e != nil && e.Root != "" {
				if e.Review != nil && e.Review.image != nil {
					return m, m.startForm("image-push", []string{"ACR/registry repository:tag", "Buildx builder", "Type publish to build and upload authored source/images"}, []string{m.opt.Suite.Registry, m.opt.Suite.Build.Builder, ""})
				}
				return m, m.startForm("push", []string{"Registry reference or local tag", "Local OCI directory (blank for registry)", "Evidence file (blank selects latest completed proof)", "Plain HTTP registry? type yes"}, []string{"", "", e.Evidence, ""})
			}
		case "l":
			if e != nil {
				return m, m.openPublications(*e)
			}
		case "s":
			if e != nil && e.Deployment != "" {
				id := e.Deployment
				return m, m.job(func(ctx context.Context, w *App) suiteConsoleResult {
					if len(id) == 64 {
						rows, err := w.ImagePlacementStatus(ctx, id)
						raw, _ := json.MarshalIndent(rows, "", "  ")
						return suiteConsoleResult{text: string(raw), err: err}
					}
					s, err := w.SuiteDeploymentStatus(ctx, id)
					raw, _ := json.MarshalIndent(s, "", "  ")
					return suiteConsoleResult{text: string(raw), err: err}
				})
			}
		case "c":
			if e != nil && e.Live != nil && !e.Live.SuiteOwned && !e.Live.External {
				return m, m.startForm("independent-chat", []string{"Type context to chat with existing agent " + e.Live.Name + ": " + e.Context}, []string{""})
			}
			if e != nil && e.Copilot != nil && e.Deployment == "" {
				return m, m.startForm("copilot-chat", []string{"Type copilot to send authored instructions and chat turns through your local CLI/login"}, []string{""})
			}
			if e != nil && e.Deployment != "" {
				return m, m.startForm("chat", []string{"Type placement context to authorize interactive Tasks: " + e.Context}, []string{""})
			}
			m.status = "Select a retained suite placement to chat; extract standalone Agents first"
		case "H":
			if e != nil && e.Copilot != nil && e.Deployment == "" && e.Reviewed {
				return m, m.startForm("copilot-reconnect", []string{"Type copilot to reconnect the reviewed source to your local CLI"}, []string{""})
			}
			m.status = "Review Copilot workspace source and cases with i before reconnecting"
		case "u":
			if e != nil && e.Copilot != nil && e.Deployment == "" {
				return m, m.startForm("copilot-run", []string{"Prompt for local Copilot CLI", "Type copilot to send instructions and prompt using your host login"}, []string{"", ""})
			}
			if e != nil && e.Deployment != "" {
				return m, m.startForm("run", []string{"Prompt for one fresh member Task", "Type destination context to authorize execution"}, []string{"", ""})
			}
			m.status = "Run requires a retained suite placement; extract standalone Agents first"
		case "R":
			if e != nil {
				if e.Image {
					m.status = "Image requests have no Orka Task history"
					return m, nil
				}
				if e.Context == "" {
					m.status = "Select a placement or live member for Activity"
					return m, nil
				}
				env := agentTUIEnvironment{Name: e.Context}
				agent := agentTUIAgent{Name: e.Name, Namespace: m.opt.Namespace, Runtime: "orka"}
				if e.Live != nil {
					agent = *e.Live
				} else if e.Deployment != "" {
					d, err := ReadSuiteDeployment(e.Deployment)
					if err != nil {
						m.status = err.Error()
						return m, nil
					}
					agent.Name, agent.Namespace = d.Summary.Member, d.Summary.Target.Namespace
				}
				r := newAgentTUIModel(m.opt)
				r.width, r.height = m.width, m.height
				r.inferenceContext = m.ctx
				r.loadRuns = m.a.consoleRecentOrkaRuns
				r.loadRun = func(ctx context.Context, env agentTUIEnvironment, ref consoleRunRef) (runview.Run, error) {
					return env.app(m.a).withRunContext(ctx).ReadOrkaRun(ctx, ref.Namespace, ref.Name, ref.UID)
				}
				next, cmd := r.openRunsFor(env, agent)
				r = next.(agentTUIModel)
				m.runs = &r
				return m, cmd
			}
		case "d":
			if e != nil && e.Root != "" && e.Review != nil {
				return m, m.startForm("edit", []string{"Workspace-relative source/case file"}, []string{"agents/" + e.Review.member() + "/instructions.md"})
			}
		}
	}
	return m, nil
}

func consoleDestination(root, name string) (config.Destination, error) {
	d, err := config.LoadDestinations(root)
	if err != nil {
		return config.Destination{}, err
	}
	if d == nil {
		return config.Destination{}, errors.New("author named destinations in workspace kmx.yaml first")
	}
	v, ok := d.Targets[name]
	if !ok {
		return v, config.ErrDestinationUnknown
	}
	if v.Runtime != "" && v.Runtime != "orka" || v.Bindings != "" || v.Inference == "" {
		return v, errors.New("console lifecycle currently supports native Orka with explicit inference only")
	}
	if v.Namespace == "" {
		v.Namespace = OrkaNamespace
	}
	return v, nil
}

func (m *suiteConsole) submit(f suiteConsoleForm) tea.Cmd {
	m.operationAction = f.action
	e := m.entry()
	var selected suiteConsoleEntry
	if e != nil {
		selected = *e
	}
	v := f.values
	switch f.action {
	case "aks-connect":
		cluster := m.pendingAKS
		if v[0] != cluster.Name {
			m.status = "Exact AKS cluster name required"
			return nil
		}
		return m.job(func(ctx context.Context, w *App) suiteConsoleResult {
			env, err := w.prepareConsoleContext(ctx, "", &cluster, false)
			return suiteConsoleResult{connected: &env, err: err}
		})
	case "kind-connect", "local-init":
		initialize := f.action == "local-init"
		confirmation := "connect"
		if initialize {
			confirmation = "initialize"
		}
		if v[1] != confirmation {
			m.status = "Local environment preparation cancelled"
			return nil
		}
		return m.job(func(ctx context.Context, w *App) suiteConsoleResult {
			env, err := w.prepareConsoleContext(ctx, v[0], nil, initialize)
			return suiteConsoleResult{connected: &env, err: err}
		})
	case "open":
		m.opt.Workspace = v[0]
		return m.job(func(ctx context.Context, w *App) suiteConsoleResult {
			r, err := w.reviewSuiteWorkspace(ctx, v[0])
			if err != nil {
				return suiteConsoleResult{err: err}
			}
			return suiteConsoleResult{entry: &suiteConsoleEntry{Name: r.name(), Origin: "workspace", Root: r.root, Review: r}, text: "Source loaded; review cases before execution"}
		})
	case "create":
		return m.job(func(ctx context.Context, w *App) suiteConsoleResult {
			text := v[3]
			if strings.HasPrefix(text, "@") {
				var err error
				text, err = readRunPromptFile(strings.TrimPrefix(text, "@"))
				if err != nil {
					return suiteConsoleResult{err: err}
				}
			}
			options := workspace.CreateOptions{Name: v[1], Model: v[2]}
			if f.execution == "image-http" {
				options.Model = ""
				options.Execution = workspace.ImageExecution
				options.ContextTokens = 8192
				options.OutputTokens = 1024
			}
			if err := workspace.CreateExclusive(ctx, v[0], options, text); err != nil {
				return suiteConsoleResult{err: err}
			}
			r, err := w.reviewSuiteWorkspace(ctx, v[0])
			if err != nil {
				return suiteConsoleResult{err: err}
			}
			binding := f.copilot
			if binding != nil {
				copy := *binding
				copy.Digest = r.digest()
				binding = &copy
			}
			return suiteConsoleResult{entry: &suiteConsoleEntry{Name: r.name(), Origin: "workspace", Root: r.root, Review: r, Copilot: binding}, text: "Agent created. Files saved to " + r.root + ". Review example cases before evaluation."}
		})
	case "copilot-run":
		if v[1] != "copilot" {
			m.status = "Local Copilot send cancelled"
			return nil
		}
		return m.job(func(ctx context.Context, w *App) suiteConsoleResult {
			answer, err := w.runSuiteCopilot(ctx, selected, v[0])
			return suiteConsoleResult{text: answer, err: err}
		})
	case "independent-chat":
		if selected.Live == nil || selected.Live.SuiteOwned || selected.Live.External || v[0] != selected.Context {
			m.status = "Select an independent agent and confirm its exact context"
			return nil
		}
		m.standaloneName = selected.Live.Name
		m.standaloneNamespace = selected.Live.Namespace
		m.standaloneContext = selected.Context
		m.standaloneEnvironment = selected.Environment
		m.chatConfirm = v[0]
		return tea.Quit
	case "copilot-chat":
		if v[0] != "copilot" {
			m.status = "Local Copilot chat cancelled"
			return nil
		}
		m.hostChat = &selected
		return tea.Quit
	case "copilot-reconnect":
		if v[0] != "copilot" || selected.Copilot == nil || !selected.Reviewed {
			return nil
		}
		return m.job(func(ctx context.Context, w *App) suiteConsoleResult {
			if err := selected.Review.checkCurrent(ctx); err != nil {
				return suiteConsoleResult{err: err}
			}
			binding := *selected.Copilot
			binding.Digest = selected.Review.digest()
			selected.Copilot = &binding
			if _, _, err := w.checkSuiteCopilot(ctx, selected); err != nil {
				return suiteConsoleResult{err: err}
			}
			return suiteConsoleResult{entry: &selected, text: "Local Copilot reconnected to reviewed source; no prompt sent"}
		})
	case "contexts":
		if m.refreshCancel != nil {
			m.refreshCancel()
		}
		m.refreshGeneration++
		m.refreshing = false
		m.opt.LocalContext, m.opt.RemoteContext = v[0], v[1]
		return m.refreshLive()
	case "runtime-check":
		return m.job(func(ctx context.Context, w *App) suiteConsoleResult {
			report, err := w.CheckSuiteRuntime(ctx, ReleaseLiftOptions{Reference: v[0], FromLayout: v[1], ToContext: v[2], ToNamespace: v[3], Environment: selected.EnvironmentFile})
			raw, _ := json.MarshalIndent(report, "", "  ")
			return suiteConsoleResult{text: string(raw), err: err}
		})
	case "runtime-install":
		return m.job(func(ctx context.Context, w *App) suiteConsoleResult {
			err := w.PrepareConsoleRuntime(ctx, v[0], v[1])
			return suiteConsoleResult{text: "Runtime preparation finished; inspect current runtime before Lift. Inference is separately configured.", err: err}
		})
	case "extract":
		return m.job(func(ctx context.Context, w *App) suiteConsoleResult {
			if selected.Environment.Name != "" {
				w = selected.Environment.app(w).withRunContext(ctx)
			} else {
				w = w.releaseLiftWorker(ctx, ReleaseLiftOptions{ToContext: selected.Context})
			}
			p, err := w.PrepareSuiteExtraction(ctx, SuiteExtractOptions{Agent: selected.Name, Namespace: selected.Live.Namespace, Name: v[1], Workspace: v[0]})
			return suiteConsoleResult{extract: p, err: err}
		})
	case "eval":
		if !selected.Reviewed {
			m.status = "Review all authored source/cases before evaluation"
			return nil
		}
		if f.destination == nil {
			m.status = "Destination must be resolved and reviewed first"
			return nil
		}
		d := *f.destination
		if v[1] != d.Context {
			m.status = "Type the exact destination context before execution"
			return nil
		}
		return m.job(func(ctx context.Context, w *App) suiteConsoleResult {
			if selected.Review == nil {
				return suiteConsoleResult{err: errors.New("source review required")}
			}
			server, err := w.consoleCreateTarget(ctx, agentTUIEnvironment{Name: d.Context})
			if err != nil || server != f.server {
				return suiteConsoleResult{err: errors.New("evaluation destination changed after review")}
			}
			if err := selected.Review.checkCurrent(ctx); err != nil {
				return suiteConsoleResult{err: err}
			}
			base, err := config.StateDir()
			if err != nil {
				return suiteConsoleResult{err: err}
			}
			if err = os.MkdirAll(base, 0700); err != nil {
				return suiteConsoleResult{err: err}
			}
			dir, err := os.MkdirTemp(base, "suite-eval-")
			if err != nil {
				return suiteConsoleResult{err: err}
			}
			p, err := PrepareSuiteWorkspaceEvaluation(ctx, SuiteWorkspaceEvaluationOptions{Workspace: selected.Root, ToContext: d.Context, ToNamespace: d.Namespace, Inference: d.Inference, Out: filepath.Join(dir, "passing.json"), ManagedPass: true, ResultPort: w.quickstartResultPort, JudgeBaseURL: v[2], JudgeKeyEnv: v[3]})
			if err != nil {
				return suiteConsoleResult{err: err}
			}
			defer p.Close()
			if p.prepared.Fingerprint != selected.Review.prepared.Fingerprint {
				return suiteConsoleResult{err: errors.New("source changed; reopen/review before evaluation")}
			}
			w = w.releaseLiftWorker(ctx, ReleaseLiftOptions{ToContext: d.Context})
			w.Cfg.Confirm = d.Context
			if err = w.EvaluateSuiteWorkspace(ctx, p); err != nil {
				return suiteConsoleResult{err: err}
			}
			selected.Evidence = p.options.Out
			return suiteConsoleResult{entry: &selected, text: "Full passing evidence: " + selected.Evidence}
		})
	case "push":
		if len(v) == 4 {
			m.publication = f
			return m.startForm("publish-confirm", []string{"Publish authored instructions and cases to " + v[0] + "? Type publish"}, []string{""})
		}
	case "publish-confirm":
		if v[0] != "publish" {
			m.liftAfterPublish = ""
			m.status = "Publication cancelled"
			return nil
		}
		v = m.publication.values
		return m.job(func(ctx context.Context, w *App) suiteConsoleResult {
			if selected.Review == nil {
				return suiteConsoleResult{err: errors.New("workspace required")}
			}
			if err := selected.Review.checkCurrent(ctx); err != nil {
				return suiteConsoleResult{err: err}
			}
			w.suiteProgress("publication", selected.Name, "running")
			var r suiteoras.PushResult
			var err error
			r, err = w.PushSuiteWorkspaceManaged(ctx, SuiteWorkspacePushOptions{Workspace: selected.Root, Reference: v[0], Layout: v[1], PlainHTTP: v[3] == "yes", Evaluation: v[2]})
			if err != nil {
				return suiteConsoleResult{err: err}
			}
			ref := v[0]
			if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
				ref = ref[:i]
			}
			selected.Reference = ref + "@" + r.Descriptor.Digest.String()
			selected.Layout = v[1]
			selected.Evidence = v[2]
			selected.Published = publicationHint(selected, v[3] == "yes")
			w.suiteProgress("publication", selected.Name, "published")
			return suiteConsoleResult{entry: &selected, text: "Published exact artifact: " + selected.Reference}
		})
	case "lift":
		if selected.Root == "" {
			m.status = "Open the authored workspace to choose named destinations; explicit-reference CLI Lift remains source independent"
			return nil
		}
		if selected.Review != nil {
			if err := selected.Review.checkCurrent(m.ctx); err != nil {
				m.status = "Source changed; reopen and review before lifting from this workspace"
				return nil
			}
		}
		d, err := consoleDestination(selected.Root, v[2])
		if err != nil {
			m.status = err.Error()
			return nil
		}
		o := ReleaseLiftOptions{Reference: v[0], Evidence: v[1], ToContext: d.Context, ToNamespace: d.Namespace, Inference: d.Inference, FromLayout: v[3], Promote: v[4] == "yes", PlainHTTP: v[5] == "yes"}
		return m.job(func(ctx context.Context, w *App) suiteConsoleResult {
			w.suiteProgress("Lift plan", selected.Name, "reading")
			p, err := w.PrepareReleaseLift(ctx, o)
			return suiteConsoleResult{plan: p, err: err}
		})
	case "apply":
		p := m.plan
		if p == nil {
			return nil
		}
		if v[0] != p.summary.Target.Context {
			m.status = "Exact destination context confirmation required"
			return nil
		}
		m.plan = nil
		return m.job(func(ctx context.Context, w *App) suiteConsoleResult {
			if selected.Environment.Name == p.options.ToContext {
				w = selected.Environment.app(w)
			}
			w = w.releaseLiftWorker(ctx, p.options)
			w.Cfg.Confirm = v[0]
			r, err := w.ApplyReleaseLift(ctx, p, p.Summary().PlanDigest)
			if err != nil {
				return suiteConsoleResult{result: r, err: err}
			}
			id := r.Placement
			if p.image == nil {
				id, err = w.RetainSuiteDeployment(ctx, p, r)
			}
			if err != nil {
				return suiteConsoleResult{result: r, err: err}
			}
			selected.Name = p.summary.Suite
			selected.Origin = "retained placement"
			selected.Deployment = id
			selected.Context = p.summary.Target.Context
			selected.Side = 0
			if selected.Context == m.columns[1].Env.Name {
				selected.Side = 1
			}
			selected.Reference = p.options.Reference
			selected.Evidence = p.options.Evidence
			selected.Layout = p.options.FromLayout
			selected.Image = p.image != nil
			selected.ObservedReady = true
			selected.EnvironmentFile = p.options.Environment
			return suiteConsoleResult{entry: &selected, result: r, text: "Placement verified. Deployment: " + id + ". Chat/run requests a separate fresh answer."}
		})
	case "run":
		return m.job(func(ctx context.Context, w *App) suiteConsoleResult {
			if len(selected.Deployment) == 64 {
				x, err := w.loadImagePlacement(ctx, selected.Deployment)
				if err != nil {
					return suiteConsoleResult{err: err}
				}
				if v[1] != x.env.Context {
					return suiteConsoleResult{err: errors.New("exact context required")}
				}
				answer, err := w.RunImagePlacement(ctx, selected.Deployment, v[0])
				return suiteConsoleResult{text: answer, err: err, feedback: consoleAnswerFeedback(answer, err)}
			}
			d, err := ReadSuiteDeployment(selected.Deployment)
			if err != nil {
				return suiteConsoleResult{err: err}
			}
			if v[1] != d.Summary.Target.Context {
				return suiteConsoleResult{err: errors.New("exact destination context confirmation required")}
			}
			cfg := *w.Cfg
			cfg.Confirm = v[1]
			w.Cfg = &cfg
			answer, err := w.RunSuiteMember(ctx, selected.Deployment, "", v[0])
			return suiteConsoleResult{text: answer, err: err, feedback: consoleAnswerFeedback(answer, err)}
		})
	case "run-live":
		if selected.Live == nil || !selected.Live.canChat() || v[1] != selected.Context {
			m.status = "Independent Agent and exact context required"
			return nil
		}
		return m.job(func(ctx context.Context, w *App) suiteConsoleResult {
			w = w.releaseLiftWorker(ctx, ReleaseLiftOptions{ToContext: selected.Context})
			w.Cfg.Confirm = v[1]
			var out strings.Builder
			w.Out = &out
			err := w.RunAgent(RunAgentOptions{Agent: selected.Name, Namespace: selected.Live.Namespace, Prompt: v[0]})
			return suiteConsoleResult{text: out.String(), err: err}
		})
	case "chat":
		if len(selected.Deployment) == 64 {
			if v[0] != selected.Context {
				m.status = "Exact placement context required"
				return nil
			}
			m.chatID, m.chatConfirm = selected.Deployment, v[0]
			return tea.Quit
		}
		d, err := ReadSuiteDeployment(selected.Deployment)
		if err != nil {
			m.status = err.Error()
			return nil
		}
		if v[0] != d.Summary.Target.Context {
			m.status = "Exact placement context required"
			return nil
		}
		m.chatID, m.chatConfirm = selected.Deployment, v[0]
		return tea.Quit
	case "edit":
		if !filepath.IsLocal(v[0]) {
			m.status = "Choose a workspace-relative file"
			return nil
		}
		rootResolved, resolveErr := filepath.EvalSymlinks(selected.Root)
		if resolveErr != nil || rootResolved != selected.Root {
			m.status = "Workspace root changed or contains links"
			return nil
		}
		path := filepath.Join(selected.Root, v[0])
		if v[0] == "kmx.yaml" {
			if _, err := os.Lstat(path); os.IsNotExist(err) {
				file, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if e == nil {
					_, e = file.WriteString("version: 1\ntargets: {}\n")
					e = errors.Join(e, file.Close())
				}
				if e != nil {
					m.status = e.Error()
					return nil
				}
			}
		}
		resolved, err := filepath.EvalSymlinks(path)
		info, statErr := os.Lstat(path)
		if err != nil || statErr != nil || resolved != path || !info.Mode().IsRegular() {
			m.status = "Choose an existing regular source file"
			return nil
		}
		if entry := m.entry(); entry != nil {
			entry.Reviewed = false
			entry.Origin = "workspace · edited; reopen to refresh"
		}
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "vi"
		}
		return tea.ExecProcess(exec.Command(editor, path), func(err error) tea.Msg {
			return suiteConsoleResult{op: m.op, err: err, text: "Source edited; reopen to refresh identity. Previous evidence is not proof of changes."}
		})
	case "command":
		commands := map[string]string{"open": "o", "create": "n", "contexts": "t", "extract": "x", "suite extract": "x", "show": "i", "suite show": "i", "eval": "e", "push": "p", "lift": "l", "runtime check": "v", "runtime install": "I", "status": "s", "suite status": "s", "run": "u", "chat": "c", "edit": "d", "refresh": "r", "help": "?", "exit": "q"}
		key, ok := commands[strings.TrimPrefix(strings.TrimSpace(v[0]), "/")]
		if !ok {
			m.status = "Unknown command; ? lists actions"
			return nil
		}
		_, cmd := m.Update(tea.KeyPressMsg{Code: []rune(key)[0]})
		return cmd
	case "azure-select":
		for _, candidate := range m.azure {
			if candidate.Context != v[0] {
				continue
			}
			for _, registry := range candidate.Registries {
				if registry.LoginServer == v[1] {
					m.opt.RemoteContext = v[0]
					m.opt.Suite.Registry = v[1] + "/agentsuite:latest"
					m.status = "Selected AKS and ACR; publisher login and actual node pull remain independently checked"
					return m.refreshLive()
				}
			}
		}
		m.status = "Select an observed AKS context and ACR login server"
		return nil
	case "image-push":
		if v[2] != "publish" {
			m.liftAfterPublish = ""
			m.status = "Publication cancelled"
			return nil
		}
		options := m.opt.Suite.Build
		options.Builder = v[1]
		plain := m.opt.Suite.PlainHTTP
		return m.job(func(ctx context.Context, w *App) suiteConsoleResult {
			if err := selected.Review.checkCurrent(ctx); err != nil {
				return suiteConsoleResult{err: err}
			}
			w.suiteProgress("build/publish", selected.Name, "running")
			pub, err := w.PublishSuiteWorkspace(ctx, selected.Root, selected.Name, v[0], plain, options)
			if err != nil {
				return suiteConsoleResult{err: err}
			}
			selected.Reference = pub.Release
			selected.Image = true
			selected.Published = publicationHint(selected, plain)
			w.suiteProgress("build/publish", selected.Name, "published")
			return suiteConsoleResult{entry: &selected, text: "Complete image release: " + pub.Release}
		})
	case "image-eval":
		if !selected.Reviewed {
			m.status = "Review source and cases first"
			return nil
		}
		env, err := ReadSuiteEnvironment(v[1])
		if err != nil {
			m.status = err.Error()
			return nil
		}
		if v[2] != env.Context {
			m.status = "Exact context confirmation required"
			return nil
		}
		return m.job(func(ctx context.Context, w *App) suiteConsoleResult {
			if err := selected.Review.checkCurrent(ctx); err != nil {
				return suiteConsoleResult{err: err}
			}
			base, err := config.StateDir()
			if err != nil {
				return suiteConsoleResult{err: err}
			}
			if err = os.MkdirAll(base, 0700); err != nil {
				return suiteConsoleResult{err: err}
			}
			dir, err := os.MkdirTemp(base, "image-eval-")
			if err != nil {
				return suiteConsoleResult{err: err}
			}
			evidence := filepath.Join(dir, "passing.json")
			w = w.releaseLiftWorker(ctx, ReleaseLiftOptions{ToContext: env.Context})
			w.Cfg.Confirm = env.Context
			opt := ReleaseLiftOptions{Reference: v[0], Environment: v[1], ToContext: env.Context, ToNamespace: env.Namespace, PlainHTTP: env.PlainHTTP, Evidence: evidence}
			candidate, err := w.prepareImageCandidate(ctx, opt)
			if err != nil {
				return suiteConsoleResult{err: err}
			}
			if candidate.suite.Digest != selected.Review.digest() {
				return suiteConsoleResult{err: errors.New("image release differs from reviewed source")}
			}
			err = w.EvaluateImageRelease(ctx, opt, evidence)
			if err != nil {
				return suiteConsoleResult{err: err}
			}
			if err := selected.Review.checkCurrent(ctx); err != nil {
				return suiteConsoleResult{err: err}
			}
			selected.Evidence, selected.Reference, selected.EnvironmentFile = evidence, v[0], v[1]
			selected.Image = true
			return suiteConsoleResult{entry: &selected, text: "Full image evaluation passed: " + evidence}
		})
	case "image-lift":
		env, err := ReadSuiteEnvironment(v[1])
		if err != nil {
			m.status = err.Error()
			return nil
		}
		opt := ReleaseLiftOptions{Reference: v[0], Environment: v[1], Evidence: v[2], ToContext: env.Context, ToNamespace: env.Namespace, PlainHTTP: env.PlainHTTP, Promote: v[3] == "yes"}
		return m.job(func(ctx context.Context, w *App) suiteConsoleResult {
			p, err := w.PrepareReleaseLift(ctx, opt)
			return suiteConsoleResult{plan: p, err: err}
		})
	}
	return nil
}

func (m *suiteConsole) plainView() tea.View {
	if m.runs != nil {
		return m.runs.View()
	}
	w := max(20, m.width)
	h := max(8, m.height)
	var rows []string
	rows = append(rows, "AGENTSUITES · Create → Prove → Publish → Lift", "")
	switch {
	case m.help:
		rows = append(rows, "o open workspace · n create agent in a new suite · x extract standalone agent", "Enter inspect source/cases · d edit source/case file · e evaluate", "p publish · l review Lift · s placement status · u run member · c chat", "t choose contexts · R recent runs · Tab details · / commands", "r refresh · q quit · Esc back/cancel · arrows select/scroll", "Destinations are selected by name from workspace kmx.yaml.", "Extraction keeps originals. Ready is placement, not an answer.")
	case m.form != nil:
		rows = append(rows, strings.ToUpper(m.form.action), m.form.labels[m.form.field], ansi.Strip(m.input.View()), "Enter next/confirm · Esc cancel")
	case m.extraction != nil:
		rows = append(rows, "EXTRACT AGENTSUITE · review before saving", "Original: "+m.extraction.Summary.Context+" / "+m.extraction.Summary.Namespace+" / "+m.extraction.Summary.Agent, "New member: "+m.extraction.Summary.Member, "Exact model: "+m.extraction.Summary.Model, "Workspace: "+m.extraction.Summary.Workspace, "Instructions: "+suiteReviewPreview(m.extraction.Summary.Instructions), "Enter save new source only · Esc cancel")
	case m.plan != nil:
		s := m.plan.Summary()
		rows = append(rows, "LIFT REVIEW", s.Suite+" / "+s.Member, s.SuiteDigest, "Destination: "+s.Target.Context+" / "+s.Target.Namespace, "Gate: "+s.Gate+" · source evidence is not destination evaluation")
		for _, r := range s.Resources {
			rows = append(rows, r.Kind+"/"+r.Name+": "+r.Action)
		}
		rows = append(rows, "Enter confirm exact destination · Esc cancel")
	case m.review != nil:
		var body []string
		if m.page == 0 {
			body = m.review.sourceLines(w - 4)
		} else {
			body = m.review.caseLines(m.page-1, w-4)
		}
		start := min(m.scroll, max(0, len(body)-1))
		rows = append(rows, body[start:min(len(body), start+h-7)]...)
		rows = append(rows, "Enter next case · ← back · ↑↓ scroll · Esc close")
	default:
		tabs := []string{"Source", "Members", "Cases", "Artifacts", "Destinations", "Activity"}
		rows = append(rows, strings.Join(tabs, " | ")+"  ["+tabs[m.tab]+"]", "")
		first := max(0, m.selected-2)
		last := min(len(m.entries), first+max(1, (h-14)/2))
		for i := first; i < last; i++ {
			e := m.entries[i]
			prefix := "  "
			if i == m.selected {
				prefix = "› "
			}
			rows = append(rows, prefix+e.Name+"  ["+e.Origin+"]")
			member := e.Name
			if e.Live != nil {
				member = e.Live.Name
			}
			if e.Review != nil {
				member = e.Review.member()
			}
			rows = append(rows, "    └ "+member)
			if i == m.selected && e.Reference != "" {
				rows = append(rows, "      "+e.Reference)
			}
		}
		if len(m.entries) == 0 {
			rows = append(rows, "No suites selected. Open a workspace (o) or create one (n).", "Standalone inventory requires explicit --local-context/--remote-context.")
		}
		if m.detail != "" {
			rows = append(rows, "", m.detail)
		}
		if e := m.entry(); e != nil {
			switch m.tab {
			case 0:
				rows = append(rows, "Workspace: "+e.Root)
				if e.Review != nil {
					rows = append(rows, "Prepared: "+e.Review.digest(), "Enter reviews exact source and all cases · d edits files")
				}
			case 1:
				if e.Review != nil {
					rows = append(rows, "Member: "+e.Review.member()+" · "+e.Review.profile()+" · "+e.Review.model())
				}
				rows = append(rows, "Agent actions: run / chat / inspect remain member-scoped")
			case 2:
				if e.Review != nil {
					for _, c := range e.Review.inventory {
						rows = append(rows, c.Case.ID+" · "+c.Metadata.CaseDigest)
					}
				}
				rows = append(rows, "Evidence hint: "+e.Evidence+" (rechecked on consumption)")
			case 3:
				rows = append(rows, "Published: "+e.Reference, "Layout: "+e.Layout, "Native Orka: no member image required")
			case 4:
				if e.Root != "" {
					rows = append(rows, m.destinationHints(e.Root))
				}
				rows = append(rows, "Placement: "+e.Deployment, "Context: "+e.Context, "s revalidates status · Ready is not an answer")
			case 5:
				rows = append(rows, "R opens bounded recent member runs; observations retain historical identities")
			}
		}
	}
	if m.running {
		rows = append(rows, "", "WORKING · Esc cancels observation/operation; completed resources may remain")
		if m.cancelling {
			rows = append(rows, "Waiting for cancellation…")
		}
	}
	start := max(0, len(m.progress)-5)
	for _, p := range m.progress[start:] {
		rows = append(rows, p.At.Format("15:04:05")+" · "+p.Stage+" · "+p.Object+" · "+p.State)
	}
	rows = append(rows, "", m.status, "o open · n create · x extract · e prove · p publish · l lift · c chat · ? help")
	return suiteConsoleRowsView(rows, m.width, m.height, m.status)
}
