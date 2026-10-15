package app

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
)

func TestIsolatedOrkaRefusesOccupiedAndLegacyBeforeWrites(t *testing.T) {
	for _, occupied := range []bool{true, false} {
		t.Run(map[bool]string{true: "occupied", false: "legacy"}[occupied], func(t *testing.T) {
			a := New(&config.Config{KubeContext: "kind-test"})
			a.captureCommand = func(_ context.Context, c *exec.Cmd) error {
				args := strings.Join(c.Args, " ")
				if strings.Contains(args, "create") || strings.Contains(args, "apply") || strings.Contains(args, "delete") {
					t.Fatal("write during preflight")
				}
				if strings.Contains(args, "get namespace") {
					if occupied {
						_, err := io.WriteString(c.Stdout, "namespace/occupied")
						return err
					}
					return nil
				}
				_, err := io.WriteString(c.Stdout, `{"items":[{"kind":"Deployment","metadata":{"name":"old","namespace":"orka-system","uid":"old","labels":{"app.kubernetes.io/name":"orka","control-plane":"controller-manager"}},"spec":{}}]}`)
				return err
			}
			if err := a.checkIsolatedOrka(t.Context(), "kmx-isolated", nil); err == nil {
				t.Fatal("unsafe coexistence accepted")
			}
		})
	}
}

func TestIsolatedOrkaSchemaComparison(t *testing.T) {
	want := map[string]any{"group": "core.orka.ai", "versions": []any{map[string]any{"name": "v1alpha1", "served": true}}}
	raw, _ := json.Marshal(want)
	var got map[string]any
	json.Unmarshal(raw, &got)
	got["scope"] = "Namespaced"
	if !orkaSchemaContains(want, got) {
		t.Fatal("API defaults rejected")
	}
	got["versions"].([]any)[0].(map[string]any)["served"] = false
	if orkaSchemaContains(want, got) {
		t.Fatal("incompatible schema accepted")
	}
}

// Explicit opt-in on an operator-owned test cluster. Resources are retained.
func TestLiveIsolatedOrkaInstall(t *testing.T) {
	ctx := os.Getenv("KMX_TEST_ISOLATED_CONTEXT")
	ns := os.Getenv("KMX_TEST_ISOLATED_NAMESPACE")
	if ctx == "" || ns == "" {
		t.Skip("explicit isolated-runtime qualification target required")
	}
	if os.Getenv("KAIMAHI_CONFIRM") != ctx {
		t.Fatal("exact context confirmation required")
	}
	a := New(&config.Config{KubeContext: ctx, ContextSource: config.SourceFlag, Confirm: ctx, ContainerEngine: "docker"})
	if err := a.prepareIsolatedOrka(t.Context(), ns, ctx); err != nil {
		t.Fatal(err)
	}
	r, err := a.readSuiteNativeRuntime(t.Context(), ns)
	if err != nil || r.WatchNamespace != ns {
		t.Fatalf("isolated runtime: %+v %v", r, err)
	}
}
