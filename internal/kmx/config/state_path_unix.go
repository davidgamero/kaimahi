//go:build !windows

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/fileaccess"
)

var refuseWindowsDriveMount = fileaccess.RefuseWindowsDriveMount

func stateDirPath() (string, error) {
	var dir string
	if home := strings.TrimSpace(os.Getenv("KMX_HOME")); home != "" {
		dir = home
	} else {
		var err error
		dir, err = os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("cannot locate a config directory (set KMX_HOME): %w", err)
		}
		dir = filepath.Join(dir, "kmx")
	}
	if err := refuseWindowsDriveMount(dir); err != nil {
		return "", err
	}
	return dir, nil
}
