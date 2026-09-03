package kagentcli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// The published .sha256 file is `<digest>  <builder's path>`. Only the first
// field is ours to trust.
func TestExpectedDigestTakesOnlyTheFirstField(t *testing.T) {
	body := []byte("hello")
	line := digestOf(body) + "  /home/runner/work/kagent/kagent/dist/kagent-linux-amd64\n"
	got, err := ExpectedDigest([]byte(line))
	if err != nil {
		t.Fatal(err)
	}
	if got != digestOf(body) {
		t.Errorf("digest %q, want %q", got, digestOf(body))
	}
}

func TestMalformedChecksumFilesAreRefused(t *testing.T) {
	for _, bad := range []string{
		"",
		"   \n",
		"not-a-digest  kagent-linux-amd64\n",
		// A truncated digest must not be accepted as a prefix match.
		digestOf([]byte("hello"))[:32] + "  kagent-linux-amd64\n",
		// 64 characters, but not hex.
		strings.Repeat("z", 64) + "  kagent-linux-amd64\n",
	} {
		if _, err := ExpectedDigest([]byte(bad)); err == nil {
			t.Errorf("checksum file %q must be refused", bad)
		}
	}
}

func TestVerifyFailsClosedOnAMismatch(t *testing.T) {
	good := []byte("the real binary")
	sha := []byte(digestOf(good) + "  kagent-linux-amd64\n")
	if err := Verify(good, sha); err != nil {
		t.Fatalf("matching bytes must verify: %v", err)
	}
	if err := Verify([]byte("a substituted binary"), sha); err == nil {
		t.Fatal("substituted bytes must be refused")
	}
}

// The whole fetch path, against a throwaway "release" server: the binary
// lands in the cache, is executable, and a second call does not re-download.
func TestEnsureDownloadsVerifiesAndCaches(t *testing.T) {
	binary := []byte("#!/bin/sh\necho kagent\n")
	goos, goarch := Platform()
	asset := AssetName(goos, goarch)
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v9.9.9/" + asset:
			hits++
			w.Write(binary)
		case "/v9.9.9/" + asset + ".sha256":
			fmt.Fprintf(w, "%s  dist/%s\n", digestOf(binary), asset)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cache := t.TempDir()
	opt := Options{Version: "9.9.9", CacheDir: cache, Base: srv.URL}
	path, err := Ensure(opt)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("cached binary is not executable: %v", info.Mode())
	}
	// The cache key carries the version, so a version bump cannot be served
	// the previous binary.
	if !strings.Contains(filepath.Base(path), "9.9.9") {
		t.Errorf("cache name %q does not carry the version", filepath.Base(path))
	}
	if _, err := Ensure(opt); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Errorf("binary downloaded %d times, want 1 (the second call must hit the cache)", hits)
	}

	// A cache hit is re-verified, so a substituted binary is refused and
	// re-fetched rather than executed. "Checksum-verified" has to mean the
	// bytes about to run, not the bytes that were fetched some other day.
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho pwned\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(opt); err != nil {
		t.Fatalf("a tampered cache entry should be replaced, not fatal: %v", err)
	}
	if hits != 2 {
		t.Errorf("a tampered cache entry was NOT re-fetched (downloads: %d)", hits)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(binary) {
		t.Errorf("the cache still holds the substituted binary: %q", got)
	}

	// A binary with no recorded digest cannot be verified, so it is not run.
	if err := os.Remove(path + ".sha256"); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(opt); err != nil {
		t.Fatal(err)
	}
	if hits != 3 {
		t.Errorf("an unverifiable cache entry was NOT re-fetched (downloads: %d)", hits)
	}
}

// A mismatch must leave NOTHING behind: a rejected binary in the cache would
// be served, unverified, by the next run.
func TestEnsureWritesNothingWhenTheChecksumFails(t *testing.T) {
	goos, goarch := Platform()
	asset := AssetName(goos, goarch)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			fmt.Fprintf(w, "%s  dist/%s\n", digestOf([]byte("what was published")), asset)
			return
		}
		w.Write([]byte("what was served"))
	}))
	defer srv.Close()

	cache := t.TempDir()
	if _, err := Ensure(Options{Version: "9.9.9", CacheDir: cache, Base: srv.URL}); err == nil {
		t.Fatal("a checksum mismatch must fail the fetch")
	}
	entries, err := os.ReadDir(cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("cache is not empty after a rejected download: %v", entries)
	}
}

// An HTTP failure is a failure, not an empty binary.
func TestEnsureRefusesANonOKResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no such release", http.StatusNotFound)
	}))
	defer srv.Close()
	if _, err := Ensure(Options{Version: "0.0.0", CacheDir: t.TempDir(), Base: srv.URL}); err == nil {
		t.Fatal("a 404 must fail the fetch")
	}
}

// The Makefile hands kmx bin/kagent so a checkout keeps one copy; an
// existing binary is used as-is and nothing is downloaded.
func TestExistingBinaryShortCircuits(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "kagent")
	if err := os.WriteFile(existing, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	path, err := Ensure(Options{Version: "9.9.9", CacheDir: t.TempDir(), Existing: existing, Base: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	if path != existing {
		t.Errorf("got %q, want the existing binary %q", path, existing)
	}
}
