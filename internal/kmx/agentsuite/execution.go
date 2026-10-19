package agentsuite

import (
	"errors"
	"strings"

	agentsuite "github.com/agent-primitives/suite"
)

const (
	ImageDeploymentLabel     = "org.agentsuite.image-deployment"
	ImageDeploymentMediaType = "application/vnd.agentsuite.image.deployment.v1+json"
	AgentKitConfigPath       = "/run/agentsuite/agent.json"
)

// ImageDeployment is a digest-bound publisher declaration, not a conformant
// sandbox binding, filesystem inventory or signed build provenance.
type ImageDeployment struct {
	Inference     *agentsuite.InferenceRequirements `json:"inference,omitempty"`
	SchemaVersion string                            `json:"schemaVersion"`
	MediaType     string                            `json:"mediaType"`
	// SuiteReference is an optional legacy retrieval hint, never source identity.
	// New builds omit it so copying a suite repository does not change the image.
	SuiteReference     string                       `json:"suiteReference,omitempty"`
	SuiteDigest        string                       `json:"suiteDigest"`
	Agent              string                       `json:"agent"`
	Platform           agentsuite.Platform          `json:"platform"`
	CompositionDigest  string                       `json:"compositionDigest"`
	BuildProfile       string                       `json:"buildProfile"`
	BuildProfileDigest string                       `json:"buildProfileDigest"`
	Execution          agentsuite.ExecutionContract `json:"execution"`
}

func DecodeImageDeployment(data []byte) (*ImageDeployment, error) {
	if len(data) > 16<<10 {
		return nil, errors.New("image declaration exceeds 16 KiB")
	}
	var r ImageDeployment
	if err := agentsuite.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	if r.SchemaVersion != agentsuite.SpecVersion || r.MediaType != ImageDeploymentMediaType || !agentsuite.IsValidDigest(r.SuiteDigest) || !agentsuite.IsValidDigest(r.CompositionDigest) || !agentsuite.IsValidDigest(r.BuildProfileDigest) || !builderIdentifierPattern.MatchString(r.Agent) || !builderIdentifierPattern.MatchString(r.BuildProfile) {
		return nil, errors.New("invalid image deployment identity")
	}
	if r.SuiteReference != "" {
		if !strings.HasSuffix(r.SuiteReference, "@"+r.SuiteDigest) {
			return nil, errors.New("suiteReference must pin suiteDigest")
		}
		if err := agentsuite.ValidateImageReference(r.SuiteReference); err != nil {
			return nil, err
		}
	}
	if err := validateBindingPlatform(r.Platform); err != nil {
		return nil, err
	}
	if err := agentsuite.ValidateExecutionContract(r.Execution); err != nil {
		return nil, err
	}
	if (r.Inference != nil) != (r.Execution.Configuration == agentsuite.AgentKitMountedConfig) {
		return nil, errors.New("mounted configuration requires inference capabilities")
	}
	if r.Inference != nil {
		if err := r.Inference.Validate(); err != nil {
			return nil, err
		}
	}
	return &r, nil
}

func EncodeImageDeployment(r ImageDeployment) (string, error) {
	data, err := agentsuite.Marshal(r)
	if err != nil {
		return "", err
	}
	if _, err := DecodeImageDeployment(data); err != nil {
		return "", err
	}
	return string(data), nil
}
