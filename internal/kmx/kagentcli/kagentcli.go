// Package kagentcli fetches the pinned kagent CLI, checksum-verified.
//
// This is the Makefile's `$(KAGENT)` rule, moved. kmx does not reimplement
// anything kagent's CLI does — `kmx agent chat` is a passthrough to `kagent
// invoke` — so the binary has to be on the machine, and a kmx installed with
// `go install` has no bin/ directory in a checkout to put it in. It goes in
// a cache directory keyed by version and platform instead, so switching
// KAGENT_VERSION can never hand you the previous version's binary.
//
// The release .sha256 files embed a build path, so the digest is compared
// field-wise rather than fed to `sha256sum -c`.
package kagentcli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ReleaseBase is the download root, overridable so the tests can serve a
// throwaway release from a local server instead of the internet.
const ReleaseBase = "https://github.com/kagent-dev/kagent/releases/download"

// Platform returns the os/arch pair kagent names its release assets with.
// The Makefile derives these from `uname`; Go already knows them, and the
// mapping is the same one (`x86_64`→amd64, `aarch64`→arm64) because Go's
// GOARCH values are the normalized names to begin with.
func Platform() (string, string) { return runtime.GOOS, runtime.GOARCH }

// AssetName is the release asset for a platform.
func AssetName(goos, goarch string) string {
	return fmt.Sprintf("kagent-%s-%s", goos, goarch)
}

// ExpectedDigest extracts the digest from a release .sha256 file. The file is
// `<digest>  <path>`, and the path is the KAGENT BUILDER's path, not
// anything on this machine — so only the first field may be trusted, and a
// file that does not start with a 64-character hex digest is refused rather
// than compared loosely.
func ExpectedDigest(shaFile []byte) (string, error) {
	fields := strings.Fields(string(shaFile))
	if len(fields) == 0 {
		return "", fmt.Errorf("kagent CLI checksum file is empty")
	}
	digest := strings.ToLower(fields[0])
	if len(digest) != 64 {
		return "", fmt.Errorf("kagent CLI checksum file does not begin with a sha256 digest")
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", fmt.Errorf("kagent CLI checksum file does not begin with a sha256 digest")
	}
	return digest, nil
}

// Verify fails closed when the downloaded bytes do not match the published
// digest.
func Verify(binary, shaFile []byte) error {
	want, err := ExpectedDigest(shaFile)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(binary)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("kagent CLI checksum mismatch: got %s, want %s", got, want)
	}
	return nil
}

// Options configure a fetch.
type Options struct {
	Version  string
	CacheDir string
	// Base overrides ReleaseBase (tests).
	Base string
	// Existing, when set, is used as-is and nothing is downloaded. This is
	// how the Makefile keeps a checkout on one copy: it passes bin/kagent.
	Existing string
	// Log receives one line when a download actually happens.
	Log io.Writer
}

// Ensure returns the path to a verified kagent binary, downloading it only
// when the cache does not already have that exact version.
func Ensure(opt Options) (string, error) {
	if opt.Existing != "" {
		if info, err := os.Stat(opt.Existing); err == nil && !info.IsDir() {
			return opt.Existing, nil
		}
	}
	goos, goarch := Platform()
	name := fmt.Sprintf("kagent-%s-%s-%s", opt.Version, goos, goarch)
	path := filepath.Join(opt.CacheDir, name)
	// A cache HIT is re-verified against the digest recorded when it was
	// downloaded. "Checksum-verified" has to mean the bytes about to be
	// executed, not the bytes that were fetched some other day: anything with
	// write access to the cache directory could otherwise substitute a binary
	// that then runs on every chat, unchecked. Hashing ~60MB costs
	// milliseconds; a compromised CLI holds the kubeconfig.
	if cached, err := os.ReadFile(path); err == nil {
		if sha, err := os.ReadFile(path + ".sha256"); err == nil && Verify(cached, sha) == nil {
			return path, nil
		}
		// Unverifiable: no record, or it no longer matches. Fetch again
		// rather than run it.
		if opt.Log != nil {
			fmt.Fprintf(opt.Log, "cached kagent CLI at %s does not match its recorded digest — re-fetching\n", path)
		}
		_ = os.Remove(path)
		_ = os.Remove(path + ".sha256")
	}

	base := opt.Base
	if base == "" {
		base = ReleaseBase
	}
	asset := AssetName(goos, goarch)
	url := fmt.Sprintf("%s/v%s/%s", base, opt.Version, asset)
	if opt.Log != nil {
		fmt.Fprintf(opt.Log, "fetching the pinned kagent CLI v%s (%s/%s), checksum-verified\n", opt.Version, goos, goarch)
	}

	binary, err := get(url)
	if err != nil {
		return "", err
	}
	shaFile, err := get(url + ".sha256")
	if err != nil {
		return "", err
	}
	if err := Verify(binary, shaFile); err != nil {
		// Nothing is written on a mismatch: a rejected binary must not be
		// left behind where the next run's cache check would find it.
		return "", err
	}

	if err := os.MkdirAll(opt.CacheDir, 0o755); err != nil {
		return "", err
	}
	// Write to a temporary name and rename, so a killed download can never
	// leave a truncated binary in the cache under the real name.
	tmp, err := os.CreateTemp(opt.CacheDir, name+".partial-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(binary); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	// Record the published digest beside the binary so the next run can
	// re-verify what it is about to execute without going back to the
	// network. Written after the rename: a digest with no binary is
	// harmless, a binary with no digest is simply re-fetched.
	if err := os.WriteFile(path+".sha256", shaFile, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func get(url string) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("cannot download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cannot download %s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
