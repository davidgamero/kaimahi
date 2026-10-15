package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
)

// Catalog entries are private retrieval hints. They never authorize execution,
// placement reuse, passing evaluation or source/cluster identity.
type suiteCatalogHint struct {
	Root, Evidence, Reference, Layout, EnvironmentFile string
	Copilot                                            *suiteCopilotBinding     `json:",omitempty"`
	Publications                                       []suitePublicationChoice `json:",omitempty"`
}

func suiteCatalogPath() (string, error) {
	dir, err := config.StateDir()
	return filepath.Join(dir, "suite-console.json"), err
}
func readSuiteCatalog() ([]suiteCatalogHint, error) {
	path, err := suiteCatalogPath()
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(path)
	if resolved, e := filepath.EvalSymlinks(parent); e == nil && resolved != parent {
		return nil, errors.New("suite catalog parent must not contain links")
	}
	if _, err = os.Lstat(path); os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("suite catalog must be a private regular file")
	}
	raw, err := readRunPromptFile(path)
	if err != nil {
		return nil, err
	}
	var rows []suiteCatalogHint
	if json.Unmarshal([]byte(raw), &rows) != nil || len(rows) > 128 {
		return nil, errors.New("invalid private suite catalog")
	}
	for _, row := range rows {
		if len(row.Publications) > 32 {
			return nil, errors.New("publication history exceeds 32 versions")
		}
	}
	return rows, nil
}
func rememberSuiteConsoleEntry(e suiteConsoleEntry) error {
	if e.Root == "" {
		return nil
	}
	root, err := filepath.Abs(e.Root)
	if err != nil {
		return err
	}
	rows, err := readSuiteCatalog()
	if err != nil {
		return err
	}
	h := suiteCatalogHint{Root: root, Evidence: e.Evidence, Reference: e.Reference, Layout: e.Layout, EnvironmentFile: e.EnvironmentFile, Copilot: e.Copilot}
	found := false
	for i, row := range rows {
		if row.Root == root {
			h.Publications = append([]suitePublicationChoice(nil), row.Publications...)
			rows[i] = h
			found = true
			break
		}
	}
	if !found {
		if len(rows) >= 128 {
			return errors.New("suite catalog full")
		}
		rows = append(rows, h)
	}
	if e.Published != nil {
		for i := range rows {
			if rows[i].Root == root {
				p := *e.Published
				history := []suitePublicationChoice{p}
				for _, old := range rows[i].Publications {
					if old.Reference != p.Reference || old.Layout != p.Layout {
						history = append(history, old)
					}
				}
				rows[i].Publications = history[:min(len(history), 32)]
				break
			}
		}
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	path, err := suiteCatalogPath()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if resolved, e := filepath.EvalSymlinks(filepath.Dir(path)); e != nil || resolved != filepath.Dir(path) {
		return errors.New("suite catalog parent must not contain links")
	}
	return writePrivateAgentFile(path, raw)
}

func loadCatalogWorkspace(ctx context.Context, a *App, h suiteCatalogHint) (suiteConsoleEntry, error) {
	r, err := a.reviewSuiteWorkspace(ctx, h.Root)
	if err != nil {
		return suiteConsoleEntry{Name: filepath.Base(h.Root), Root: h.Root, Origin: "workspace unavailable"}, err
	}
	return suiteConsoleEntry{Name: r.name(), Origin: "workspace", Root: r.root, Review: r, Evidence: h.Evidence, Reference: h.Reference, Layout: h.Layout, EnvironmentFile: h.EnvironmentFile, Image: r.image != nil, Copilot: h.Copilot}, nil
}
