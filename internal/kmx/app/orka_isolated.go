package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/scaffold"
	"go.yaml.in/yaml/v3"
)

// A second installation reuses the existing CRD schema read-only. It never
// upgrades shared CRDs, adopts an occupied namespace or changes another release.
func (a *App) checkIsolatedOrka(ctx context.Context, namespace string, chart []byte) error {
	if err := scaffold.ValidateNamespace(namespace); err != nil {
		return err
	}
	if namespace == OrkaNamespace || len(namespace) > 40 {
		return errors.New("choose a new namespace, distinct from orka-system, at most 40 characters")
	}
	for _, ns := range []string{namespace, namespace + "-runtimes"} {
		raw, err := a.orkaCapture(ctx, nil, "get", "namespace", ns, "--ignore-not-found=true", "-o", "name")
		if err != nil {
			return err
		}
		if len(bytes.TrimSpace(raw)) != 0 {
			return fmt.Errorf("namespace %s is occupied; choose a new namespace", ns)
		}
	}
	raw, err := a.orkaCapture(ctx, nil, "get", "deployments", "-A", "-l", "app.kubernetes.io/name=orka", "-o", "json")
	if err != nil {
		return err
	}
	var list objectList[orkaDeployment]
	if err := decodeConsoleList(raw, &list); err != nil {
		return err
	}
	controllers := 0
	for _, d := range list.Items {
		if d.Metadata.Labels["control-plane"] != "controller-manager" && d.Metadata.Labels["app.kubernetes.io/component"] != "controller" {
			continue
		}
		controllers++
		if _, err := suiteNativeControllerRuntime(d, d.Metadata.Namespace); err != nil {
			return errors.New("existing Orka controller is not a Ready pinned v0.2.0 isolated installation; a new namespace cannot isolate incompatible CRDs or cluster-wide watches")
		}
		for _, c := range d.Spec.Template.Spec.Containers {
			if c.Name != "controller" {
				continue
			}
			isolation, err := suiteNativeControllerFlag(c.Args, "enforce-namespace-isolation")
			if err != nil || isolation != "true" {
				return errors.New("existing controller does not enforce namespace isolation")
			}
			runtimeNS, err := suiteNativeControllerFlag(c.Args, "acp-runtime-namespace")
			if err != nil {
				return errors.New("existing controller runtime namespace cannot be established")
			}
			if runtimeNS == namespace || runtimeNS == namespace+"-runtimes" {
				return errors.New("runtime namespace overlaps existing controller")
			}
		}
	}
	if controllers == 0 {
		return errors.New("no compatible existing Orka controller found; use standard runtime setup on a clean target")
	}
	for _, kind := range []string{"validatingwebhookconfigurations", "mutatingwebhookconfigurations"} {
		raw, err := a.orkaCapture(ctx, nil, "get", kind, "-o", "json")
		if err != nil {
			return err
		}
		var webhooks objectList[struct {
			Webhooks []struct {
				Rules             []struct{ APIGroups []string }
				NamespaceSelector struct{ MatchLabels map[string]string }
			}
		}]
		if err := decodeConsoleList(raw, &webhooks); err != nil {
			return err
		}
		for _, config := range webhooks.Items {
			for _, hook := range config.Webhooks {
				orka := false
				for _, rule := range hook.Rules {
					for _, group := range rule.APIGroups {
						if group == "*" || strings.HasSuffix(group, ".orka.ai") {
							orka = true
						}
					}
				}
				if !orka {
					continue
				}
				ns := hook.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"]
				if ns == "" || ns == namespace || ns == namespace+"-runtimes" {
					return errors.New("existing admission webhook may overlap new Orka namespace; independent namespace isolation is unproven")
				}
			}
		}
	}
	crds, _, err := orkaChartCRDs(chart)
	if err != nil {
		return err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(crds))
	for {
		var expected map[string]any
		if err := decoder.Decode(&expected); err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		if len(expected) == 0 {
			continue
		}
		meta, ok := expected["metadata"].(map[string]any)
		if !ok {
			return errors.New("invalid chart CRD metadata")
		}
		name, _ := meta["name"].(string)
		raw, err := a.orkaCapture(ctx, nil, "get", "crd", name, "-o", "json")
		if err != nil {
			return fmt.Errorf("shared CRD %s is missing or unreadable; refusing CRD writes", name)
		}
		var live map[string]any
		if json.Unmarshal(raw, &live) != nil {
			return errors.New("invalid existing CRD")
		}
		// Admission may add defaults, but every packaged schema field must match.
		if !orkaSchemaContains(expected["spec"], live["spec"]) {
			return fmt.Errorf("shared CRD %s differs from pinned chart; namespace isolation cannot repair schema incompatibility", name)
		}
	}
	return nil
}

