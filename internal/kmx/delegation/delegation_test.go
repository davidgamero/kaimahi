// Package delegation holds the test that make and kmx are ONE
// implementation of the journey (D27, condition 1).
//
// The claim is not "the Makefile mentions kmx somewhere". It is that each
// delegating target hands kmx the right work with the right arguments — and
// the only way to know that is to ask make itself, with the same variable
// expansion a developer's invocation gets. `make -n` runs nothing, so this
// stays a unit test: no cluster, no network, no Go toolchain beyond the one
// already running it.
package delegation

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
)

func dryRun(t *testing.T, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}
	cmd := exec.Command("make", append([]string{"-n"}, args...)...)
	cmd.Dir = "../../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make -n %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// Every target the Makefile delegates, and the kmx command it must produce.
func TestMakeTargetsDelegateToKmx(t *testing.T) {
	for _, tc := range []struct {
		target string
		want   string
	}{
		{"up", "bin/kmx up"},
		{"cluster", "bin/kmx up --step cluster"},
		{"ollama", "bin/kmx up --step ollama"},
		{"model", "bin/kmx up --step model"},
		{"kagent", "bin/kmx up --step kagent"},
		{"agent", "bin/kmx up --step agent"},
		{"tools-agent", "bin/kmx up --step tools-agent"},
		{"status", "bin/kmx status"},
		{"down", "bin/kmx down"},
		{"chat", `bin/kmx agent chat hello-world "Hello! Who are you and where are you running?"`},
		// Milestone 2 (D28): the governance half.
		{"plane", "bin/kmx plane --source ."},
		{"plane-image", "bin/kmx plane --step image --source ."},
		{"plane-secrets", "bin/kmx plane --step secrets"},
		{"govern", "bin/kmx govern hello-world --agent hello-world --preset governed-ollama"},
		{"ledger", "bin/kmx ledger hello-world"},
		{"grants", "bin/kmx grants"},
		{"tool-audit", "bin/kmx audit tool hello-tools"},
		{"approval-audit", "bin/kmx audit approval"},
	} {
		t.Run(tc.target, func(t *testing.T) {
			out := dryRun(t, tc.target)
			if !strings.Contains(out, tc.want) {
				t.Errorf("`make %s` does not invoke `%s`:\n%s", tc.target, tc.want, out)
			}
		})
	}
}

// The delegation has to carry the operator's settings, or `KIND_CLUSTER=mine
// make up` would build one cluster and `KIND_CLUSTER=mine kmx up` another.
func TestDelegationPassesTheOperatorsSettings(t *testing.T) {
	out := dryRun(t, "up", "KIND_CLUSTER=mine", "CONTAINER_ENGINE=podman", "MODEL=llama3", "CHAT_PORT=9999")
	for _, want := range []string{
		"KIND_CLUSTER='mine'",
		"KUBE_CTX='kind-mine'",
		"CONTAINER_ENGINE='podman'",
		"MODEL='llama3'",
		"CHAT_PORT='9999'",
		"KAGENT_VERSION='0.9.12'",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("delegation does not pass %s:\n%s", want, out)
		}
	}
}

// A confirmation given to make must not be asked for again by kmx.
func TestConfirmationRidesThrough(t *testing.T) {
	out := dryRun(t, "down", "KIND_CLUSTER=mine", "KAIMAHI_CONFIRM=kind-mine")
	if !strings.Contains(out, "KAIMAHI_CONFIRM='kind-mine'") {
		t.Errorf("KAIMAHI_CONFIRM is not passed to kmx:\n%s", out)
	}
}

// In a checkout there is one kagent binary, not two: make fetches it, kmx is
// told where it is.
func TestChatHandsKmxTheCheckoutsKagentBinary(t *testing.T) {
	out := dryRun(t, "chat")
	if !strings.Contains(out, "KAGENT='bin/kagent'") {
		t.Errorf("chat does not hand kmx bin/kagent:\n%s", out)
	}
}

func TestChatPassesInteractiveSettings(t *testing.T) {
	out := dryRun(t, "chat", "INTERACTIVE=1", "SESSION=session-1", "AGENT=hello-tools")
	for _, want := range []string{"agent chat --interactive", "--session 'session-1'", "hello-tools"} {
		if !strings.Contains(out, want) {
			t.Errorf("interactive chat does not pass %s:\n%s", want, out)
		}
	}
}

func TestChatSessionIsNotInterpolatedIntoShellSource(t *testing.T) {
	out := dryRun(t, "chat", "INTERACTIVE=1", "SESSION=x' ; touch /tmp/pwn ; : '")
	if strings.Contains(out, "touch /tmp/pwn") {
		t.Fatalf("session value was interpolated into shell source:\n%s", out)
	}
	if !strings.Contains(out, `--session "$KMX_CHAT_SESSION"`) {
		t.Fatalf("session is not read from the exported environment:\n%s", out)
	}
}

