package agentsuite

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

const (
	ExecutionHTTPV1       = "http-v1"
	AgentKitMountedConfig = "agentkit-v0-mounted-v1"
)

// ExecutionContract is an experimental declaration of a built image's interface.
// The configuration name selects exact semantics, not arbitrary process overrides.
// It does not select a member runtime or require an image for native execution.
type ExecutionContract struct {
	Configuration string           `json:"configuration,omitempty"`
	Kind          string           `json:"kind"`
	Protocol      string           `json:"protocol"`
	Port          int              `json:"port"`
	HealthPath    string           `json:"healthPath"`
	Inputs        []ExecutionInput `json:"inputs"`
}

type ExecutionInput struct {
	Name        string `json:"name"`
	Environment string `json:"environment"`
	Secret      bool   `json:"secret"`
}

func ValidateExecutionContract(c ExecutionContract) error {
	if c.Configuration != "" && c.Configuration != AgentKitMountedConfig {
		return errors.New("unsupported execution configuration ABI")
	}
	if c.Configuration == AgentKitMountedConfig && (c.Port != 8080 || c.HealthPath != "/healthz") {
		return errors.New("mounted AgentKit configuration requires port 8080 and /healthz")
	}
	if c.Kind != ExecutionHTTPV1 {
		return fmt.Errorf("unsupported execution contract %q", c.Kind)
	}
	if c.Protocol != "openai-chat-v1" {
		return errors.New("HTTP execution requires openai-chat-v1")
	}
	if c.Port < 1024 || c.Port > 65535 {
		return errors.New("execution port must be between 1024 and 65535")
	}
	if !strings.HasPrefix(c.HealthPath, "/") || path.Clean(c.HealthPath) != c.HealthPath || strings.ContainsAny(c.HealthPath, "?#\\\r\n") {
		return errors.New("healthPath must be a normalized HTTP path")
	}
	names, envs := map[string]bool{}, map[string]bool{}
	for _, input := range c.Inputs {
		if !identifierPattern.MatchString(input.Name) || names[input.Name] || !envPattern.MatchString(input.Environment) || envs[input.Environment] || unsafeInjectionEnv(input.Environment) || input.Environment == "PATH" || input.Environment == "HOME" || input.Environment == "AGENTKIT_PROTOCOL" || input.Environment == "AGENTKIT_PORT" {
			return errors.New("execution inputs must be valid, unique and non-injecting")
		}
		names[input.Name], envs[input.Environment] = true, true
	}
	return nil
}
