//go:build !windows

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/fileaccess"
)

func TestAllStatePathsPropagateWindowsDriveRefusalBeforeCreation(t *testing.T) {
	home := filepath.Join(t.TempDir(), "state-must-not-exist")
	t.Setenv("KMX_HOME", home)
	original := refuseWindowsDriveMount
	t.Cleanup(func() { refuseWindowsDriveMount = original })
	want := fmt.Errorf("fixture: %w", fileaccess.ErrWindowsDriveMount)
	checks := 0
	refuseWindowsDriveMount = func(path string) error {
		checks++
		if path != home {
			t.Fatalf("mount guard checked %q, want %q", path, home)
		}
		return want
	}

	paths := []struct {
		name    string
		resolve func() (string, error)
	}{
		{"state", StateDir},
		{"prepared state", PrepareStateDir},
		{"existing state", ExistingStateDir},
		{"cache", CacheDir},
		{"toolchain", ToolchainDir},
		{"lift records", LiftRecordDir},
		{"preview journals", LocalPreviewJournalDir},
		{"agent versions", LocalAgentVersionDir},
		{"prepared journals", PrepareLocalPreviewJournalDir},
		{"selected context read", ReadSelectedContext},
		{"selected context write", func() (string, error) { return WriteSelectedContext("kind-fixture") }},
	}
	for _, path := range paths {
		before := checks
		if got, err := path.resolve(); got != "" || !errors.Is(err, fileaccess.ErrWindowsDriveMount) {
			t.Errorf("%s = %q, %v; want Windows drive refusal", path.name, got, err)
		}
		if checks != before+1 {
			t.Errorf("%s mount checks = %d, want one", path.name, checks-before)
		}
		if _, err := os.Lstat(home); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s created state before refusal: %v", path.name, err)
		}
	}
}
