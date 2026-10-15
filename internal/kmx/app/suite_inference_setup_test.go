package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestSuiteInferenceDetectionUsesKeyNamesAndRefusesMismatch(t *testing.T) {
	a, _, _, _ := extractionFixture(t)
	s := suiteInferenceSetup{Environment: agentTUIEnvironment{Name: "kind-test"}, Namespace: OrkaNamespace, Provider: "foundry-model", Secret: "existing-key", Key: "api-key", Endpoint: "https://model.example/openai/v1", Model: "model-a"}
	provider := false
	marker := "secret\napi-key\n"
	mismatch := false
	a.captureCommand = func(_ context.Context, c *exec.Cmd) error {
		args := strings.Join(c.Args, " ")
		if !strings.Contains(args, " get ") {
			t.Fatal("discovery performed write", args)
		}
		if strings.Contains(args, "get secret") {
			if !strings.Contains(args, "go-template=secret") || strings.Contains(args, "-o json") {
				t.Fatal("Secret value read", args)
			}
			_, err := io.WriteString(c.Stdout, marker)
			return err
		}
		if !provider {
			return nil
		}
		model := s.Model
		if mismatch {
			model = "other"
		}
		return json.NewEncoder(c.Stdout).Encode(map[string]any{"metadata": map[string]string{"name": s.Provider, "uid": "provider-uid"}, "spec": map[string]any{"type": "openai", "baseURL": s.Endpoint, "defaultModel": model, "secretRef": map[string]string{"name": s.Secret, "key": s.Key}}})
	}
	state, err := a.inspectSuiteInference(t.Context(), s)
	if err != nil || !strings.Contains(state, "create Provider only") {
		t.Fatalf("state=%s %v", state, err)
	}
	provider = true
	state, err = a.inspectSuiteInference(t.Context(), s)
	if err != nil || !strings.HasPrefix(state, "Provider matches") {
		t.Fatalf("reuse=%s %v", state, err)
	}
	mismatch = true
	if _, err = a.inspectSuiteInference(t.Context(), s); err == nil {
		t.Fatal("mismatched Provider accepted")
	}
	mismatch = false
	marker = "secret\nother-key\n"
	if _, err = a.inspectSuiteInference(t.Context(), s); err == nil {
		t.Fatal("missing key accepted")
	}
	a.captureCommand = func(context.Context, *exec.Cmd) error { return errors.New("denied") }
	if _, err = a.inspectSuiteInference(t.Context(), s); err == nil {
		t.Fatal("unknown treated as absent")
	}
}

func TestSuiteInferenceSetupRequiresExactConfirmationBeforeReads(t *testing.T) {
	a, _, _, _ := extractionFixture(t)
	a.captureCommand = func(context.Context, *exec.Cmd) error { t.Fatal("read before exact confirmation"); return nil }
	s := suiteInferenceSetup{Environment: agentTUIEnvironment{Name: "remote"}}
	if err := a.setupSuiteInference(t.Context(), s, "different", ""); err == nil {
		t.Fatal("setup without consent")
	}
}

func TestSuiteInferenceSetupCreatesOnlyAbsentResourcesAndKeepsKeyOffArgv(t *testing.T) {
	a, _, _, _ := extractionFixture(t)
	t.Setenv("TEST_FOUNDRY_SETUP_KEY", "fixture-secret-material")
	s := suiteInferenceSetup{Environment: agentTUIEnvironment{Name: "kind-test"}, Namespace: OrkaNamespace, Provider: "test-provider", Secret: "test-key", Key: "api-key", Endpoint: "https://model.example/v1", Model: "model-a"}
	var created []string
	a.captureCommand = func(_ context.Context, c *exec.Cmd) error {
		args := strings.Join(c.Args, " ")
		if strings.Contains(args, "fixture-secret-material") {
			t.Fatal("key leaked into argv")
		}
		if strings.Contains(args, "config view") {
			_, err := io.WriteString(c.Stdout, `{"contexts":[{"name":"kind-test","context":{"cluster":"local"}}],"clusters":[{"name":"local","cluster":{"server":"https://127.0.0.1:6443"}}]}`)
			return err
		}
		if strings.Contains(args, " get ") {
			return nil
		}
		if !strings.Contains(args, " create -f - -o name") {
			t.Fatalf("non-create mutation: %s", args)
		}
		var doc map[string]any
		if err := json.NewDecoder(c.Stdin).Decode(&doc); err != nil {
			t.Fatal(err)
		}
		kind := doc["kind"].(string)
		created = append(created, kind)
		if kind == "Secret" && doc["stringData"].(map[string]any)["api-key"] != "fixture-secret-material" {
			t.Fatal("wrong secret stdin")
		}
		if kind == "Provider" {
			raw, _ := json.Marshal(doc)
			if strings.Contains(string(raw), "fixture-secret-material") {
				t.Fatal("Provider contains credential")
			}
		}
		return nil
	}
	if err := a.setupSuiteInference(t.Context(), s, "kind-test", "TEST_FOUNDRY_SETUP_KEY"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(created, ",") != "Secret,Provider" {
		t.Fatalf("creates=%v", created)
	}
}

func TestSuiteConsoleIndependentChatAndSuiteOwnership(t *testing.T) {
	for _, owned := range []bool{false, true} {
		m := &suiteConsole{input: textinput.New(), entries: []suiteConsoleEntry{{Name: "agent", Context: "kind-test", Live: &agentTUIAgent{Name: "agent", Namespace: OrkaNamespace, SuiteOwned: owned}}}}
		hasChat := false
		for _, action := range m.suiteActions() {
			if action.key == "c" {
				hasChat = true
			}
		}
		if hasChat == owned {
			t.Fatal("wrong chat menu ownership gating")
		}
		m.Update(tea.KeyPressMsg{Code: 'c'})
		if owned {
			if m.form != nil || m.standaloneName != "" {
				t.Fatal("suite-owned live name chat enabled")
			}
			continue
		}
		if m.form == nil || m.form.action != "independent-chat" {
			t.Fatal("standalone chat unavailable")
		}
		if cmd := m.submit(suiteConsoleForm{action: "independent-chat", values: []string{"wrong"}}); cmd != nil {
			t.Fatal("chat without exact context")
		}
		if cmd := m.submit(suiteConsoleForm{action: "independent-chat", values: []string{"kind-test"}}); cmd == nil || m.standaloneName != "agent" {
			t.Fatal("chat not launched")
		}
	}
}

func TestSuiteCreationFieldStylingKeepsPlainTextReadable(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	styled := newSuiteConsoleTheme().property("Model", "model-a")
	if ansi.Strip(styled) != "Model: model-a" || !strings.Contains(styled, "\x1b[") {
		t.Fatal("field/value styling missing")
	}
	t.Setenv("NO_COLOR", "1")
	if got := newSuiteConsoleTheme().property("Model", "model-a"); got != "Model: model-a" {
		t.Fatalf("NO_COLOR=%q", got)
	}
}
