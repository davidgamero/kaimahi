package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite/workspace"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/scaffold"
)

type SuiteExtractOptions struct{ Agent, Namespace, Name, Workspace string }

// Suggestions are availability observations, not reservations. Exclusive
// workspace creation still checks the destination at the point of writing.
func suggestExtractionWorkspace(parent, agent string) (string, string, error) {
	parent, err := filepath.Abs(parent)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(parent)
	if err != nil {
		return "", "", fmt.Errorf("cannot inspect destination parent: %w", err)
	}
	if !info.IsDir() {
		return "", "", errors.New("destination parent must be a directory")
	}
	base := slugAgentName(agent)
	base = strings.TrimRight(base[:min(len(base), 50)], "-") + "-copy"
	for i := 1; i <= 1000; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		path := filepath.Join(parent, name)
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			if err := scaffold.ValidateName(name); err != nil {
				return "", "", err
			}
			if name != agent {
				return path, name, nil
			}
		} else if err != nil {
			return "", "", fmt.Errorf("cannot check destination availability: %w", err)
		}
	}
	return "", "", errors.New("no available suggested folder; choose a different destination")
}

func extractionWorkspaceAvailable(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return errors.New("destination already exists; choose a new workspace directory")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("cannot check destination: %w", err)
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("destination parent unavailable: %w", err)
	}
	if !parent.IsDir() {
		return errors.New("destination parent must be a directory")
	}
	return nil
}

type SuiteExtractSummary struct {
	Agent        string `json:"agent"`
	Namespace    string `json:"namespace"`
	Context      string `json:"context"`
	ClusterUID   string `json:"clusterUID"`
	SourceUID    string `json:"sourceUID"`
	Member       string `json:"member"`
	Model        string `json:"model"`
	Instructions string `json:"instructions"`
	Workspace    string `json:"workspace"`
	State        string `json:"state"`
}
type extractionRead struct {
	kind, namespace, name string
	raw                   map[string]any
}
type PreparedSuiteExtraction struct {
	Summary SuiteExtractSummary
	options SuiteExtractOptions
	reads   []extractionRead
}

func ValidateSuiteExtractOptions(o SuiteExtractOptions) error {
	if scaffold.ValidateObjectName(o.Agent) != nil || scaffold.ValidateNamespace(o.Namespace) != nil || scaffold.ValidateName(o.Name) != nil || o.Name == o.Agent || strings.TrimSpace(o.Workspace) == "" {
		return errors.New("extract requires an agent, explicit namespace, new workspace and distinct valid --name")
	}
	return nil
}

