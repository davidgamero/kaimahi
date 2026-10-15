package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	spec "github.com/agent-primitives/suite"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/cliui"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
	rt "github.com/kaimahi-agents/kaimahi/internal/kmx/runtime"
	"github.com/opencontainers/go-digest"
)

// A retained placement is private local observation, not signed authorization or
// an operation recovery log. Reloading always re-resolves the release and checks
// the actual destination before offering invocation.
type imagePlacementRecord struct {
	Version   int                   `json:"version"`
	Options   ReleaseLiftOptions    `json:"options"`
	Subject   suiteImageSubject     `json:"subject"`
	Resources []ReleaseLiftResource `json:"resources"`
}

func imagePlacementPath(id string) (string, error) {
	if !spec.IsValidDigest("sha256:" + id) {
		return "", errors.New("deployment must be an image placement ID printed by lift")
	}
	state, err := config.StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(state, "image-placements", id+".json"), nil
}

// Offline rediscovery only. Operations must still use loadImagePlacement.
func readImageConsoleEntries(selected string) ([]suiteConsoleEntry, error) {
	state, err := config.StateDir()
	if err != nil {
		return nil, err
	}
	files, err := os.ReadDir(filepath.Join(state, "image-placements"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(files) > 256 {
		return nil, errors.New("image placement catalog exceeds 256 records")
	}
	var rows []suiteConsoleEntry
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(file.Name(), ".json")
		if selected != "" && id != selected {
			continue
		}
		path, err := imagePlacementPath(id)
		if err != nil {
			return nil, err
		}
		raw, err := readImagePassingFile(path)
		if err != nil {
			return nil, err
		}
		var r imagePlacementRecord
		if spec.Unmarshal(raw, &r) != nil || r.Version != 1 {
			return nil, ErrReleaseLiftApply
		}
		canonical, err := spec.Marshal(r)
		if err != nil || digest.FromBytes(canonical).Encoded() != id {
			return nil, ErrReleaseLiftApply
		}
		rows = append(rows, suiteConsoleEntry{Name: r.Subject.Member, Origin: "retained image placement (not rechecked)", Deployment: id, Reference: r.Options.Reference, Evidence: r.Options.Evidence, EnvironmentFile: r.Options.Environment, Context: r.Subject.Target.Context, Image: true, Inference: r.Subject.Model})
		if info, err := file.Info(); err == nil {
			rows[len(rows)-1].Modified = info.ModTime()
		}
	}
	return rows, nil
}

func saveImagePlacement(x *releaseLiftImage) (string, error) {
	r := imagePlacementRecord{Version: 1, Options: x.options, Subject: x.subject, Resources: x.observed}
	raw, err := spec.Marshal(r)
	if err != nil {
		return "", err
	}
	id := digest.FromBytes(raw).Encoded()
	path, err := imagePlacementPath(id)
	if err != nil {
		return "", err
	}
	return id, saveWorkspaceRecord(path, r)
}

func (a *App) loadImagePlacement(ctx context.Context, id string) (*releaseLiftImage, error) {
	path, err := imagePlacementPath(id)
	if err != nil {
		return nil, err
	}
	raw, err := readImagePassingFile(path)
	if err != nil {
		return nil, err
	}
	var r imagePlacementRecord
	if spec.Unmarshal(raw, &r) != nil || r.Version != 1 || len(r.Resources) != 3 {
		return nil, ErrReleaseLiftApply
	}
	canonical, err := spec.Marshal(r)
	if err != nil || digest.FromBytes(canonical).Encoded() != id {
		return nil, ErrReleaseLiftApply
	}
	x, err := a.prepareImageCandidate(ctx, r.Options)
	if err != nil {
		return nil, err
	}
	evidenceBytes, err := readImagePassingFile(r.Options.Evidence)
	if err != nil {
		return nil, err
	}
	evidence, err := decodeImagePassing(ctx, evidenceBytes, x.suite)
	if err != nil {
		return nil, err
	}
	source, destination := evidence.Subject, x.subject
	if r.Options.Promote {
		source.Target, destination.Target = ReleaseLiftTarget{}, ReleaseLiftTarget{}
		source.BindingDigest, destination.BindingDigest = "", ""
	}
	if !reflect.DeepEqual(source, destination) {
		return nil, errors.New("retained image evidence no longer matches placement")
	}
	want, _ := spec.Marshal(r.Subject)
	got, _ := spec.Marshal(x.subject)
	if string(want) != string(got) {
		return nil, errors.New("placement prerequisites changed; inspect and evaluate/lift again")
	}
	worker := a.releaseLiftWorker(ctx, r.Options)
	for i, doc := range x.plan.Objects {
		row, err := worker.inspectImageReleaseResource(ctx, x, doc, &r.Resources[i])
		if err != nil || row.Action != "reuse" {
			return nil, ErrReleaseLiftApply
		}
	}
	x.observed = r.Resources
	return x, nil
}

func imagePlacementConnection(x *releaseLiftImage) WorkspaceDeployment {
	record := WorkspaceDeployment{WorkspaceName: x.suite.Name, Environment: x.env}
	record.Receipt.Plan = rt.SuitePlanSummary{ArtifactDigest: x.suite.Digest, Target: rt.SuiteTarget{Context: x.env.Context, ClusterUID: x.env.ClusterUID, Namespace: x.env.Namespace}, Members: []rt.SuiteMemberSummary{{Agent: x.suite.MemberID, Digest: x.plan.Digest, ConfigurationDigest: strings.TrimPrefix(x.subject.ConfigurationDigest, "sha256:")}}}
	member := rt.SuiteMemberOutcome{Agent: x.suite.MemberID, State: "ready"}
	for _, r := range x.observed {
		member.Resources = append(member.Resources, rt.ResourceResult{Kind: r.Kind, Name: r.Name, Namespace: x.env.Namespace, UID: r.UID})
	}
	record.Receipt.Members = []rt.SuiteMemberOutcome{member}
	return record
}

func (a *App) ImagePlacementStatus(ctx context.Context, id string) ([]SuiteMemberStatus, error) {
	x, err := a.loadImagePlacement(ctx, id)
	if err != nil {
		return nil, err
	}
	return a.workspaceDeploymentStatus(ctx, imagePlacementConnection(x))
}

func (a *App) RunImagePlacement(ctx context.Context, id, prompt string) (string, error) {
	x, err := a.loadImagePlacement(ctx, id)
	if err != nil {
		return "", err
	}
	answer, err := a.RunSuiteDeployment(ctx, imagePlacementConnection(x), x.suite.MemberID, prompt)
	if err != nil {
		return "", err
	}
	if _, err := a.loadImagePlacement(ctx, id); err != nil {
		return "", err
	}
	return answer, nil
}

type imageChatBackend struct {
	app        *App
	id, member string
}

func (b *imageChatBackend) Agent() string { return b.member }
func (b *imageChatBackend) Connect(ctx context.Context, r *chatRenderer) ([]cliui.Field, error) {
	x, err := b.app.loadImagePlacement(ctx, b.id)
	if err != nil {
		return nil, err
	}
	b.member = x.suite.MemberID
	r.statusStart(b.member, x.env.Context)
	return []cliui.Field{{Label: "Suite", Value: x.suite.Digest}, {Label: "Runtime", Value: "image-http"}, {Label: "Model", Value: x.subject.Model}, {Label: "Conversation", Value: "independent requests; no server session"}}, nil
}
func (b *imageChatBackend) Send(ctx context.Context, prompt string, r *chatRenderer) error {
	answer, err := b.app.RunImagePlacement(ctx, b.id, prompt)
	if err != nil {
		return err
	}
	r.beginAssistant(b.member)
	r.assistant(b.member, answer, true)
	return nil
}
func (a *App) ChatImagePlacement(id string) error {
	return a.runInteractiveChatBackend(&imageChatBackend{app: a, id: id, member: "image agent"})
}
