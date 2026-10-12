package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite/workspace"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/imagelift"
	rt "github.com/kaimahi-agents/kaimahi/internal/kmx/runtime"
)

// PrepareReleaseLift is the deploy-only release entry point shared by CLI/TUI.
// Execution dispatch comes from packaged metadata. Native release support can
// join this boundary without another command or runtime-selector flag.
func (a *App) PrepareReleaseLift(ctx context.Context, reference string, env imagelift.Environment) (*rt.PreparedSuiteDeployment, error) {
	return a.PrepareSuiteLift(ctx, reference, env)
}

// ApplyReleaseLift records a placement without requiring authored workspace
// files. Caller review and retained member plans establish immutable input.
func (a *App) ApplyReleaseLift(ctx context.Context, reference string, env imagelift.Environment, prepared *rt.PreparedSuiteDeployment, reviewed string) (rt.SuiteDeploymentReceipt, error) {
	var empty rt.SuiteDeploymentReceipt
	if prepared == nil || prepared.Summary().PlanDigest != reviewed {
		return empty, errors.New("release plan differs from review")
	}
	if err := env.Validate(); err != nil {
		return empty, err
	}
	summary := prepared.Summary()
	_, digest, pinned := strings.Cut(reference, "@")
	if !pinned || digest != summary.ArtifactDigest {
		return empty, errors.New("release identity differs from reviewed plan")
	}
	if summary.Target.Context != env.Context || summary.Target.ClusterUID != env.ClusterUID || summary.Target.Namespace != env.Namespace || summary.Instance != env.Name {
		return empty, errors.New("release target differs from review")
	}
	fresh, err := a.PrepareReleaseLift(ctx, reference, env)
	if err != nil {
		return empty, err
	}
	if fresh.Summary().PlanDigest != reviewed {
		return empty, errors.New("release inputs changed after review")
	}
	// Clone operator input before writes so the recorded placement cannot drift
	// through a caller-owned map while the retained plan is being applied.
	env = cloneReleaseEnvironment(env)
	state, err := config.StateDir()
	if err != nil {
		return empty, err
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(env.ClusterUID+"\x00"+env.Namespace+"\x00"+env.Name)))
	path := filepath.Join(state, "suite-placements", key+".json")
	record := WorkspaceDeployment{WorkspaceName: summary.Suite, Environment: env, Publication: SuitePublication{Suite: reference, Platform: env.Platform, Images: map[string]string{}}}
	for id, m := range env.Members {
		record.Publication.Images[id] = m.Image
	}
	if err := saveWorkspaceRecord(path, record); err != nil {
		return empty, err
	}
	receipt, deployErr := prepared.Deploy(ctx, func(s rt.SuitePlanSummary) error {
		if s.PlanDigest != reviewed {
			return errors.New("release review changed")
		}
		return nil
	})
	record.Receipt = receipt
	return receipt, errors.Join(deployErr, saveWorkspaceRecord(path, record))
}

func cloneReleaseEnvironment(env imagelift.Environment) imagelift.Environment {
	data, _ := json.Marshal(env)
	var copy imagelift.Environment
	_ = json.Unmarshal(data, &copy)
	return copy
}

// CheckImageExecution requires the explicit packaged execution profile rather
// than inferring image/native execution from caller flags or a workspace path.
func CheckImageExecution(ctx context.Context, root string) error {
	return workspace.ValidateImageDirectory(ctx, root)
}