func TestInteractiveChatDoesNotSendTheDefaultTask(t *testing.T) {
	out := dryRun(t, "chat", "INTERACTIVE=1")
	if strings.Contains(out, config.DefaultTask) {
		t.Fatalf("interactive chat sent the default one-shot task:\n%s", out)
	}
	out = dryRun(t, "chat", "INTERACTIVE=1", "TASK=explicit first turn")
	if !strings.Contains(out, `"explicit first turn"`) {
		t.Fatalf("interactive chat dropped an explicit task:\n%s", out)
	}
}

// The managed path is NOT kmx's (D27: no AKS in milestone 1; D28(4): kind
// only in milestone 2). Its bring-up must still be the Makefile's own
// recipes, and so must its plane and its governance: kmx side-loads a local
// image and applies the manifest unrendered, which on a registry-backed
// cluster would mean ErrImageNeverPull, forever.
func TestTheManagedPathDoesNotDelegate(t *testing.T) {
	out := dryRun(t, "cluster", "TARGET=aks", "AKS_RESOURCE_GROUP=rg", "ACR_NAME=acr")
	if strings.Contains(out, "bin/kmx up") {
		t.Errorf("TARGET=aks must not route cluster bring-up through kmx:\n%s", out)
	}
	if !strings.Contains(out, "aks-up.sh") {
		t.Errorf("TARGET=aks cluster should still run scripts/aks-up.sh:\n%s", out)
	}

	// The recipes themselves, read out of make's own database. `make -n`
	// cannot be used for this: a recipe line containing $(MAKE) is executed
	// even under -n (that is make's recursion rule), and the managed
	// `govern` has one — it would go looking for a real AKS context.
	// Question mode prints the database and runs nothing.
	db := recipes(t, "TARGET=aks", "AKS_RESOURCE_GROUP=rg", "ACR_NAME=acr")
	for target, want := range map[string]string{
		"plane":          "plane-deploy.sh",
		"plane-image":    "az acr build",
		"plane-secrets":  "plane-secrets.sh",
		"govern":         "plane-admin.sh issue",
		"ledger":         "plane-admin.sh ledger",
		"grants":         "plane-admin.sh grants",
		"tool-audit":     "plane-admin.sh tool-audit",
		"approval-audit": "plane-admin.sh approval-audit",
	} {
		recipe, ok := db[target]
		if !ok {
			t.Errorf("TARGET=aks has no %s recipe at all", target)
			continue
		}
		if strings.Contains(recipe, "$(KMX)") || strings.Contains(recipe, "bin/kmx") {
			t.Errorf("TARGET=aks %s must stay on the scripts, not kmx:\n%s", target, recipe)
		}
		if !strings.Contains(recipe, want) {
			t.Errorf("TARGET=aks %s no longer runs %q:\n%s", target, want, recipe)
		}
	}
}

// recipes returns each target's recipe as make itself records it, without
// running anything (`make -qp`: question mode, print database).
func recipes(t *testing.T, args ...string) map[string]string {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}
	cmd := exec.Command("make", append([]string{"-qp"}, args...)...)
	cmd.Dir = "../../.."
	// -q exits 1 when a target is out of date; the database is still
	// printed, so the exit status is not the signal here.
	out, _ := cmd.Output()

	db := map[string]string{}
	target, body := "", []string{}
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "\t"):
			if target != "" {
				body = append(body, line)
			}
		case strings.HasPrefix(line, "#"), line == "":
			// Comments and blanks separate entries but do not end a recipe
			// (make interleaves them), so only a new target line does.
		default:
			if target != "" {
				db[target] = strings.Join(body, "\n")
			}
			target, body = "", nil
			if name, _, ok := strings.Cut(line, ":"); ok && !strings.ContainsAny(name, " =$") {
				target = name
			}
		}
	}
	if target != "" {
		db[target] = strings.Join(body, "\n")
	}
	return db
}

// The plane's manifests and the governed presets are inside the binary, so a
// manifest edit has to RELINK it. Without this the Makefile would happily
// reuse a bin/kmx built before the edit and deploy the previous manifest —
// the same class of staleness the unconditional proxy restart exists for.
func TestEditingAPlaneManifestRebuildsKmx(t *testing.T) {
	for _, asset := range []string{
		"k8s/plane/proxy.yaml",
		"k8s/models/governed-ollama.yaml",
	} {
		if !strings.Contains(makeVariable(t, "KMX_ASSETS"), asset) {
			t.Errorf("KMX_ASSETS does not list %s, so editing it would not relink bin/kmx", asset)
		}
	}
}

// makeVariable asks make what a variable expands to, so the test reads the
// same value a recipe would.
func makeVariable(t *testing.T, name string) string {
	t.Helper()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is not installed")
	}
	cmd := exec.Command("make", "-f", "Makefile", "-f", "/dev/stdin", "print-"+name)
	cmd.Stdin = strings.NewReader("print-%:\n\t@echo $($*)\n")
	cmd.Dir = "../../.."
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("cannot read %s: %v", name, err)
	}
	return string(out)
}
