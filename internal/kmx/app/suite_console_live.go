package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
)

type suiteConsoleColumn struct {
	Env      agentTUIEnvironment
	Error    string
	Observed time.Time
}
type suiteConsoleRefresh struct {
	generation int
	result     suiteConsoleResult
}
type suiteConsoleRefreshTick struct{}

// Access discovery is local kubeconfig inspection. Only the selected context
// per side is contacted; a failed side never discards the other side's results.
func loadSuiteConsole(ctx context.Context, a *App, opt AgentTUIOptions) suiteConsoleResult {
	result := loadSuiteConsoleSources(ctx, a, opt)
	envs, err := a.agentTUIEnvironments(ctx)
	columns := [2]suiteConsoleColumn{}
	result.columns = &columns
	if err != nil && len(envs) == 0 {
		columns[0].Error = "Context discovery unavailable"
		columns[1].Error = columns[0].Error
		result.err = errors.Join(result.err, fmt.Errorf("context discovery unavailable; local source remains available"))
		return result
	}
	result.err = errors.Join(result.err, err)
	prefs, prefErr := loadLiftPreferences()
	preferred := agentTUIPreferredEnvironments(envs, prefs, a.Cfg.KubeContext)
	explicit := [2]string{opt.LocalContext, opt.RemoteContext}
	for side := range columns {
		name := explicit[side]
		if name == "" {
			name = preferred[side]
		}
		for _, env := range envs {
			if env.Local != (side == 0) {
				continue
			}
			if name == "" || env.Name == name {
				columns[side].Env = env
				break
			}
		}
		if name != "" && columns[side].Env.Name == "" {
			columns[side].Error = "Selected context unavailable or wrong local/remote type: " + safeTerminal(name)
		}
	}
	if opt.Deployment != "" {
		targetContext := ""
		for _, entry := range result.entries {
			if entry.Deployment == opt.Deployment {
				targetContext = entry.Context
				break
			}
		}
		for _, env := range envs {
			if env.Name == targetContext {
				side := 1
				if env.Local {
					side = 0
				}
				if explicit[side] != "" && explicit[side] != env.Name {
					result.err = errors.New("selected deployment conflicts with explicit console context")
					return result
				}
				columns[side].Env = env
				break
			}
		}
	}
	// Workspaces always live in the local authoring pane. Retained placements
	// are shown only on their selected context, never by guessing from a name.
	rows := make([]suiteConsoleEntry, 0, len(result.entries))
	for _, e := range result.entries {
		if e.Deployment == "" {
			e.Side = 0
			rows = append(rows, e)
			continue
		}
		matched := false
		for side, c := range columns {
			if c.Env.Name != "" && e.Context == c.Env.Name {
				e.Side = side
				e.Environment = c.Env
				rows = append(rows, e)
				matched = true
				break
			}
		}
		if !matched && opt.Deployment == e.Deployment {
			for _, env := range envs {
				if env.Name == e.Context {
					side := 1
					if env.Local {
						side = 0
					}
					if explicit[side] != "" && explicit[side] != env.Name {
						result.err = errors.New("selected deployment conflicts with explicit console context")
						break
					}
					columns[side].Env = env
					e.Side = side
					e.Environment = env
					rows = append(rows, e)
					matched = true
					break
				}
			}
			if !matched && result.err == nil {
				result.err = errors.New("selected deployment context unavailable")
			}
		}
	}
	type inventory struct {
		side   int
		agents []agentTUIAgent
		err    error
	}
	ch := make(chan inventory, 2)
	count := 0
	for side, c := range columns {
		if c.Env.Name == "" {
			continue
		}
		count++
		go func(side int, env agentTUIEnvironment) {
			readCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			agents, err := a.agentTUIInventory(readCtx, env, valueOr(opt.Namespace, OrkaNamespace))
			ch <- inventory{side, agents, err}
		}(side, c.Env)
	}
	for i := 0; i < count; i++ {
		inv := <-ch
		c := &columns[inv.side]
		if inv.err != nil {
			c.Error = "Live inventory unavailable (permission, connection or malformed response); not absence"
		} else {
			c.Observed = time.Now()
		}
		for _, agent := range inv.agents {
			if inv.side == 0 && (inv.err != nil || agent.Ready != "yes" || agent.Deleting) {
				continue
			}
			v := agent
			matched := false
			for j := range rows {
				e := &rows[j]
				if e.Deployment == "" || e.Context != c.Env.Name {
					continue
				}
				d, readErr := ReadSuiteDeployment(e.Deployment)
				if readErr != nil {
					continue
				}
				if d.Summary.Member != agent.Name || d.Summary.Target.Namespace != agent.Namespace || d.Placement.Agent.UID != agent.UID {
					continue
				}
				e.Live = &v
				e.ObservedReady = agent.Ready == "yes" && !agent.Deleting
				e.Observation = "Live identity requires revalidation"
				if agent.UID != d.Placement.Agent.UID {
					e.Observation = "Resource UID changed · invocation refused"
				}
				matched = true
				break
			}
			if matched {
				continue
			}
			name, origin := agent.Name, "standalone · not extracted"
			if agent.SuiteOwned {
				origin = "suite-owned · no retained placement"
				if agent.SuiteName != "" {
					name = agent.SuiteName
				}
			}
			rows = append(rows, suiteConsoleEntry{Name: name, Origin: origin, Live: &v, Context: c.Env.Name, Environment: c.Env, Side: inv.side, Observation: "Observed Ready: " + agent.Ready})
		}
	}
	// A private image record is a hint, not a live workload. Revalidate its exact
	// artifact/bindings/UIDs and Ready observation before listing it on kind.
	for i := range rows {
		e := &rows[i]
		if e.Side != 0 || !e.Image || e.Deployment == "" {
			continue
		}
		readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		status, err := e.Environment.app(a).ImagePlacementStatus(readCtx, e.Deployment)
		cancel()
		if err != nil {
			result.err = errors.Join(result.err, fmt.Errorf("local image placement %s unavailable", e.Deployment))
			continue
		}
		e.ObservedReady = len(status) > 0
		for _, member := range status {
			if !member.Ready {
				e.ObservedReady = false
			}
		}
	}
	result.entries = rows
	result.text = "Live local / remote view · refresh every 5s · Ready is not execution"
	if prefErr != nil {
		result.err = errors.Join(result.err, errors.New("saved context preferences unreadable; configured contexts used"))
	}
	return result
}

