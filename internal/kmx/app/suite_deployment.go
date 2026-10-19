package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/cliui"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/scaffold"
	godigest "github.com/opencontainers/go-digest"
)

var deploymentIDPattern = regexp.MustCompile(`^([a-f0-9]{32}|[a-f0-9]{64})$`)

func ValidateSuiteDeploymentID(id string) error {
	if !deploymentIDPattern.MatchString(id) {
		return errors.New("invalid deployment ID")
	}
	return nil
}

// NativeDeployment is private rediscovery state, not an evaluation receipt or
// permission to reuse a deployment. Each consumer rechecks package and live proof.
type NativeDeployment struct {
	Version   int
	ID        string
	Options   ReleaseLiftOptions
	Summary   ReleaseLiftSummary
	Placement suiteNativePlacement
}

func deploymentDirectory() (string, error) {
	root, err := config.StateDir()
	return filepath.Join(root, "suite-deployments"), err
}

func (a *App) RetainSuiteDeployment(ctx context.Context, p *PreparedReleaseLift, r ReleaseLiftResult) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if p == nil || r.placement == nil || r.State != "ready" || !reflect.DeepEqual(p.Summary(), r.Summary) {
		return "", errors.New("only verified native Orka placement can be retained")
	}
	if err := a.VerifyReleaseLiftPlacement(ctx, r); err != nil {
		return "", err
	}
	id, err := randomHex(16)
	if err != nil {
		return "", err
	}
	dir, err := deploymentDirectory()
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	if resolved, e := filepath.EvalSymlinks(dir); e != nil || resolved != dir {
		return "", errors.New("deployment directory must not contain links")
	}
	if info, e := os.Stat(dir); e != nil || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("deployment directory must be private")
	}
	record := NativeDeployment{1, id, p.options, p.Summary(), *r.placement}
	if record.Options.Evidence, err = filepath.Abs(record.Options.Evidence); err != nil {
		return "", err
	}
	if record.Options.FromLayout != "" {
		if record.Options.FromLayout, err = filepath.Abs(record.Options.FromLayout); err != nil {
			return "", err
		}
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	root, name, _, err := openSessionReferenceDirectory(filepath.Join(dir, id+".json"))
	if err != nil {
		return "", err
	}
	defer root.Close()
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	_, writeErr := f.Write(raw)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
		return "", err
	}
	return id, nil
}

func ReadSuiteDeployment(id string) (*NativeDeployment, error) {
	if !deploymentIDPattern.MatchString(id) {
		return nil, errors.New("invalid deployment ID")
	}
	dir, err := deploymentDirectory()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, id+".json")
	if resolved, e := filepath.EvalSymlinks(dir); e != nil || resolved != dir {
		return nil, errors.New("deployment directory must not contain links")
	}
	if info, e := os.Stat(dir); e != nil || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("deployment directory must be private")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("deployment record must be a private regular file")
	}
	root, name, _, err := openSessionReferenceDirectory(path)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	raw, _, err := readSessionReferenceFile(root, name)
	if err != nil {
		return nil, err
	}
	var d NativeDeployment
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&d) != nil || d.Version != 1 || d.ID != id || d.Summary.Runtime != "orka" {
		return nil, errors.New("invalid native deployment record")
	}
	if dec.Decode(new(any)) != io.EOF {
		return nil, errors.New("trailing deployment data")
	}
	if !validSuitePassingSubject(d.Placement.Subject) || !suiteNativeOwner(d.Placement.Subject).valid() {
		return nil, errors.New("malformed retained subject")
	}
	if scaffold.ValidateObjectName(d.Summary.Member) != nil || d.Summary.Member != d.Placement.Subject.Member || d.Summary.Model != d.Placement.Subject.Model {
		return nil, errors.New("malformed retained member")
	}
	return &d, nil
}

