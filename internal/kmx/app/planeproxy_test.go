package app

import (
	"strings"
	"testing"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/planebuild"
)

// The exact stderr from the failure this retry exists for: main's post-merge
// job on f9914d4, twice, before the same command succeeded unchanged.
const proxyRaceErr = `go: github.com/kaimahi-agents/kaimahi/plane/cmd/kaimahi-proxy@v0.0.0-20260903053920-f9914d4ce40c: ` +
	`module github.com/kaimahi-agents/kaimahi@v0.0.0-20260903053920-f9914d4ce40c found, ` +
	`but does not contain package github.com/kaimahi-agents/kaimahi/plane/cmd/kaimahi-proxy`

func TestProxyRaceIsRecognised(t *testing.T) {
	if !planeNotOnProxyYet.MatchString(proxyRaceErr) {
		t.Error("this failure must trigger the explicit module resolve")
	}
}

// The priming step must name the module that actually provides the binary,
// not the package and not the repo root — asking for either is what fails.
func TestNestedModuleIsThePlanesOwnModule(t *testing.T) {
	if planebuild.NestedModule != "github.com/kaimahi-agents/kaimahi/plane" {
		t.Errorf("nested module is %q", planebuild.NestedModule)
	}
	if !strings.HasPrefix(planebuild.ModulePath, planebuild.NestedModule+"/") {
		t.Errorf("%q must live inside %q", planebuild.ModulePath, planebuild.NestedModule)
	}
	if planebuild.NestedModule == "github.com/kaimahi-agents/kaimahi" {
		t.Error("the root module is exactly what Go already falls back to")
	}
}

func TestRealBuildFailuresAreNotRetried(t *testing.T) {
	// Waiting a minute to repeat a compile error helps nobody, and hiding a
	// genuine missing package behind five retries would be worse.
	for name, out := range map[string]string{
		"compile error":       "plane/internal/meter/meter.go:12:2: undefined: foo",
		"another module":      `module example.com/other@v1.2.3 found, but does not contain package example.com/other/cmd/thing`,
		"unknown revision":    "go: github.com/kaimahi-agents/kaimahi/plane@abc123: unknown revision abc123",
		"network unreachable": "go: module lookup disabled by GOFLAGS=-mod=vendor",
		"empty":               "",
	} {
		if planeNotOnProxyYet.MatchString(out) {
			t.Errorf("%s must not trigger the resolve path: %q", name, out)
		}
	}
}
