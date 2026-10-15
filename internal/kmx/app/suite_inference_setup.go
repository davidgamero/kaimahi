package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/scaffold"
)

// Setup is separate from authoring and from release placement. Only public
// Provider configuration and Secret key names cross the discovery boundary.
type suiteInferenceSetup struct {
	Environment                                       agentTUIEnvironment
	Namespace, Provider, Endpoint, Model, Secret, Key string
}

func (a *App) inspectSuiteInference(ctx context.Context, s suiteInferenceSetup) (string, error) {
	if err := validateSuiteInference(s); err != nil {
		return "", err
	}
	w := s.Environment.app(a).withRunContext(ctx)
	raw, err := w.orkaCapture(ctx, nil, "-n", s.Namespace, "get", consoleProviderKind, s.Provider, "--ignore-not-found=true", "-o", "json")
	if err != nil {
		return "", errors.New("Provider discovery unavailable; check runtime and access")
	}
	if len(strings.TrimSpace(string(raw))) > 0 {
		var p struct {
			Metadata struct {
				Name, UID   string
				Annotations map[string]string
			}
			Spec struct {
				Type, BaseURL, DefaultModel string
				SecretRef                   struct{ Name, Key string }
			}
		}
		if json.Unmarshal(raw, &p) != nil || p.Metadata.Name != s.Provider || p.Metadata.UID == "" {
			return "", errors.New("invalid Provider identity")
		}
		if p.Spec.Type != "openai" || p.Spec.BaseURL != s.Endpoint || p.Spec.DefaultModel != s.Model || p.Spec.SecretRef.Name != s.Secret || p.Spec.SecretRef.Key != s.Key {
			return "", errors.New("existing Provider has different configuration; choose a new name")
		}
		for k := range p.Metadata.Annotations {
			if strings.Contains(k, "suite") {
				return "", errors.New("suite-owned Provider cannot be reused for inference setup")
			}
		}
		marker, err := w.suiteSecretKeyState(ctx, s)
		if err != nil {
			return "", err
		}
		if marker != "present" {
			return "", errors.New("existing Provider Secret/key unavailable; configure credentials separately")
		}
		return "Provider matches; Secret key exists (credential validity not tested)", nil
	}
	marker, err := w.suiteSecretKeyState(ctx, s)
	if err != nil {
		return "", err
	}
	if marker == "present" {
		return "Secret key exists; create Provider only", nil
	}
	return "Secret absent; create Secret and Provider", nil
}

func (a *App) suiteSecretKeyState(ctx context.Context, s suiteInferenceSetup) (string, error) {
	raw, err := a.orkaCapture(ctx, nil, "-n", s.Namespace, "get", "secret", s.Secret, "--ignore-not-found=true", "-o", "go-template=secret\n{{range $k,$v := .data}}{{$k}}\n{{end}}")
	if err != nil {
		return "", errors.New("Secret metadata lookup failed; not absence")
	}
	if len(raw) == 0 {
		return "absent", nil
	}
	keys := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if keys[0] != "secret" {
		return "", errors.New("invalid Secret metadata response")
	}
	for _, k := range keys[1:] {
		if k == s.Key {
			return "present", nil
		}
	}
	return "", errors.New("Secret exists without selected key; refusing replacement")
}

func validateSuiteInference(s suiteInferenceSetup) error {
	for _, name := range []string{s.Provider, s.Secret} {
		if err := scaffold.ValidateName(name); err != nil {
			return err
		}
	}
	if err := scaffold.ValidateNamespace(s.Namespace); err != nil {
		return err
	}
	u, err := url.Parse(s.Endpoint)
	if s.Environment.Name == "" || s.Model == "" || s.Key != "api-key" || err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("explicit context, HTTPS endpoint, model and api-key reference required")
	}
	return nil
}

func (a *App) setupSuiteInference(ctx context.Context, s suiteInferenceSetup, typed, envName string) error {
	if typed != s.Environment.Name {
		return errors.New("exact context confirmation required")
	}
	if err := validateSuiteInference(s); err != nil {
		return err
	}
	w := s.Environment.app(a).withRunContext(ctx)
	if _, err := w.inspectSuiteInference(ctx, s); err != nil {
		return err
	}
	w.Cfg.Confirm = typed
	if err := w.guardOrkaMutation(ctx, CreateOptions{Name: s.Provider, Namespace: s.Namespace}, "connect Foundry inference with create-only Provider/Secret setup"); err != nil {
		return err
	}
	state, err := w.inspectSuiteInference(ctx, s)
	if err != nil {
		return err
	}
	if strings.HasPrefix(state, "Provider matches") {
		return nil
	}
	if strings.HasPrefix(state, "Secret absent") {
		if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(envName) {
			return errors.New("Secret absent; select an environment variable containing the API key")
		}
		key := os.Getenv(envName)
		if strings.TrimSpace(key) == "" {
			return errors.New("selected API-key environment variable is empty")
		}
		secret := map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]string{"name": s.Secret, "namespace": s.Namespace}, "type": "Opaque", "stringData": map[string]string{s.Key: key}}
		body, _ := json.Marshal(secret)
		if _, err := w.orkaCapture(ctx, body, "create", "-f", "-", "-o", "name"); err != nil {
			return errors.New("Secret creation failed or outcome unknown; inspect target before retrying")
		}
	}
	doc := map[string]any{"apiVersion": "core.orka.ai/v1alpha1", "kind": "Provider", "metadata": map[string]string{"name": s.Provider, "namespace": s.Namespace}, "spec": map[string]any{"type": "openai", "baseURL": s.Endpoint, "defaultModel": s.Model, "secretRef": map[string]string{"name": s.Secret, "key": s.Key}}}
	body, _ := json.Marshal(doc)
	if _, err := w.orkaCapture(ctx, body, "create", "-f", "-", "-o", "name"); err != nil {
		return fmt.Errorf("Provider creation failed or outcome unknown; retained Secret may remain")
	}
	return nil
}

func (a *App) discoverSuiteInferenceBinding(ctx context.Context, s suiteInferenceSetup) (suiteInferenceSetup, error) {
	w := s.Environment.app(a)
	raw, err := w.orkaCapture(ctx, nil, "-n", s.Namespace, "get", consoleProviderKind, "-o", "json")
	if err != nil {
		return s, errors.New("Provider discovery unavailable; check installed runtime and access")
	}
	var list objectList[struct {
		Metadata struct {
			Name        string
			Annotations map[string]string
		}
		Spec struct {
			Type, BaseURL, DefaultModel string
			SecretRef                   struct{ Name, Key string }
		}
	}]
	if err := decodeConsoleList(raw, &list); err != nil {
		return s, err
	}
	var matches []suiteInferenceSetup
	for _, p := range list.Items {
		if p.Spec.Type != "openai" || p.Spec.BaseURL != s.Endpoint || p.Spec.DefaultModel != s.Model || p.Spec.SecretRef.Key != "api-key" {
			continue
		}
		reserved := false
		for k := range p.Metadata.Annotations {
			if strings.Contains(k, "suite") {
				reserved = true
			}
		}
		if reserved {
			continue
		}
		candidate := s
		candidate.Provider = p.Metadata.Name
		candidate.Secret = p.Spec.SecretRef.Name
		matches = append(matches, candidate)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	// Multiple matches require explicit selection by name, never arbitrary adoption.
	return s, nil
}