func (a *App) observeSuiteDeployment(ctx context.Context, id string) (*NativeDeployment, *App, ReleaseLiftResult, error) {
	d, err := ReadSuiteDeployment(id)
	if err != nil {
		return nil, nil, ReleaseLiftResult{}, err
	}
	if ValidateReleaseLiftOptions(d.Options) != nil {
		return nil, nil, ReleaseLiftResult{}, errors.New("invalid retained selectors")
	}
	w := a.releaseLiftWorker(ctx, d.Options)
	in, err := readReleaseLiftInputs(ctx, d.Options)
	if err != nil {
		return nil, nil, ReleaseLiftResult{}, errors.New("retained artifact/evidence unavailable or changed")
	}
	if godigest.FromBytes(in.evidence).String() != d.Summary.EvidenceDigest || d.Options.ToContext != d.Placement.Subject.Target.Context || d.Options.ToNamespace != "" && d.Options.ToNamespace != d.Placement.Subject.Target.Namespace {
		return nil, nil, ReleaseLiftResult{}, errors.New("retained evidence or destination changed")
	}
	p, err := w.prepareSuiteNative(ctx, in.suite, d.Placement.Subject.Target.Namespace, strings.TrimPrefix(d.Options.Inference, "provider:"))
	if err != nil {
		return nil, nil, ReleaseLiftResult{}, err
	}
	if !reflect.DeepEqual(p.Subject, d.Placement.Subject) || d.Summary.SuiteDigest != p.Subject.SuiteDigest || d.Summary.Member != p.Subject.Member || d.Summary.Target != releaseLiftTarget(p.Subject.Target) {
		return nil, nil, ReleaseLiftResult{}, errors.New("retained deployment identity changed")
	}
	for i, doc := range []map[string]any{p.bundle.Provider, p.bundle.Agent} {
		digest, err := suiteNativeDigest("kmx.native-orka.spec/v1", doc["spec"])
		if err != nil {
			return nil, nil, ReleaseLiftResult{}, err
		}
		proof := []suiteNativeOwnedResource{d.Placement.Provider, d.Placement.Agent}[i]
		if proof.SpecDigest != digest || proof.UID == "" || proof.Generation < 1 {
			return nil, nil, ReleaseLiftResult{}, errors.New("retained resource proof does not match packaged rendering")
		}
	}
	gate, model, err := w.releaseLiftGate(ctx, in.receipt.Subject, p.Subject, d.Options.Promote)
	if err != nil || gate != d.Summary.Gate || model != d.Summary.ServingModel {
		return nil, nil, ReleaseLiftResult{}, errors.New("retained evidence gate changed")
	}
	r := ReleaseLiftResult{Summary: d.Summary, State: "ready", placement: &d.Placement}
	if err = w.VerifyReleaseLiftPlacement(ctx, r); err != nil {
		return nil, nil, r, err
	}
	return d, w, r, nil
}

func (a *App) SuiteDeploymentStatus(ctx context.Context, id string) (ReleaseLiftSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	d, _, _, err := a.observeSuiteDeployment(ctx, id)
	if err != nil {
		return ReleaseLiftSummary{}, err
	}
	summary := d.Summary
	summary.Resources = append([]ReleaseLiftResource(nil), summary.Resources...)
	if len(summary.Resources) != 2 {
		return ReleaseLiftSummary{}, errors.New("invalid retained resource inventory")
	}
	for i, proof := range []suiteNativeOwnedResource{d.Placement.Provider, d.Placement.Agent} {
		summary.Resources[i].Action = "observed-ready"
		summary.Resources[i].UID, summary.Resources[i].Generation, summary.Resources[i].SpecDigest = proof.UID, proof.Generation, proof.SpecDigest
		summary.Resources[i].ResourceVersion = ""
	}
	return summary, nil
}

func (a *App) RunSuiteMember(ctx context.Context, id, member, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	if strings.TrimSpace(prompt) == "" || len(prompt) > 1<<20 {
		return "", errors.New("member prompt must be nonblank and at most 1 MiB")
	}
	if len(id) == 64 {
		x, err := a.loadImagePlacement(ctx, id)
		if err != nil {
			return "", err
		}
		if member != "" && member != x.suite.MemberID {
			return "", errors.New("member differs from retained image placement")
		}
		return a.RunImagePlacement(ctx, id, prompt)
	}
	d, w, r, err := a.observeSuiteDeployment(ctx, id)
	if err != nil {
		return "", err
	}
	if member != "" && member != d.Summary.Member {
		return "", errors.New("member is not in selected deployment")
	}
	w.InvocationCommand = "kmx run --deployment " + shellArg(id) + " --member " + shellArg(d.Summary.Member) + " --prompt-file -"
	if err = w.guardOrkaMutation(ctx, CreateOptions{Name: d.Summary.Member, Namespace: d.Summary.Target.Namespace}, "invoke suite member "+d.Summary.Member); err != nil {
		return "", err
	}
	fresh, _, _, err := w.observeSuiteDeployment(ctx, id)
	if err != nil {
		return "", err
	}
	if !reflect.DeepEqual(d, fresh) {
		return "", errors.New("retained input changed during execution consent")
	}
	answer, err := w.answerRetainedMember(ctx, r, prompt)
	if err != nil {
		return "", err
	}
	if err = w.VerifyReleaseLiftPlacement(ctx, r); err != nil {
		return "", err
	}
	return answer, nil
}