func (m *suiteConsole) refreshLive() tea.Cmd {
	if m.refreshing || m.running || m.creation != nil || m.contexts != nil || m.publications != nil || m.liftGuide != nil || m.feedback != nil {
		return nil
	}
	m.refreshGeneration++
	generation := m.refreshGeneration
	m.refreshing = true
	ctx, stop := context.WithTimeout(m.ctx, 30*time.Second)
	m.refreshCancel = stop
	opt := m.opt
	a := *m.a.withRunContext(ctx)
	m.workers.Add(1)
	return func() tea.Msg {
		defer m.workers.Done()
		defer stop()
		return suiteConsoleRefresh{generation, loadSuiteConsole(ctx, &a, opt)}
	}
}

func (m *suiteConsole) sideIndices(side int) []int {
	return m.sortedSideIndices(side)
}
func (m *suiteConsole) selectSide(side int) {
	m.focus = side
	indices := m.sideIndices(side)
	m.columnSelection[side] = min(m.columnSelection[side], max(0, len(indices)-1))
	m.selected = -1
	if len(indices) > 0 {
		m.selected = indices[m.columnSelection[side]]
	}
	m.detailScroll = 0
	m.detail = ""
}
func (m *suiteConsole) moveSide(delta int) {
	indices := m.sideIndices(m.focus)
	m.columnSelection[m.focus] = max(0, min(len(indices)-1, m.columnSelection[m.focus]+delta))
	m.selectSide(m.focus)
}
func suiteEntryKey(e suiteConsoleEntry) string {
	if e.Deployment != "" {
		return "placement/" + e.Deployment
	}
	if e.Live != nil {
		return e.Context + "/" + e.Live.Namespace + "/" + e.Live.Name + "/" + e.Live.UID
	}
	return "source/" + e.Root
}

func (m *suiteConsole) acceptRefresh(result suiteConsoleResult) {
	oldSelected := ""
	if e := m.entry(); e != nil {
		oldSelected = suiteEntryKey(*e)
	}
	oldScroll, oldDetail := m.detailScroll, m.detail
	var keys [2]string
	for side := range keys {
		indices := m.sideIndices(side)
		if len(indices) > 0 {
			keys[side] = suiteEntryKey(m.entries[indices[min(m.columnSelection[side], len(indices)-1)]])
		}
	}
	if result.entries != nil {
		// Retain review state only for the same exact source fingerprint.
		for i := range result.entries {
			fresh := &result.entries[i]
			for _, old := range m.entries {
				if suiteEntryKey(*fresh) == suiteEntryKey(old) && fresh.Review != nil && old.Review != nil && fresh.Review.prepared.Fingerprint == old.Review.prepared.Fingerprint {
					fresh.Reviewed = old.Reviewed
				}
			}
		}
		m.entries = result.entries
	}
	if result.columns != nil {
		m.columns = *result.columns
	}
	m.discoveryError = ""
	if result.err != nil {
		m.discoveryError = safeTerminal(result.err.Error())
	}
	for side := range keys {
		indices := m.sideIndices(side)
		for pos, i := range indices {
			if suiteEntryKey(m.entries[i]) == keys[side] {
				m.columnSelection[side] = pos
				break
			}
		}
	}
	m.selectSide(m.focus)
	if e := m.entry(); e != nil && suiteEntryKey(*e) == oldSelected {
		m.detailScroll, m.detail = oldScroll, oldDetail
	}
}
