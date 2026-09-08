package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/run"
)

func TestFirstAnswerProfileRequiresEveryExplicitDisable(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want bool
	}{
		{"minimal", `{"kaimahi":{"profile":"first-answer"},"kagent-tools":{"enabled":false},"kmcp":{"enabled":false},"ui":{"replicas":0}}`, true},
		{"full", `{"kagent-tools":{"enabled":true},"kmcp":{"enabled":true},"ui":{"replicas":1}}`, false},
		{"missing is custom", `{"kagent-tools":{"enabled":false},"kmcp":{"enabled":false}}`, false},
		{"unmarked lookalike is custom", `{"kagent-tools":{"enabled":false},"kmcp":{"enabled":false},"ui":{"replicas":0},"providers":{"default":"custom"}}`, false},
		{"marked profile may carry base values", `{"kaimahi":{"profile":"first-answer"},"kagent-tools":{"enabled":false},"kmcp":{"enabled":false},"ui":{"replicas":0},"providers":{"default":"ollama"}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := isFirstAnswerProfile(tc.raw)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("minimal=%v, want %v", got, tc.want)
			}
		})
	}
	if _, err := isFirstAnswerProfile(`{`); err == nil {
		t.Fatal("malformed values were accepted")
	}
}

func TestQuickstartPreservesAnExistingFullKagentRelease(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "commands")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("COMMAND_LOG", log)
	fakeTool(t, dir, "helm", `
printf 'helm %s\n' "$*" >> "$COMMAND_LOG"
case "$1 $2" in
  "list --all") printf '%s\n' '[{"name":"kagent","status":"deployed"}]' ;;
  "get values") printf '%s\n' '{"kagent-tools":{"enabled":true},"kmcp":{"enabled":true},"ui":{"replicas":1}}' ;;
  *) echo "unexpected helm mutation" >&2; exit 9 ;;
esac`)
	fakeTool(t, dir, "kubectl", `printf 'kubectl %s\n' "$*" >> "$COMMAND_LOG"`)
	var errOut bytes.Buffer
	a := &App{
		Cfg: &config.Config{KubeContext: "kind-test"},
		Run: &run.Runner{Stdout: &errOut, Stderr: &errOut},
		Err: &errOut,
	}
	if err := a.stepQuickstartKagent(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "upgrade") {
		t.Fatalf("quickstart mutated a full release:\n%s", text)
	}
	if !strings.Contains(text, "rollout status deployment/kagent-controller --timeout=420s") {
		t.Fatalf("quickstart did not verify the existing controller:\n%s", text)
	}
	if !strings.Contains(errOut.String(), "preserving it") {
		t.Fatalf("preservation was not visible: %s", errOut.String())
	}
}

func TestQuickstartRefusesUnreadableHelmStateBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "commands")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("COMMAND_LOG", log)
	fakeTool(t, dir, "helm", `printf 'helm %s\n' "$*" >> "$COMMAND_LOG"; echo forbidden >&2; exit 1`)
	a := &App{
		Cfg: &config.Config{KubeContext: "kind-test"},
		Run: &run.Runner{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}},
		Err: &bytes.Buffer{},
	}
	err := a.stepQuickstartKagent()
	if err == nil || !strings.Contains(err.Error(), "refusing to apply the quickstart profile") {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, readErr := os.ReadFile(log)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(raw), "upgrade") {
		t.Fatalf("read failure caused a mutation:\n%s", raw)
	}
}

func TestQuickstartRefusesMalformedHelmStateBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "commands")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("COMMAND_LOG", log)
	fakeTool(t, dir, "helm", `printf 'helm %s\n' "$*" >> "$COMMAND_LOG"; printf '{'`)
	a := &App{
		Cfg: &config.Config{KubeContext: "kind-test"},
		Run: &run.Runner{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}},
		Err: &bytes.Buffer{},
	}
	err := a.stepQuickstartKagent()
	if err == nil || !strings.Contains(err.Error(), "cannot decode Helm release state") {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, readErr := os.ReadFile(log)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(raw), "upgrade") {
		t.Fatalf("malformed state caused a mutation:\n%s", raw)
	}
}

func TestQuickstartRefusesUnhealthyCustomRelease(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "commands")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("COMMAND_LOG", log)
	fakeTool(t, dir, "helm", `
printf 'helm %s\n' "$*" >> "$COMMAND_LOG"
case "$1 $2" in
  "list --all") printf '%s\n' '[{"name":"kagent","status":"failed"}]' ;;
  "get values") printf '%s\n' '{"custom":true}' ;;
esac`)
	a := &App{
		Cfg: &config.Config{KubeContext: "kind-test"},
		Run: &run.Runner{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}},
		Err: &bytes.Buffer{},
	}
	err := a.stepQuickstartKagent()
	if err == nil || !strings.Contains(err.Error(), `status "failed"`) {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, readErr := os.ReadFile(log)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(raw), "upgrade") {
		t.Fatalf("unhealthy custom release was changed:\n%s", raw)
	}
}

func TestQuickstartInstallDelegatesReadinessToHelm(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "commands")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("COMMAND_LOG", log)
	fakeTool(t, dir, "helm", `
printf 'helm %s\n' "$*" >> "$COMMAND_LOG"
if [ "$1 $2" = "list --all" ]; then printf '%s\n' '[]'; fi`)
	fakeTool(t, dir, "kubectl", `printf 'kubectl %s\n' "$*" >> "$COMMAND_LOG"`)
	var errOut bytes.Buffer
	a := &App{
		Cfg: &config.Config{KubeContext: "kind-test", KagentVersion: "0.9.12"},
		Run: &run.Runner{Stdout: &errOut, Stderr: &errOut},
		Err: &errOut,
	}
	if err := a.stepQuickstartKagent(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"upgrade --install kagent-crds", "upgrade --install kagent ", "--wait --wait-for-jobs --timeout 420s", "kaimahi.profile=first-answer", "kagent-tools.enabled=false", "kmcp.enabled=false", "ui.replicas=0"} {
		if !strings.Contains(text, want) {
			t.Errorf("install lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "kubectl") || strings.Contains(text, "pods --all") {
		t.Fatalf("install used namespace-wide pod readiness:\n%s", text)
	}
}
