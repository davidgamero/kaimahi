package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/scaffold"
)

// Host sources store routing metadata only; Azure/Copilot retain their own login.
type consoleInferenceSource struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Model     string `json:"model"`
	Endpoint  string `json:"endpoint,omitempty"`
	Tenant    string `json:"tenant,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Secret    string `json:"secret,omitempty"`
	SecretKey string `json:"secretKey,omitempty"`
}

type consoleInferenceSnapshot struct {
	Version, Server string
	Model           map[string]any
	Sources         []consoleInferenceSource
}

func consoleInferencePath(env agentTUIEnvironment, agent agentTUIAgent) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	id := fmt.Sprintf("%x", sha256.Sum256([]byte(env.Name+"\x00"+agent.key())))
	return filepath.Join(dir, "kmx", "console-inference", id+".json"), nil
}

func loadConsoleInference(env agentTUIEnvironment, agent agentTUIAgent) (*consoleInferenceSource, error) {
	path, err := consoleInferencePath(env, agent)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var source consoleInferenceSource
	if err = json.Unmarshal(raw, &source); err != nil {
		return nil, fmt.Errorf("invalid saved inference source")
	}
	if source.Kind != "foundry" && source.Kind != "copilot" {
		return nil, fmt.Errorf("invalid saved host inference kind")
	}
	if err = source.validate(agent.Runtime); err != nil {
		return nil, err
	}
	return &source, nil
}

func saveConsoleInference(env agentTUIEnvironment, agent agentTUIAgent, source *consoleInferenceSource) error {
	path, err := consoleInferencePath(env, agent)
	if err != nil {
		return err
	}
	if source == nil {
		err = os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err = source.validate(agent.Runtime); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(source, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateAgentFile(path, raw)
}

func (s consoleInferenceSource) validate(runtime string) error {
	if err := refuseWizardCredentials(s.Name, s.Model, s.Endpoint, s.Tenant, s.Provider, s.Secret, s.SecretKey); err != nil {
		return err
	}
	if strings.TrimSpace(s.Model) == "" {
		return fmt.Errorf("model or deployment is required")
	}
	switch s.Kind {
	case "foundry":
		if runtime != "orka" {
			return fmt.Errorf("Foundry host inference requires a native Orka agent")
		}
		return (foundryChatConfig{Endpoint: s.Endpoint, Deployment: s.Model, Tenant: s.Tenant}).validate()
	case "copilot":
		if runtime != "orka" {
			return fmt.Errorf("Copilot host inference requires a native Orka agent")
		}
		return nil
	case "ollama", "apikey":
		if runtime != "orka" && runtime != "kagent" {
			return fmt.Errorf("unsupported runtime")
		}
		if err := scaffold.ValidateObjectName(s.Name); err != nil {
			return err
		}
		u, err := url.Parse(s.Endpoint)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("use an HTTP(S) endpoint without credentials, query or fragment")
		}
		if s.Kind == "apikey" {
			if err := scaffold.ValidateObjectName(s.Secret); err != nil {
				return fmt.Errorf("existing Secret name is required: %w", err)
			}
			if s.SecretKey == "" || strings.ContainsAny(s.SecretKey, " /\r\n") {
				return fmt.Errorf("Secret key name is required")
			}
			if s.Provider != "openai" && s.Provider != "anthropic" {
				return fmt.Errorf("provider must be openai or anthropic")
			}
			if runtime == "kagent" && s.Provider != "openai" {
				return fmt.Errorf("kagent connector creation currently supports OpenAI-compatible endpoints")
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown inference source")
	}
}

func (a *App) consoleLoadInference(ctx context.Context, env agentTUIEnvironment, agent agentTUIAgent) (consoleInferenceSnapshot, error) {
	var snapshot consoleInferenceSnapshot
	server, err := a.consoleCreateTarget(ctx, env)
	if err != nil {
		return snapshot, err
	}
	snapshot.Server = server
	worker := env.app(a)
	kind, configs := consoleInferenceKinds(agent.Runtime)
	raw, err := worker.orkaCapture(ctx, nil, "-n", agent.Namespace, "get", kind, agent.Name, "-o", "json")
	if err != nil {
		return snapshot, err
	}
	var object struct {
		Metadata struct{ ResourceVersion string }
		Spec     struct{ Model map[string]any }
	}
	if err = json.Unmarshal(raw, &object); err != nil {
		return snapshot, err
	}
	if object.Metadata.ResourceVersion == "" {
		return snapshot, fmt.Errorf("Agent has no resourceVersion")
	}
	snapshot.Version, snapshot.Model = object.Metadata.ResourceVersion, object.Spec.Model
	raw, err = worker.orkaCapture(ctx, nil, "-n", agent.Namespace, "get", configs, "-o", "json")
	if err != nil {
		return snapshot, err
	}
	var list objectList[struct {
		Metadata struct{ Name string }
		Spec     struct{ Type, Provider, DefaultModel, Model, BaseURL string }
	}]
	if err = json.Unmarshal(raw, &list); err != nil {
		return snapshot, err
	}
	for _, c := range list.Items {
		snapshot.Sources = append(snapshot.Sources, consoleInferenceSource{Kind: "cluster", Name: c.Metadata.Name, Model: valueOr(c.Spec.DefaultModel, c.Spec.Model), Provider: valueOr(c.Spec.Type, c.Spec.Provider), Endpoint: c.Spec.BaseURL})
	}
	saved, err := loadConsoleInference(env, agent)
	if err != nil {
		return snapshot, err
	}
	if saved != nil {
		snapshot.Sources = append(snapshot.Sources, *saved)
	}
	return snapshot, nil
}

func consoleInferenceKinds(runtime string) (string, string) {
	if runtime == "kagent" {
		return "agents.kagent.dev", "modelconfigs.kagent.dev"
	}
	return "agents.core.orka.ai", "providers.core.orka.ai"
}

func (a *App) consoleSaveInference(ctx context.Context, env agentTUIEnvironment, agent agentTUIAgent, snapshot consoleInferenceSnapshot, source consoleInferenceSource, model string) error {
	if agent.External {
		return fmt.Errorf("inference editing requires an AI agent")
	}
	if err := refuseWizardCredentials(model); err != nil {
		return err
	}
	if source.Kind != "cluster" {
		if err := source.validate(agent.Runtime); err != nil {
			return err
		}
	}
	if source.Kind == "foundry" || source.Kind == "copilot" {
		if source.Kind == "foundry" {
			client, err := newFoundryChatClient(foundryChatConfig{Endpoint: source.Endpoint, Deployment: source.Model, Tenant: source.Tenant})
			if err != nil {
				return err
			}
			defer client.http.CloseIdleConnections()
			if _, err = client.bearer(ctx); err != nil {
				return err
			}
		} else {
			path := detectCopilotCLI()
			if path == "" {
				return fmt.Errorf("install Copilot CLI, then sign in with copilot login")
			}
			if copilotLoginStatus(ctx, path) != "logged in" {
				return fmt.Errorf("Copilot is not signed in; run copilot login, then retry")
			}
			models, err := copilotModels(ctx, path)
			if err != nil {
				return err
			}
			found := false
			for _, m := range models {
				if m.Model == source.Model {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("model is not available through the signed-in Copilot account")
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return saveConsoleInference(env, agent, &source)
	}
	server, err := a.consoleCreateTarget(ctx, env)
	if err != nil {
		return err
	}
	if server != snapshot.Server {
		return fmt.Errorf("environment changed; reopen inference to review the target")
	}
	worker := env.app(a)
	kind, _ := consoleInferenceKinds(agent.Runtime)
	currentRaw, err := worker.orkaCapture(ctx, nil, "-n", agent.Namespace, "get", kind, agent.Name, "-o", "json")
	if err != nil {
		return err
	}
	var current struct {
		Metadata struct{ ResourceVersion string }
	}
	if err = json.Unmarshal(currentRaw, &current); err != nil {
		return err
	}
	if snapshot.Version == "" || current.Metadata.ResourceVersion != snapshot.Version {
		return fmt.Errorf("agent changed; reopen inference before saving")
	}
	if source.Kind != "cluster" {
		if err := worker.consoleCreateConnector(ctx, agent, source); err != nil {
			return err
		}
		model = "" // New connector's default model is the chosen model.
	}
	patch, err := consoleInferencePatch(agent.Runtime, snapshot.Version, source.Name, agent.Namespace, model, snapshot.Model)
	if err != nil {
		return err
	}
	raw, err := worker.orkaCapture(ctx, nil, "-n", agent.Namespace, "patch", kind, agent.Name, "--type=json", "-p", string(patch), "-o", "json")
	if err != nil {
		return fmt.Errorf("could not update agent; any new connector remains available: %w", err)
	}
	if err = saveConsoleInference(env, agent, nil); err != nil {
		return fmt.Errorf("agent updated, but clearing host override failed: %w", err)
	}
	if agent.Runtime == "orka" {
		var updated orkaObject
		if err = json.Unmarshal(raw, &updated); err != nil {
			return err
		}
		if updated.Metadata.UID == "" {
			return fmt.Errorf("updated Agent returned no identity")
		}
		waitCtx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		return worker.waitOrkaReady(waitCtx, agent.Namespace, orkaIdentity{Kind: "Agent", Name: agent.Name, UID: updated.Metadata.UID, Generation: updated.Metadata.Generation})
	}
	return nil
}

func (a *App) consoleCreateConnector(ctx context.Context, agent agentTUIAgent, s consoleInferenceSource) error {
	if err := s.validate(agent.Runtime); err != nil {
		return err
	}
	if agent.Runtime == "kagent" {
		spec := map[string]any{"provider": "OpenAI", "model": s.Model, "apiKeySecret": s.Secret, "apiKeySecretKey": s.SecretKey, "openAI": map[string]any{"baseUrl": s.Endpoint}}
		if s.Kind == "ollama" {
			spec = map[string]any{"provider": "Ollama", "model": s.Model, "ollama": map[string]any{"host": strings.TrimSuffix(s.Endpoint, "/v1")}}
		}
		body, _ := json.Marshal(map[string]any{"apiVersion": "kagent.dev/v1alpha2", "kind": "ModelConfig", "metadata": map[string]string{"name": s.Name, "namespace": agent.Namespace}, "spec": spec})
		_, err := a.orkaCapture(ctx, body, "-n", agent.Namespace, "create", "--validate=strict", "-f", "-", "-o", "json")
		return err
	}
	secret, key, provider, endpoint := s.Secret, s.SecretKey, s.Provider, s.Endpoint
	if s.Kind == "ollama" {
		secret = s.Name + "-keyless"
		key = "api-key"
		provider = "openai"
		endpoint = strings.TrimRight(endpoint, "/")
		if !strings.HasSuffix(endpoint, "/v1") {
			endpoint += "/v1"
		}
	}
	bundle, err := createOrkaBundle(CreateOptions{Name: s.Name, Namespace: agent.Namespace, ProviderType: provider, Model: s.Model, BaseURL: endpoint, Secret: secret, SecretKey: key})
	if err != nil {
		return err
	}
	if err = a.orkaAbsent(ctx, agent.Namespace, bundle.Provider); err != nil {
		return err
	}
	if s.Kind == "ollama" {
		// A separate, create-only dummy Secret satisfies the Orka Provider schema;
		// it contains no credential and never overwrites a user's Secret.
		body, _ := json.Marshal(map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]string{"name": secret, "namespace": agent.Namespace}, "stringData": map[string]string{"api-key": "not-used-by-this-endpoint"}})
		if _, err = a.orkaCapture(ctx, body, "-n", agent.Namespace, "create", "-f", "-", "-o", "name"); err != nil {
			return fmt.Errorf("keyless connector Secret could not be created: %w", err)
		}
	}
	id, err := a.createOrkaObject(ctx, agent.Namespace, bundle.Provider)
	if err != nil {
		return err
	}
	waitCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	return a.waitOrkaReady(waitCtx, agent.Namespace, id)
}

func consoleInferencePatch(runtime, version, configuration, namespace, model string, currentModel map[string]any) ([]byte, error) {
	if version == "" || configuration == "" {
		return nil, fmt.Errorf("inference update requires an Agent version and configuration")
	}
	patch := []map[string]any{{"op": "test", "path": "/metadata/resourceVersion", "value": version}}
	switch runtime {
	case "orka":
		updated := map[string]any{}
		for k, v := range currentModel {
			updated[k] = v
		}
		if model != "" {
			updated["name"] = model
		} else {
			delete(updated, "name")
		}
		patch = append(patch, map[string]any{"op": "add", "path": "/spec/providerRef", "value": map[string]string{"name": configuration, "namespace": namespace}}, map[string]any{"op": "add", "path": "/spec/model", "value": updated})
	case "kagent":
		patch = append(patch, map[string]any{"op": "add", "path": "/spec/declarative/modelConfig", "value": configuration})
	default:
		return nil, fmt.Errorf("unsupported inference runtime %q", runtime)
	}
	return json.Marshal(patch)
}