// PrepareSuiteExtraction performs only named GETs. No Secret content, source
// files, cache, evaluation, admission or cluster mutation belongs to extraction.
func (a *App) PrepareSuiteExtraction(ctx context.Context, o SuiteExtractOptions) (*PreparedSuiteExtraction, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := ValidateSuiteExtractOptions(o); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(o.Workspace)
	if err != nil {
		return nil, err
	}
	o.Workspace = root
	if err := extractionWorkspaceAvailable(root); err != nil {
		return nil, err
	}
	p := &PreparedSuiteExtraction{options: o}
	read := func(kind, ns, name string) (map[string]any, error) {
		raw, err := a.orkaCapture(ctx, nil, "-n", ns, "get", kind, name, "-o", "json")
		if err != nil {
			return nil, fmt.Errorf("extraction cannot read %s/%s", kind, name)
		}
		var doc map[string]any
		if len(raw) > 1<<20 || json.Unmarshal(raw, &doc) != nil {
			return nil, errors.New("invalid or oversized extraction input")
		}
		meta, _ := doc["metadata"].(map[string]any)
		if meta == nil {
			return nil, errors.New("missing extraction metadata")
		}
		uid, _ := meta["uid"].(string)
		rv, _ := meta["resourceVersion"].(string)
		if meta["name"] != name || meta["namespace"] != ns || uid == "" || rv == "" || meta["deletionTimestamp"] != nil {
			return nil, errors.New("extraction identity unavailable")
		}
		p.reads = append(p.reads, extractionRead{kind, ns, name, doc})
		return doc, nil
	}
	uid, terminating, err := a.readLiftClusterUID(ctx)
	if err != nil || terminating {
		return nil, errors.New("extraction cluster identity unavailable")
	}
	doc, err := read(orkaPlural("Agent"), o.Namespace, o.Agent)
	if err != nil {
		return nil, err
	}
	if doc["apiVersion"] != "core.orka.ai/v1alpha1" || doc["kind"] != "Agent" {
		return nil, errors.New("extract supports native Orka Agents only")
	}
	if err := refuseOrkaSuiteDocument(doc); err != nil {
		return nil, err
	}
	meta := doc["metadata"].(map[string]any)
	spec, _ := doc["spec"].(map[string]any)
	if err := extractionFields(spec, "spec", "providerRef", "model", "systemPrompt"); err != nil {
		return nil, err
	}
	ref, _ := spec["providerRef"].(map[string]any)
	if err := extractionFields(ref, "spec.providerRef", "name", "namespace"); err != nil {
		return nil, err
	}
	provider, _ := ref["name"].(string)
	ns, _ := ref["namespace"].(string)
	if ns == "" {
		ns = o.Namespace
	}
	if scaffold.ValidateObjectName(provider) != nil || scaffold.ValidateNamespace(ns) != nil {
		return nil, errors.New("invalid Provider reference")
	}
	pd, err := read(orkaPlural("Provider"), ns, provider)
	if err != nil {
		return nil, err
	}
	ps, _ := pd["spec"].(map[string]any)
	if pd["kind"] != "Provider" || pd["apiVersion"] != "core.orka.ai/v1alpha1" {
		return nil, errors.New("invalid Provider kind/version")
	}
	if err := refuseOrkaSuiteDocument(pd); err != nil {
		return nil, errors.New("source uses a suite-owned Provider; extraction refused")
	}
	if err := extractionFields(ps, "provider.spec", "type", "baseURL", "defaultModel", "secretRef"); err != nil {
		return nil, err
	}
	if ps["type"] != "openai" {
		return nil, errors.New("extract requires an openai Provider")
	}
	model, _ := spec["model"].(map[string]any)
	if spec["model"] != nil {
		if err := extractionFields(model, "spec.model", "name"); err != nil {
			return nil, err
		}
	}
	name, _ := model["name"].(string)
	if _, present := model["name"]; present && strings.TrimSpace(name) == "" {
		return nil, errors.New("explicit model name is invalid")
	}
	if name == "" {
		name, _ = ps["defaultModel"].(string)
	}
	prompt, _ := spec["systemPrompt"].(map[string]any)
	if err := extractionFields(prompt, "spec.systemPrompt", "inline", "configMapRef"); err != nil {
		return nil, err
	}
	text, _ := prompt["inline"].(string)
	if cm, ok := prompt["configMapRef"]; ok {
		if text != "" {
			return nil, errors.New("ambiguous instruction source")
		}
		ref, _ := cm.(map[string]any)
		if err := extractionFields(ref, "spec.systemPrompt.configMapRef", "name", "key"); err != nil {
			return nil, err
		}
		cn, _ := ref["name"].(string)
		key, _ := ref["key"].(string)
		if scaffold.ValidateObjectName(cn) != nil || key == "" {
			return nil, errors.New("invalid instruction reference")
		}
		cmDoc, err := read("configmap", o.Namespace, cn)
		if err != nil {
			return nil, err
		}
		data, _ := cmDoc["data"].(map[string]any)
		text, _ = data[key].(string)
	}
	if strings.TrimSpace(text) == "" || strings.TrimSpace(name) == "" || scaffold.RefuseKeyShapes(text+"\n"+name) != nil || scaffold.ValidateBlockText(text) != nil || scaffold.ValidateSingleLineText(name) != nil {
		return nil, errors.New("instructions or exact model cannot be extracted")
	}
	sourceUID, _ := meta["uid"].(string)
	p.Summary = SuiteExtractSummary{o.Agent, o.Namespace, a.Cfg.KubeContext, uid, sourceUID, o.Name, name, text, o.Workspace, "review only; original unchanged; unevaluated"}
	// Bracket every dependency before returning the reviewed snapshot. Status
	// churn may refuse conservatively; no mixed-source snapshot is exported.
	for _, r := range append([]extractionRead(nil), p.reads...) {
		fresh, e := read(r.kind, r.namespace, r.name)
		if e != nil || !reflect.DeepEqual(r.raw, fresh) {
			return nil, errors.New("extraction source changed during reads")
		}
	}
	p.reads = p.reads[:len(p.reads)/2]
	return p, nil
}

func extractionFields(m map[string]any, path string, allowed ...string) error {
	if m == nil {
		return fmt.Errorf("missing object %s", path)
	}
	var unsupported []string
	for k := range m {
		found := false
		for _, a := range allowed {
			if k == a {
				found = true
			}
		}
		if !found {
			unsupported = append(unsupported, path+"."+k)
		}
	}
	sort.Strings(unsupported)
	if len(unsupported) > 0 {
		return fmt.Errorf("unsupported extraction fields: %s", strings.Join(unsupported, ", "))
	}
	return nil
}

func (a *App) ExtractSuite(ctx context.Context, p *PreparedSuiteExtraction) error {
	if p == nil {
		return errors.New("extraction review required")
	}
	fresh, err := a.PrepareSuiteExtraction(ctx, p.options)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(p, fresh) {
		return errors.New("source changed after extraction review; review again")
	}
	return workspace.CreateExclusive(ctx, p.options.Workspace, workspace.CreateOptions{Name: p.options.Name, Model: p.Summary.Model}, p.Summary.Instructions)
}
