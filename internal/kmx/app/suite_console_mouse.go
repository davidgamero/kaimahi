package app

import tea "charm.land/bubbletea/v2"

// Hit regions are collected by the same render pass that wraps and clips text.
// Mouse input never estimates a row from a label or acts through an overlay.
type suiteMouseRegion struct {
	x, y, width, height int
	action              string
	index               int
}

func (m *suiteConsole) mouseRegion(r suiteMouseRegion) {
	if m.mouseRegions != nil && r.width > 0 && r.height > 0 {
		*m.mouseRegions = append(*m.mouseRegions, r)
	}
}

func (m *suiteConsole) mouseKey(code rune) (tea.Model, tea.Cmd) {
	return m.Update(tea.KeyPressMsg{Code: code})
}

func (m *suiteConsole) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.running || m.runs != nil || m.review != nil || m.width < 54 || m.height < 16 {
		return m, nil
	}
	mouse := msg.Mouse()
	if mouse.X < 0 || mouse.Y < 0 || mouse.X >= m.width || mouse.Y >= m.height {
		return m, nil
	}
	if _, ok := msg.(tea.MouseWheelMsg); ok {
		delta := 0
		switch mouse.Button {
		case tea.MouseWheelDown:
			delta = 1
		case tea.MouseWheelUp:
			delta = -1
		default:
			return m, nil
		}
		if m.contexts != nil || m.creation != nil || m.actionsOpen || m.form != nil || m.publications != nil || m.sortOpen || m.listSearching || m.liftGuide != nil {
			if delta > 0 {
				return m.mouseKey(tea.KeyDown)
			}
			return m.mouseKey(tea.KeyUp)
		}
		if m.review != nil || m.extraction != nil || m.plan != nil || m.help {
			if delta > 0 {
				return m.mouseKey(tea.KeyPgDown)
			}
			return m.mouseKey(tea.KeyPgUp)
		}
		if m.feedback != nil {
			if delta > 0 {
				return m.mouseKey(tea.KeyPgDown)
			}
			return m.mouseKey(tea.KeyPgUp)
		}
		left, body, _ := m.paneGeometry()
		if mouse.Y < 2 || mouse.Y >= 2+body {
			return m, nil
		}
		side := 0
		if mouse.X >= left {
			side = 1
		}
		if side != m.focus {
			m.selectSide(side)
		}
		if m.tab == 0 || mouse.Y < 2+body/2 {
			m.moveSide(delta)
		} else {
			m.detailScroll = max(0, min(m.maxDetailScroll(), m.detailScroll+delta*3))
		}
		return m, nil
	}
	if _, ok := msg.(tea.MouseClickMsg); !ok || mouse.Button != tea.MouseLeft {
		return m, nil
	}
	var regions []suiteMouseRegion
	copy := *m
	copy.mouseRegions = &regions
	copy.View()
	for i := len(regions) - 1; i >= 0; i-- {
		r := regions[i]
		if mouse.X < r.x || mouse.X >= r.x+r.width || mouse.Y < r.y || mouse.Y >= r.y+r.height {
			continue
		}
		switch r.action {
		case "block":
			return m, nil
		case "tab":
			m.tab = r.index
			m.detailScroll = 0
		case "sort":
			m.selectSide(r.index)
			m.listSearching = false
			m.sortOpen = true
			m.sortSelection = m.listSort[r.index]
		case "search":
			if m.focus != r.index {
				m.selectSide(r.index)
			}
			m.listSearching = true
		case "sort-choice":
			m.setListSort(r.index)
			m.sortOpen = false
		case "side":
			if m.focus != r.index {
				m.selectSide(r.index)
			}
		case "entry":
			m.listSearching = false
			e := m.entries[r.index]
			m.selectSide(e.Side)
			for pos, index := range m.sideIndices(e.Side) {
				if index == r.index {
					m.columnSelection[e.Side] = pos
					break
				}
			}
			m.selectSide(e.Side)
			m.actionsOpen = true
			m.actionSelection = 0
		case "new":
			m.selectSide(r.index)
			return m, m.startCreation()
		case "context":
			m.selectSide(r.index)
			return m, m.openContexts(r.index)
		case "refresh":
			return m, m.refreshLive()
		case "open":
			return m.mouseKey('o')
		case "authoring":
			return m.mouseKey('w')
		case "creation":
			m.creation.step().selection = r.index
			return m, m.advanceCreation()
		case "context-choice":
			m.contexts.selection = r.index
			return m.mouseKey(tea.KeyEnter)
		case "publication":
			m.publications.selection = r.index
			return m.mouseKey(tea.KeyEnter)
		case "guide":
			m.liftGuide.selection = r.index
			return m.mouseKey(tea.KeyEnter)
		case "guide-next":
			return m.mouseKey(tea.KeyEnter)
		case "action":
			m.actionSelection = r.index
			return m.mouseKey(tea.KeyEnter)
		case "completion":
			m.completionSelection = r.index
			choices := m.formCompletions()
			if r.index < len(choices) {
				m.input.SetValue(choices[r.index])
				m.input.CursorEnd()
				m.completionSelection = 0
			}
		case "next":
			return m.mouseKey(tea.KeyEnter)
		case "cancel":
			return m.mouseKey(tea.KeyEsc)
		case "back":
			return m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt})
		case "aks":
			return m, m.discoverContextAKS()
		}
		return m, nil
	}
	return m, nil
}