func orkaSchemaContains(want, got any) bool {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for k, v := range w {
			if !orkaSchemaContains(v, g[k]) {
				return false
			}
		}
		return true
	case []any:
		g, ok := got.([]any)
		if !ok || len(w) != len(g) {
			return false
		}
		for i, v := range w {
			if !orkaSchemaContains(v, g[i]) {
				return false
			}
		}
		return true
	default:
		wb, _ := json.Marshal(want)
		gb, _ := json.Marshal(got)
		return bytes.Equal(wb, gb) || reflect.DeepEqual(want, got)
	}
}

func (a *App) prepareIsolatedOrka(ctx context.Context, namespace, typed string) error {
	if a.Cfg == nil || typed != a.Cfg.KubeContext {
		return errors.New("exact target context confirmation required")
	}
	if err := a.preflight(depHelm, depKubectl); err != nil {
		return err
	}
	chart, err := a.fetchOrkaInstaller()
	if err != nil {
		return err
	}
	if err := a.checkIsolatedOrka(ctx, namespace, chart); err != nil {
		return err
	}
	if err := a.guardOrkaMutation(ctx, CreateOptions{Name: "kmx-orka", Namespace: namespace}, "install a separate KMX-managed Orka release in "+namespace+" and "+namespace+"-runtimes; reuse compatible shared CRDs read-only"); err != nil {
		return err
	}
	if err := a.checkIsolatedOrka(ctx, namespace, chart); err != nil {
		return err
	}
	f, err := os.CreateTemp("", "kmx-isolated-orka-*.tgz")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(chart); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	release := "kmx-" + namespace
	args := []string{"install", release, f.Name(), "--namespace", namespace, "--kube-context", a.Cfg.KubeContext, "--create-namespace", "--skip-crds", "--wait", "--timeout", "10m", "--set-string", "fullnameOverride=" + release, "--set-string", "controller.watchNamespace=" + namespace, "--set-string", "controller.acpRuntime.namespace=" + namespace + "-runtimes", "--set-string", "labels.kmx\\.kaimahi\\.dev/installed=true", "--set-string", "controller.mode=harness-v2", "--set-string", "controller.image.digest=" + orkaControllerDigest, "--set-string", "workers.ai.image.digest=" + orkaAIWorkerDigest, "--set-string", "workers.general.image.digest=" + orkaGeneralWorkerDigest, "--set-string", "publisher.image.digest=" + orkaPublisherDigest, "--set-string", "controller.acpRuntime.codexImage=" + orkaCodexImage, "--set-string", "controller.acpRuntime.claudeImage=" + orkaClaudeImage, "--set-string", "controller.acpRuntime.copilotImage=" + orkaCopilotImage, "--set-string", "controller.acpRuntime.opencodeImage=" + orkaOpencodeImage}
	args = append(args, "--set-string", "annotations.kmx\\.kaimahi\\.dev/installed=true", "--set-string", "annotations.kmx\\.kaimahi\\.dev/runtime-version="+OrkaVersion)
	// Preflight every rendered object's identity before Helm can create anything,
	// including cluster-scoped policy/RBAC/webhook names. No adoption is allowed.
	renderArgs := append([]string{"template"}, args[1:]...)
	filtered := []string{}
	for i := 0; i < len(renderArgs); i++ {
		if renderArgs[i] == "--wait" || renderArgs[i] == "--create-namespace" || renderArgs[i] == "--skip-crds" {
			continue
		}
		if renderArgs[i] == "--timeout" {
			i++
			continue
		}
		filtered = append(filtered, renderArgs[i])
	}
	filtered = append(filtered, "--set", "controller.gateway.crdsReadyOverride=true")
	rendered, err := a.azureCapture(ctx, 30e9, "helm", filtered, "isolated Orka chart rendering failed")
	if err != nil {
		return err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(rendered))
	for {
		var doc struct {
			Kind     string
			Metadata struct{ Name, Namespace string }
		}
		if err := decoder.Decode(&doc); err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		if doc.Kind == "" {
			continue
		}
		if doc.Metadata.Name == "" || strings.Contains(doc.Metadata.Name, "{{") {
			return errors.New("unresolved chart object identity")
		}
		query := []string{"get", doc.Kind, doc.Metadata.Name, "--ignore-not-found=true", "-o", "name"}
		switch doc.Kind {
		case "Namespace", "ClusterRole", "ClusterRoleBinding", "ValidatingWebhookConfiguration", "MutatingWebhookConfiguration", "ValidatingAdmissionPolicy", "ValidatingAdmissionPolicyBinding":
		case "ServiceAccount", "Role", "RoleBinding", "Deployment", "Service", "Secret", "ConfigMap", "PersistentVolumeClaim", "NetworkPolicy", "Job":
			if doc.Metadata.Namespace == "" {
				doc.Metadata.Namespace = namespace
			}
			if doc.Metadata.Namespace != namespace && doc.Metadata.Namespace != namespace+"-runtimes" {
				return errors.New("chart object escapes isolated namespaces")
			}
		default:
			return fmt.Errorf("unqualified chart resource kind %s", doc.Kind)
		}
		if doc.Metadata.Namespace != "" {
			query = append(query, "-n", doc.Metadata.Namespace)
		}
		raw, err := a.orkaCapture(ctx, nil, query...)
		if err != nil {
			return err
		}
		if len(bytes.TrimSpace(raw)) > 0 {
			return fmt.Errorf("chart resource %s/%s occupied; refusing adoption", doc.Kind, doc.Metadata.Name)
		}
	}
	if err := a.checkIsolatedOrka(ctx, namespace, chart); err != nil {
		return err
	}
	quiet := *a.Run
	a.suiteProgress("Installing isolated pinned Orka; waiting for readiness", namespace, "running")
	quiet.Echo = false
	quiet.Stdout, quiet.Stderr = io.Discard, io.Discard
	if err := quiet.Run("helm", args...); err != nil {
		return errors.New("isolated Orka installation failed or partial; inspect Helm release before any retry")
	}
	if _, err := a.readSuiteNativeRuntime(ctx, namespace); err != nil {
		return err
	}
	// Result transport has namespace-specific access, separate from worker RBAC.
	objects := []map[string]any{
		{"apiVersion": "v1", "kind": "ServiceAccount", "metadata": map[string]string{"name": "orka-result-reader", "namespace": namespace}},
		{"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "Role", "metadata": map[string]string{"name": "orka-result-reader", "namespace": namespace}, "rules": []any{map[string]any{"apiGroups": []string{"core.orka.ai"}, "resources": []string{"tasks"}, "verbs": []string{"get"}}}},
		{"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "RoleBinding", "metadata": map[string]string{"name": "orka-result-reader", "namespace": namespace}, "roleRef": map[string]string{"apiGroup": "rbac.authorization.k8s.io", "kind": "Role", "name": "orka-result-reader"}, "subjects": []any{map[string]string{"kind": "ServiceAccount", "name": "orka-result-reader", "namespace": namespace}}},
	}
	for _, obj := range objects {
		raw, _ := json.Marshal(obj)
		if _, err := a.orkaCapture(ctx, raw, "create", "-f", "-", "-o", "name"); err != nil {
			return errors.New("runtime installed but result-reader setup incomplete; inspect target")
		}
	}
	return nil
}
