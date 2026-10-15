package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	suiteoras "github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite/oras"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite/workspace"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/scaffold"
)

// Runtime readiness is a separate prerequisite report, never evaluation or a
// placement plan. No admission, Helm invocation or target mutation occurs here.
type SuiteRuntimeReport struct {
	SuiteDigest string `json:"suiteDigest"`
	Runtime     string `json:"runtime"`
	Context     string `json:"context"`
	Namespace   string `json:"namespace"`
	Required    string `json:"required"`
	State       string `json:"state"`
	Detail      string `json:"detail"`
	Next        string `json:"next,omitempty"`
}

func (a *App) CheckSuiteRuntime(ctx context.Context, opt ReleaseLiftOptions) (SuiteRuntimeReport, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var report SuiteRuntimeReport
	if opt.ToContext == "" {
		return report, errors.New("runtime check requires an explicit destination")
	}
	if opt.Environment != "" {
		x, err := a.prepareImageCandidate(ctx, opt)
		if err != nil {
			return report, err
		}
		return SuiteRuntimeReport{SuiteDigest: x.suite.Digest, Runtime: "image-http", Context: opt.ToContext, Namespace: x.env.Namespace, Required: "Kubernetes linux/amd64 and declared image bindings", State: "ready", Detail: "Image profile uses Kubernetes workloads directly; no Orka installation required. Registry pull and execution remain unproven until workload execution."}, nil
	}
	var artifact *suiteoras.Artifact
	var err error
	if opt.FromLayout != "" {
		_, digest, ok := strings.Cut(opt.Reference, "@")
		if !ok {
			return report, errors.New("exact suite digest required")
		}
		artifact, err = suiteoras.ReadArtifactLayout(ctx, opt.FromLayout, digest)
	} else {
		artifact, err = suiteoras.ReadArtifactRegistry(ctx, opt.Reference, opt.PlainHTTP)
	}
	if err != nil {
		return report, errors.New("cannot read exact suite artifact")
	}
	suite, err := workspace.LoadArtifact(ctx, artifact)
	if err != nil {
		return report, err
	}
	report = SuiteRuntimeReport{SuiteDigest: suite.Digest, Runtime: suite.Runtime.ID, Context: opt.ToContext, Namespace: valueOr(opt.ToNamespace, OrkaNamespace), State: "unknown"}
	if opt.ExpectedRuntime != "" && opt.ExpectedRuntime != suite.Runtime.ID {
		return report, errors.New("destination runtime expectation differs from packaged runtime")
	}
	if suite.Runtime.ID != "orka" {
		report.Required = suite.Runtime.ID + " " + suite.Runtime.Version
		report.State = "operator-prepared"
		report.Detail = "This runtime has no KMX installer. Provision the exact packaged runtime separately."
		return report, nil
	}
	report.Required = "Orka " + OrkaVersion + " harness-v2 pinned chart/controller/AI worker"
	w := a.releaseLiftWorker(ctx, opt)
	raw, err := w.orkaCapture(ctx, nil, "-n", report.Namespace, "get", "deploy", "-o", "json")
	if err != nil {
		report.Detail = "Controller inventory unreadable; not absent"
		return report, nil
	}
	var envelope struct{ Items json.RawMessage }
	if json.Unmarshal(raw, &envelope) != nil || !strings.HasPrefix(strings.TrimSpace(string(envelope.Items)), "[") {
		report.Detail = "Controller inventory malformed; not absent"
		return report, nil
	}
	var deployments []orkaDeployment
	if json.Unmarshal(envelope.Items, &deployments) != nil {
		return report, errors.New("invalid controller inventory")
	}
	var matches []orkaDeployment
	for _, d := range deployments {
		l := d.Metadata.Labels
		if l["app.kubernetes.io/name"] == "orka" && (l["control-plane"] == "controller-manager" || l["app.kubernetes.io/component"] == "controller") {
			matches = append(matches, d)
		}
	}
	if len(matches) == 0 {
		report.State = "absent"
		report.Detail = "No Orka controller observed. Separate installation rechecks CRDs, Helm ownership and collisions before writing."
		report.Next = "kmx --context " + shellArg(opt.ToContext) + " orka install --provider -"
		return report, nil
	}
	if len(matches) != 1 {
		report.State = "incompatible"
		report.Detail = "Multiple runtime controllers; operator resolution required"
		return report, nil
	}
	if _, err := suiteNativeControllerRuntime(matches[0], report.Namespace); err != nil {
		report.State = "incompatible"
		report.Detail = "Installed Orka does not satisfy the pinned suite runtime, readiness or watch namespace. Existing installations are not upgraded/replaced automatically."
		report.Next = "kmx --context " + shellArg(opt.ToContext) + " orka status"
		return report, nil
	}
	if _, err := w.readSuiteNativeRuntime(ctx, report.Namespace); err != nil {
		report.Detail = "Controller matches but worker identity/readiness could not be verified"
		return report, nil
	}
	report.State = "ready"
	report.Detail = "Runtime prerequisites verified. Inference, Secret references, evidence and placement are checked separately by Lift."
	return report, nil
}

type suiteRuntimePrerequisiteError struct{ context string }

func (e *suiteRuntimePrerequisiteError) Error() string {
	return fmt.Sprintf("suite runtime prerequisites unavailable: native Orka requires %s harness-v2 on the selected destination; inspect with kmx --context %s orka status; for an absent installation use kmx --context %s orka install --provider - (separate confirmed setup, no automatic upgrade)", OrkaVersion, shellArg(e.context), shellArg(e.context))
}
func (e *suiteRuntimePrerequisiteError) Unwrap() error { return ErrReleaseLift }

func (a *App) PrepareConsoleRuntime(ctx context.Context, contextName, typed string) error {
	if strings.TrimSpace(contextName) == "" || typed != contextName || scaffold.RefuseKeyShapes(contextName) != nil {
		return errors.New("explicit matching context confirmation required")
	}
	w := a.releaseLiftWorker(ctx, ReleaseLiftOptions{ToContext: contextName})
	w.Cfg.Confirm = typed
	w.InvocationCommand = "kmx --context " + shellArg(contextName) + " orka install --provider -"
	return w.OrkaInstall(OrkaOptions{Provider: "-"})
}
