package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Retrieval hints only. Choosing a row never authorizes Lift; the shared planner
// must resolve its exact graph, evidence and destination again.
type suitePublicationChoice struct {
	Reference, Layout, Evidence, Environment, SuiteDigest string
	PublishedAt                                           time.Time
	Image, PlainHTTP                                      bool
}
type suitePublicationPicker struct {
	entry     suiteConsoleEntry
	versions  []suitePublicationChoice
	query     string
	selection int
}

func (p *suitePublicationPicker) items() []chatPickerItem {
	var items []chatPickerItem
	for i, v := range p.versions {
		label := v.Reference
		if i == 0 {
			label = "Latest known · " + label
		}
		detail := v.Layout
		if !v.PublishedAt.IsZero() {
			detail = v.PublishedAt.Local().Format("2006-01-02 15:04:05") + " · " + detail
		}
		if p.entry.Review != nil && v.SuiteDigest != "" && v.SuiteDigest != p.entry.Review.digest() {
			detail += " · differs from current source"
		}
		items = append(items, chatPickerItem{name: label, detail: detail})
	}
	if p.entry.Root != "" {
		items = append(items, chatPickerItem{name: "Publish a new version", detail: "Review and confirm publication first"})
	}
	return append(items, chatPickerItem{name: "Enter reference manually (advanced)", detail: "Use an exact digest published elsewhere"})
}
func (p *suitePublicationPicker) matches() []int {
	return (chatPicker{items: p.items(), query: p.query, searchEnabled: true}).matches()
}

func (m *suiteConsole) openPublications(e suiteConsoleEntry) tea.Cmd {
	m.liftAfterPublish = ""
	p := &suitePublicationPicker{entry: e}
	if e.Root != "" {
		hints, err := readSuiteCatalog()
		if err != nil {
			m.status = err.Error()
			return nil
		}
		root, err := filepath.Abs(e.Root)
		if err != nil {
			m.status = err.Error()
			return nil
		}
		for _, h := range hints {
			if h.Root == root {
				p.versions = append(p.versions, h.Publications...)
				break
			}
		}
		// Root CLI image push also maintains a verified-on-use publication cache.
		if e.Image || e.Review != nil && e.Review.image != nil {
			if pub, err := ReadSuitePublication(root, e.Name); err == nil && pub.Release != "" {
				found := false
				for _, v := range p.versions {
					if v.Reference == pub.Release {
						found = true
					}
				}
				if !found {
					_, digest, _ := strings.Cut(pub.Suite, "@")
					v := suitePublicationChoice{Reference: pub.Release, Image: true, SuiteDigest: digest}
					if path, err := workspaceRecord(root, e.Name, "-images"); err == nil {
						if info, err := os.Stat(path); err == nil {
							v.PublishedAt = info.ModTime()
						}
					}
					p.versions = append(p.versions, v)
				}
			}
		}
	}
	if e.Reference != "" {
		found := false
		for i, v := range p.versions {
			if v.Reference == e.Reference && v.Layout == e.Layout {
				found = true
				p.versions[i].Evidence = e.Evidence
				p.versions[i].Environment = e.EnvironmentFile
			}
		}
		if !found {
			p.versions = append(p.versions, suitePublicationChoice{Reference: e.Reference, Layout: e.Layout, Evidence: e.Evidence, Environment: e.EnvironmentFile, Image: e.Image || e.Review != nil && e.Review.image != nil})
		}
	}
	sort.SliceStable(p.versions, func(i, j int) bool { return p.versions[i].PublishedAt.After(p.versions[j].PublishedAt) })
	if m.refreshCancel != nil {
		m.refreshCancel()
	}
	m.refreshGeneration++
	m.refreshing = false
	if len(p.versions) == 0 && e.Root != "" && e.Review != nil {
		return m.startLiftGuide(e, suitePublicationChoice{Image: e.Review.image != nil})
	}
	m.publications = p
	m.status = "Select a published version; artifact and evidence are revalidated before Lift"
	return nil
}

func (m *suiteConsole) publicationForm(v suitePublicationChoice, manual bool) tea.Cmd {
	var cmd tea.Cmd
	plain := ""
	if v.PlainHTTP {
		plain = "yes"
	}
	if v.Image {
		cmd = m.startForm("image-lift", []string{"Exact image release reference", "Environment bindings file", "Private evidence file", "Source-only promotion? type yes"}, []string{v.Reference, v.Environment, v.Evidence, ""})
	} else {
		cmd = m.startForm("lift", []string{"Exact reference@sha256:digest", "Evidence file", "Named destination (kmx.yaml)", "Local layout (blank for registry)", "Source-only promotion? type yes", "Plain HTTP registry? type yes"}, []string{v.Reference, v.Evidence, "", v.Layout, "", plain})
	}
	if !manual {
		m.form.field = 1
		m.input.SetValue(m.form.values[1])
		m.input.CursorEnd()
		m.status = "Selected: " + v.Reference
	}
	return cmd
}
func (m *suiteConsole) updatePublications(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.publications
	switch key.String() {
	case "esc", "alt+left":
		m.publications = nil
	case "up":
		p.selection = max(0, p.selection-1)
	case "down", "tab":
		p.selection = min(max(0, len(p.matches())-1), p.selection+1)
	case "backspace":
		r := []rune(p.query)
		if len(r) > 0 {
			p.query = string(r[:len(r)-1])
			p.selection = 0
		}
	case "enter":
		matches := p.matches()
		if len(matches) == 0 {
			m.status = "No matching versions"
			return m, nil
		}
		i := matches[min(p.selection, len(matches)-1)]
		m.publications = nil
		if i < len(p.versions) {
			return m, m.startLiftGuide(p.entry, p.versions[i])
		}
		if p.entry.Root != "" && i == len(p.versions) {
			return m, m.startLiftGuide(p.entry, suitePublicationChoice{Image: p.entry.Image || p.entry.Review != nil && p.entry.Review.image != nil})
		}
		return m, m.publicationForm(suitePublicationChoice{Image: p.entry.Image || p.entry.Review != nil && p.entry.Review.image != nil}, true)
	default:
		if key.Text != "" && key.Mod&(tea.ModCtrl|tea.ModAlt) == 0 && len(p.query)+len(key.Text) < 512 {
			p.query += key.Text
			p.selection = 0
		}
	}
	return m, nil
}

func publicationHint(e suiteConsoleEntry, plain bool) *suitePublicationChoice {
	digest := ""
	if e.Review != nil {
		digest = e.Review.digest()
	}
	return &suitePublicationChoice{Reference: e.Reference, Layout: e.Layout, Evidence: e.Evidence, Environment: e.EnvironmentFile, SuiteDigest: digest, Image: e.Image, PlainHTTP: plain, PublishedAt: time.Now().UTC()}
}

func (p *suitePublicationPicker) heading() string {
	return fmt.Sprintf("LIFT · %s · PUBLISHED VERSIONS", p.entry.Name)
}
