//go:build linux

package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/fileaccess"
)

func TestStateDirRefusesLiveWindowsDriveBeforeCreation(t *testing.T) {
	var drive string
	for _, candidate := range []string{"/mnt/c", "/mnt/d"} {
		if errors.Is(fileaccess.RefuseWindowsDriveMount(candidate), fileaccess.ErrWindowsDriveMount) {
			drive = candidate
			break
		}
	}
	if drive == "" {
		t.Skip("no Windows drive mount on this host; live state refusal not exercised")
	}
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(drive, "kmx-state-guard-"+hex.EncodeToString(nonce))
	t.Setenv("KMX_HOME", home)
	if _, err := PrepareStateDir(); !errors.Is(err, fileaccess.ErrWindowsDriveMount) {
		t.Fatalf("PrepareStateDir = %v, want Windows drive refusal", err)
	}
	if _, err := os.Lstat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state guard created %s: %v", home, err)
	}
}
