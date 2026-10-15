package app

import (
	"context"
	"os"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite/workspace"
)

var suiteListSorts = []string{"Recently edited / created", "Oldest first", "Name A–Z", "Name Z–A"}

// Recency comes from authored files, not regenerated package caches. Failure to
// observe a timestamp leaves it unknown, never substitutes refresh time.
func suiteSourceModified(ctx context.Context, review *suiteWorkspaceReview) time.Time {
	if review == nil {
		return time.Time{}
	}
	files, err := workspace.SourceFiles(ctx, review.root, review.prepared)
	if err != nil {
		return time.Time{}
	}
	root, err := os.OpenRoot(review.root)
	if err != nil {
		return time.Time{}
	}
	defer root.Close()
	var latest time.Time
	for name := range files {
		if info, err := root.Stat(name); err == nil && info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest
}

func (e suiteConsoleEntry) modifiedTime() time.Time {
	latest := e.Modified
	if e.Review != nil && e.Review.modified.After(latest) {
		latest = e.Review.modified
	}
	if e.Live != nil && e.Live.Modified.After(latest) {
		latest = e.Live.Modified
	}
	return latest
}

func (e suiteConsoleEntry) inferenceLabel() string {
	if e.Copilot != nil && e.Deployment == "" && e.Review != nil {
		return "Copilot CLI / " + e.Review.model()
	}
	if e.Live != nil {
		return valueOr(e.Live.Provider, "inference") + " / " + valueOr(e.Live.Model, "unknown model")
	}
	if e.Inference != "" {
		return e.Inference
	}
	if e.Review != nil {
		if e.Review.image != nil {
			return "inference selected at evaluation"
		}
		return e.Review.model()
	}
	return "inference unknown"
}

func (m *suiteConsole) sideTotal(side int) int {
	total := 0
	for _, e := range m.entries {
		if m.listEntryVisible(e, side) {
			total++
		}
	}
	return total
}

func (m *suiteConsole) listEntryVisible(e suiteConsoleEntry, side int) bool {
	if e.Side != side {
		return false
	}
	if side != 0 {
		return true
	}
	if m.authoringView {
		return e.Deployment == "" && e.Live == nil && e.Review != nil
	}
	return e.Context != "" && e.Context == m.columns[0].Env.Name && m.columns[0].Env.Local && (e.ObservedReady || e.Live != nil && e.Live.Ready == "yes" && !e.Live.Deleting)
}

func (m *suiteConsole) sortedSideIndices(side int) []int {
	var indices []int
	for i, e := range m.entries {
		if !m.listEntryVisible(e, side) {
			continue
		}
		text := e.Name + " " + e.Origin + " " + e.Context + " " + e.inferenceLabel()
		if e.Review != nil {
			text += " " + e.Review.member() + " " + e.Review.model()
		}
		if e.Live != nil {
			text += " " + e.Live.Name + " " + e.Live.Model + " " + e.Live.Namespace
		}
		if _, ok := pickerFuzzyScore(text, m.listQuery[side]); ok {
			indices = append(indices, i)
		}
	}
	sort.SliceStable(indices, func(i, j int) bool {
		a, b := m.entries[indices[i]], m.entries[indices[j]]
		if m.listSort[side] < 2 {
			x, y := a.modifiedTime(), b.modifiedTime()
			// Unknown timestamps always follow observed ones, in either time order.
			if x.IsZero() != y.IsZero() {
				return !x.IsZero()
			}
			if !x.Equal(y) {
				if m.listSort[side] == 1 {
					return x.Before(y)
				}
				return x.After(y)
			}
		}
		x, y := strings.ToLower(a.Name), strings.ToLower(b.Name)
		if x != y {
			if m.listSort[side] == 3 {
				return x > y
			}
			return x < y
		}
		return suiteEntryKey(a) < suiteEntryKey(b)
	})
	return indices
}

func (m *suiteConsole) setListSort(order int) {
	key := ""
	if e := m.entry(); e != nil {
		key = suiteEntryKey(*e)
	}
	m.listSort[m.focus] = order
	m.columnSelection[m.focus] = 0
	for pos, i := range m.sideIndices(m.focus) {
		if suiteEntryKey(m.entries[i]) == key {
			m.columnSelection[m.focus] = pos
			break
		}
	}
	m.selectSide(m.focus)
}

func (m *suiteConsole) updateListControls(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.sortOpen {
		switch key.String() {
		case "esc":
			m.sortOpen = false
		case "up":
			m.sortSelection = max(0, m.sortSelection-1)
		case "down":
			m.sortSelection = min(len(suiteListSorts)-1, m.sortSelection+1)
		case "enter":
			m.setListSort(m.sortSelection)
			m.sortOpen = false
		}
		return m, nil
	}
	side := m.focus
	switch key.String() {
	case "esc":
		m.listQuery[side] = ""
		m.listSearching = false
		m.columnSelection[side] = 0
		m.selectSide(side)
	case "enter":
		m.listSearching = false
	case "up":
		m.moveSide(-1)
	case "down":
		m.moveSide(1)
	case "backspace":
		r := []rune(m.listQuery[side])
		if len(r) > 0 {
			m.listQuery[side] = string(r[:len(r)-1])
			m.columnSelection[side] = 0
			m.selectSide(side)
		}
	default:
		if key.Text != "" && key.Mod&(tea.ModCtrl|tea.ModAlt) == 0 && len(m.listQuery[side])+len(key.Text) <= 512 {
			m.listQuery[side] += key.Text
			m.columnSelection[side] = 0
			m.selectSide(side)
		}
	}
	return m, nil
}
