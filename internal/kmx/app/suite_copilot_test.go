package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"charm.land/bubbles/v2/textinput"
)

func TestSuiteCopilotCreateReloadRunAndRefusals(t *testing.T) {
	t.Setenv("KMX_HOME", t.TempDir())
	a, _, _, _ := extractionFixture(t)
	remote := false
	a.captureCommand = func(_ context.Context, c *exec.Cmd) error {
		if !strings.Contains(strings.Join(c.Args, " "), "config view") {
			t.Fatalf("unexpected cluster operation: %v", c.Args)
		}
		server := "https://127.0.0.1:6443"
		if remote {
			server = "https://aks.example"
		}
		_, err := fmt.Fprintf(c.Stdout, `{"contexts":[{"name":"kind-test","context":{"cluster":"kind-test"}}],"clusters":[{"name":"kind-test","cluster":{"server":%q}}]}`, server)
		return err
	}
	dir := t.TempDir()
	args := filepath.Join(dir, "calls")
	t.Setenv("COPILOT_TEST_CALLS", args)
	body := `{"jsonrpc":"2.0","id":1,"result":{"models":[{"id":"model-a","capabilities":{"type":"chat"}}]}}`
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	fakeTool(t, dir, "copilot", `if [ "$1" = "--server" ]; then IFS= read -r header; printf '%s' `+shellArg(frame)+`; else printf '%s\n' "$@" >> "$COPILOT_TEST_CALLS"; printf '%s\n' '{"type":"assistant.message","data":{"content":"host answer"}}'; fi`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	env := agentTUIEnvironment{Name: "kind-test", Local: true}
	choices, err := a.suiteCopilotModels(t.Context(), env)
	if err != nil || len(choices) != 1 {
		t.Fatalf("models: %v %v", choices, err)
	}
	var workers sync.WaitGroup
	m := &suiteConsole{a: a, ctx: t.Context(), workers: &workers, input: textinput.New(), events: make(chan suiteConsoleProgress, 256)}
	root := filepath.Join(t.TempDir(), "copilot-source")
	consoleFinish(t, m, m.submit(suiteConsoleForm{action: "create", execution: "copilot", copilot: &suiteCopilotBinding{Environment: env}, values: []string{root, "helper", "model-a", "Answer briefly."}}))
	hints, err := readSuiteCatalog()
	if err != nil || len(hints) != 1 {
		t.Fatalf("hints %v %v", hints, err)
	}
	e, err := loadCatalogWorkspace(t.Context(), a, hints[0])
	if err != nil || e.Copilot == nil {
		t.Fatalf("reload %v", err)
	}
	answer, err := a.runSuiteCopilot(t.Context(), e, "Hello")
	if err != nil || answer != "host answer" {
		t.Fatalf("answer %q %v", answer, err)
	}
	raw, _ := os.ReadFile(args)
	if !strings.Contains(string(raw), "--model\nmodel-a") || !strings.Contains(string(raw), "Answer briefly.") {
		t.Fatal("authored model/instructions missing")
	}
	remote = true
	if _, err := a.runSuiteCopilot(t.Context(), e, "refuse"); err == nil {
		t.Fatal("remote context accepted")
	}
	remote = false
	if err := os.WriteFile(filepath.Join(root, "agents/helper/instructions.md"), []byte("Changed instructions"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.runSuiteCopilot(t.Context(), e, "refuse"); err == nil {
		t.Fatal("source drift accepted")
	}
	after, _ := os.ReadFile(args)
	if string(after) != string(raw) {
		t.Fatal("refusal sent another prompt")
	}
	workers.Wait()
}
