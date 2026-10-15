package workspace

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// CreateExclusive authors into a previously absent directory. Text is validated
// by the same source writer as create. Failed writes leave an explicitly named
// incomplete directory; they never remove concurrent user files.
func CreateExclusive(ctx context.Context, root string, opt CreateOptions, instructions string) error {
	abs, err := safeAbsolute(root)
	if err != nil {
		return err
	}
	parent, err := os.OpenRoot(filepath.Dir(abs))
	if err != nil {
		return err
	}
	defer parent.Close()
	stage, err := os.MkdirTemp(filepath.Dir(abs), ".kmx-author-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	in := filepath.Join(stage, "instructions.md")
	if err := os.WriteFile(in, []byte(instructions), 0600); err != nil {
		return err
	}
	opt.InstructionsFile = in
	if err := ValidateCreateOptions(opt); err != nil {
		return err
	}
	preparedRoot := filepath.Join(stage, "workspace")
	if err := Create(ctx, preparedRoot, opt); err != nil {
		return err
	}
	if _, err := Prepare(ctx, preparedRoot, filepath.Join(stage, "cache")); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := parent.Mkdir(filepath.Base(abs), 0700); err != nil {
		return fmt.Errorf("new workspace directory must not exist: %w", err)
	}
	destination, err := parent.OpenRoot(filepath.Base(abs))
	if err != nil {
		return err
	}
	defer destination.Close()
	source, err := os.OpenRoot(preparedRoot)
	if err != nil {
		return err
	}
	defer source.Close()
	err = fs.WalkDir(source.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if name == "." || name == "workspace.yaml" {
			return nil
		}
		if entry.IsDir() {
			return destination.Mkdir(name, 0700)
		}
		raw, err := source.ReadFile(name)
		if err != nil {
			return err
		}
		f, err := destination.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, err = f.Write(raw)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
	if err != nil {
		return fmt.Errorf("incomplete workspace retained: %w", err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	raw, err := source.ReadFile("workspace.yaml")
	if err != nil {
		return err
	}
	marker, err := destination.OpenFile("workspace.yaml", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = marker.Write(raw)
	closeErr := marker.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}