func (a *App) answerRetainedMember(parent context.Context, r ReleaseLiftResult, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, defaultEvaluationCaseTimeout)
	defer cancel()
	ns, name := r.Summary.Target.Namespace, r.Summary.Member
	suffix, err := randomHex(8)
	if err != nil {
		return "", err
	}
	taskName := strings.TrimRight(name[:min(len(name), 40)], "-.") + "-run-" + suffix
	doc := map[string]any{"apiVersion": "core.orka.ai/v1alpha1", "kind": "Task", "metadata": map[string]any{"name": taskName, "namespace": ns}, "spec": map[string]any{"type": "ai", "prompt": prompt, "agentRef": map[string]any{"name": name, "namespace": ns}, "resources": map[string]any{}}}
	options, err := interactiveResultOptions(ns)
	if err != nil {
		return "", err
	}
	if a.quickstartResultPort != "" {
		options.ResultPort = a.quickstartResultPort
	}
	session, err := a.openOrkaResultSession(ctx, options)
	if err != nil {
		return "", err
	}
	defer session.close()
	if err = session.probeBeforeCreate(ctx, ns, taskName, "no Task created"); err != nil {
		return "", err
	}
	if err = a.VerifyReleaseLiftPlacement(ctx, r); err != nil {
		return "", err
	}
	id, created, err := a.createOrkaObjectObserved(ctx, ns, doc)
	if err != nil {
		return "", err
	}
	if err = validateSuiteNativeTask(created, doc); err != nil {
		return "", err
	}
	if _, err = fmt.Fprintf(a.Err, "Task: %s\nRetrieve: %s\n", id.Name, a.taskRecovery(id.Name, a.Cfg.KubeContext, ns)); err != nil {
		return "", fmt.Errorf("Task exists; reporting failed: %w", err)
	}
	a.suiteProgress("answer", id.Name, "running")
	answer, err := a.waitOrkaTaskResult(ctx, ns, id, session)
	if err != nil {
		return "", a.runTaskWaitError(ctx, err, id.Name, a.Cfg.KubeContext, ns)
	}
	a.suiteProgress("answer", id.Name, "answered")
	return answer, nil
}

type suiteMemberChat struct {
	app        *App
	id, member string
}

func (b *suiteMemberChat) Agent() string { return b.member }
func (b *suiteMemberChat) Connect(ctx context.Context, _ *chatRenderer) ([]cliui.Field, error) {
	s, err := b.app.SuiteDeploymentStatus(ctx, b.id)
	if err != nil {
		return nil, err
	}
	if b.member != "" && b.member != s.Member {
		return nil, errors.New("member mismatch")
	}
	b.member = s.Member
	return []cliui.Field{{Label: "Suite", Value: s.Suite}, {Label: "Artifact", Value: s.SuiteDigest}, {Label: "Destination", Value: s.Target.Context + " / " + s.Target.Namespace}}, nil
}
func (b *suiteMemberChat) Send(ctx context.Context, text string, r *chatRenderer) error {
	answer, err := b.app.RunSuiteMember(ctx, b.id, b.member, text)
	if err != nil {
		return err
	}
	r.assistant(b.member, answer, true)
	return nil
}
func (a *App) ChatSuiteMember(id, member, initial string) error {
	if len(id) == 64 {
		if member != "" {
			x, err := a.loadImagePlacement(a.operationContext(), id)
			if err != nil {
				return err
			}
			if member != x.suite.MemberID {
				return errors.New("member differs from retained placement")
			}
		}
		return a.ChatImagePlacement(id)
	}
	return a.runInteractiveChatBackendInitial(&suiteMemberChat{a, id, member}, initial)
}

func (a *App) RunSuiteMemberWithOptions(ctx context.Context, id, member string, o RunAgentOptions) error {
	prompt := o.Prompt
	if o.PromptFile != "" {
		if o.PromptFile == "-" {
			if a.Stdin == nil {
				return errors.New("stdin required")
			}
			b, err := io.ReadAll(io.LimitReader(a.Stdin, 1<<20+1))
			if err != nil {
				return err
			}
			prompt = string(b)
		} else {
			s, err := readRunPromptFile(o.PromptFile)
			if err != nil {
				return err
			}
			prompt = s
		}
	}
	answer, err := a.RunSuiteMember(ctx, id, member, prompt)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(a.Out, answer)
	return err
}

func (a *App) WriteSuiteDeploymentStatus(ctx context.Context, id, output string) error {
	if len(id) == 64 {
		rows, err := a.ImagePlacementStatus(ctx, id)
		if err != nil {
			return err
		}
		if output == "json" {
			return json.NewEncoder(a.Out).Encode(rows)
		}
		for _, r := range rows {
			if _, err = fmt.Fprintf(a.Out, "Member: %s\nImage: %s\nReady: %t\nUID: %s\n", r.Agent, r.Image, r.Ready, r.UID); err != nil {
				return err
			}
		}
		return nil
	}
	s, err := a.SuiteDeploymentStatus(ctx, id)
	if err != nil {
		return err
	}
	if output == "json" {
		return json.NewEncoder(a.Out).Encode(s)
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "Suite: %s\nMember: %s\nArtifact: %s\nTarget: %s / %s\nPlacement: verified ready (not an answer or destination evaluation)\n", s.Suite, s.Member, s.SuiteDigest, s.Target.Context, s.Target.Namespace)
	_, err = a.Out.Write(b.Bytes())
	return err
}
