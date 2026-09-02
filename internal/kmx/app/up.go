package app

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/run"
)

// Steps of `kmx up`, in order. They are addressable individually so the
// Makefile's `cluster`, `ollama`, `model`, `kagent`, `agent` and
// `tools-agent` targets can delegate to the same code rather than keeping a
// second copy of it.
var UpSteps = []string{"cluster", "ollama", "model", "kagent", "agent", "tools-agent"}

// Up runs the whole journey, or the single named step.
//
// This is the Makefile's kind `UP_STEPS` — cluster, ollama, model, kagent,
// agent, tools-agent, status. RUNTIME ONLY: milestone 1 does not deploy the
// governance plane (D27), and says so at the end rather than leaving anyone
// to discover it from an empty ledger.
func (a *App) Up(step string) error {
	steps := UpSteps
	if step != "" {
		found := false
		for _, s := range UpSteps {
			if s == step {
				found, steps = true, []string{s}
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown step %q — one of: %s", step, strings.Join(UpSteps, ", "))
		}
	}
	if err := a.preflightUp(steps); err != nil {
		return err
	}

	action := "bring up the kmx runtime (kind, Ollama, kagent, agents)"
	command := "kmx up"
	if step != "" {
		action, command = "run the '"+step+"' step", "kmx up --step "+step
	}
	if err := a.Guard(action, command); err != nil {
		return err
	}

	if step != "" {
		for _, s := range steps {
			var err error
			switch s {
			case "cluster":
				err = a.stepCluster()
			case "ollama":
				err = a.stepOllama()
			case "model":
				err = a.stepModel()
			case "kagent":
				err = a.stepKagent()
			case "agent":
				err = a.stepAgent()
			case "tools-agent":
				err = a.stepToolsAgent()
			}
			if err != nil {
				return err
			}
		}
	} else if err := a.upOverlapped(); err != nil {
		return err
	}

	if step == "" {
		if err := a.Status(); err != nil {
			return err
		}
		// One line: `kmx up` is the RUNTIME. Governance is a deliberate
		// second step, and saying nothing here would leave an operator to
		// infer it from an empty ledger.
		// The credential is the RESOLVED one, not the default: with CRED set,
		// a copied `kmx govern hello-world` would govern a different
		// credential than the one `kmx govern` and `kmx ledger` then use.
		a.notef("\nRuntime only: the Kaimahi governance plane is NOT deployed yet.\n"+
			"Nothing is metered, budgeted or ledgered until it is:\n"+
			"  kmx plane       # the proxy and its ledger\n"+
			"  kmx govern %s  # put %s behind it (docs/spend.md)",
			a.Cfg.Credential, config.DefaultAgent)
		a.notef("\nTalk to the agent:  kmx agent chat %s \"%s\"", config.DefaultAgent, config.DefaultTask)
	}
	return nil
}

// upOverlapped is the whole journey, with the two agents brought up
// together.
//
// The order the steps are DECLARED in (UpSteps) is the order an operator
// reads them in and the order `--step` runs them in, and it is kept here:
// Ollama is deployed and its model pulled before kagent is installed, so a
// cluster that cannot pull at all still fails on the smaller download first.
//
// Only the two agents overlap, and the measurements say why (W25, on a
// 2-CPU GitHub runner):
//
//	hello-world Ready 29s + hello-tools Ready 16s, serially → 33s together
//	ollama's rollout (41s) overlapped with kagent's five pods (61s) → 116s,
//	  against 118s serially: nothing gained
//
// The second line is the useful finding. Those two are not waiting on each
// other, they are waiting on the same network: they pull ~2GB of images
// between them, so running them at once splits the bandwidth instead of
// saving time. The agents are different — their images are already on the
// node — and that is where the 12 seconds are.
//
// Nothing is skipped and nothing is weakened: every command, wait and
// preservation check the serial version ran still runs, with the same
// timeouts.
func (a *App) upOverlapped() error {
	if err := a.stepCluster(); err != nil {
		return err
	}
	if err := a.stepOllama(); err != nil {
		return err
	}
	if err := a.stepModel(); err != nil {
		return err
	}
	if err := a.stepKagent(); err != nil {
		return err
	}
	a.notef("\nbringing up both agents together (each lane's output is tagged)")
	return a.runLanes([]lane{
		{"agent", func(b *App) error { return b.stepAgent() }},
		{"tools-agent", func(b *App) error { return b.stepToolsAgent() }},
	})
}

func (a *App) preflightUp(steps []string) error {
	type dependency struct {
		step, tool, why, install string
	}
	dependencies := []dependency{
		{"cluster", "kind", "to create the local Kubernetes cluster", "https://kind.sigs.k8s.io/docs/user/quick-start/#installation"},
		{"cluster", "kubectl", "to talk to the cluster", "https://kubernetes.io/docs/tasks/tools/"},
		{"ollama", "kubectl", "to deploy Ollama", "https://kubernetes.io/docs/tasks/tools/"},
		{"model", "kubectl", "to pull the model", "https://kubernetes.io/docs/tasks/tools/"},
		{"kagent", "helm", "to install kagent", "https://helm.sh/docs/intro/install/"},
		{"kagent", "kubectl", "to wait for kagent", "https://kubernetes.io/docs/tasks/tools/"},
		{"agent", "kubectl", "to apply the agent", "https://kubernetes.io/docs/tasks/tools/"},
		{"tools-agent", "kubectl", "to apply the tools agent", "https://kubernetes.io/docs/tasks/tools/"},
	}
	wanted := map[string]bool{}
	for _, step := range steps {
		wanted[step] = true
	}
	var problems []string
	seen := map[string]bool{}
	for _, dep := range dependencies {
		if !wanted[dep.step] || seen[dep.tool] {
			continue
		}
		seen[dep.tool] = true
		if err := run.MustExist(dep.tool, dep.why, dep.install); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if wanted["cluster"] {
		engine := a.Cfg.ContainerEngine
		if err := run.MustExist(engine, "as the selected kind container engine",
			"https://docs.docker.com/get-docker/ (Docker) or https://podman.io/docs/installation (Podman)"); err != nil {
			problems = append(problems, err.Error())
		} else if _, err := a.Run.Capture(engine, "info"); err != nil {
			problems = append(problems, fmt.Sprintf("%s is installed but unavailable — start it or fix access, then run `%s info`: %v", engine, engine, err))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	noun := "dependency"
	if len(problems) != 1 {
		noun = "dependencies"
	}
	return fmt.Errorf("preflight: %d missing or unusable %s:\n\n- %s", len(problems), noun, strings.Join(problems, "\n- "))
}

// ---- cluster --------------------------------------------------------------

// stepCluster brings up the kind cluster: create it, or on Podman recover it.
//
// `kind get clusters` failing is NOT "no clusters": a broken or absent kind,
// or a docker socket the user cannot reach, would otherwise read as "the
// cluster is missing" and send us straight into a create that fails later
// and less clearly.
//
// The Podman half is #53's, carried across when this recipe became a
// delegation so the fix is not lost: on that engine "listed" does not mean
// "running", and a cluster whose nodes were stopped has to be started rather
// than re-created.
func (a *App) stepCluster() error {
	if err := run.MustExist("kind", "to create the local Kubernetes cluster",
		"https://kind.sigs.k8s.io/docs/user/quick-start/#installation"); err != nil {
		return err
	}
	if err := run.MustExist("kubectl", "to talk to the cluster",
		"https://kubernetes.io/docs/tasks/tools/"); err != nil {
		return err
	}
	existing, err := a.Run.Capture("kind", "get", "clusters")
	if err != nil {
		return fmt.Errorf("`kind get clusters` failed — refusing to guess whether %q exists (is the %s daemon running?): %w",
			a.Cfg.KindCluster, a.Cfg.ContainerEngine, err)
	}
	listed := false
	for _, line := range strings.Split(existing, "\n") {
		if strings.TrimSpace(line) == a.Cfg.KindCluster {
			listed = true
			break
		}
	}

	// The Docker path, unchanged: create it if it is not there.
	if a.Cfg.ContainerEngine != "podman" {
		if listed {
			a.notef("kind cluster %q already exists", a.Cfg.KindCluster)
			return nil
		}
		return a.Run.Run("kind", "create", "cluster", "--name", a.Cfg.KindCluster)
	}

	// The Podman path (#53, by @sajayantony): restarting the podman machine
	// stops kind's node containers, after which `kind get clusters` still
	// lists the cluster and nothing can reach it. Listed-but-stopped is
	// therefore not "already exists" — start the nodes back up.
	if listed {
		nodes, err := a.Run.Capture("podman", "ps", "-a",
			"--filter", "label=io.x-k8s.kind.cluster="+a.Cfg.KindCluster,
			"--format", "{{.Names}}")
		if err != nil {
			return fmt.Errorf("cannot list podman's nodes for kind cluster %q: %w", a.Cfg.KindCluster, err)
		}
		names, err := podmanNodes(nodes, a.Cfg.KindCluster)
		if err != nil {
			return err
		}
		if err := a.Run.Run("podman", append([]string{"start"}, names...)...); err != nil {
			return err
		}
	} else if err := a.Run.Run("kind", "create", "cluster", "--name", a.Cfg.KindCluster); err != nil {
		return err
	}

	// Restarted or freshly created, the API server and CoreDNS have to be up
	// before anything else runs: without this the Ollama model pull could
	// start into a cluster that was not yet serving.
	ready := run.Poll(60, 2*time.Second, func() bool {
		return a.kubectlQuiet("get", "--raw=/readyz")
	})
	if !ready {
		return fmt.Errorf("kind cluster %q API did not become ready after 120s", a.Cfg.KindCluster)
	}
	return a.kubectlRun("-n", "kube-system", "rollout", "status", "deployment/coredns", "--timeout=180s")
}

// podmanNodes turns `podman ps -a --format {{.Names}}` output into the node
// list to start, and refuses an empty one.
//
// Empty is the case worth naming: kind still lists the cluster, so something
// exists as far as kind is concerned, but Podman has no containers for it —
// kind's record and Podman's reality disagree. Starting nothing and carrying
// on would walk into a cluster nothing can reach, so it fails closed here
// instead.
func podmanNodes(listing, cluster string) ([]string, error) {
	names := strings.Fields(listing)
	if len(names) == 0 {
		return nil, fmt.Errorf("kind lists cluster %q, but podman has no nodes for it", cluster)
	}
	return names, nil
}

// ---- ollama ---------------------------------------------------------------

func (a *App) stepOllama() error {
	if err := a.apply("ollama.yaml"); err != nil {
		return err
	}
	return a.kubectlRun("-n", "ollama", "rollout", "status", "deploy/ollama", "--timeout=300s")
}

// stepModel pulls the pinned model into the running Ollama.
func (a *App) stepModel() error {
	return a.kubectlRun("-n", "ollama", "exec", "deploy/ollama", "--", "ollama", "pull", a.Cfg.Model)
}

// ---- kagent ---------------------------------------------------------------

func (a *App) stepKagent() error {
	if err := run.MustExist("helm", "to install kagent", "https://helm.sh/docs/intro/install/"); err != nil {
		return err
	}
	version := a.Cfg.KagentVersion
	if err := a.Run.Run("helm", "upgrade", "--install", "kagent-crds",
		"oci://ghcr.io/kagent-dev/kagent/helm/kagent-crds",
		"--version", version, "--namespace", "kagent", "--create-namespace",
		"--kube-context", a.Cfg.KubeContext); err != nil {
		return err
	}

	// helm -f wants a path, and the values file lives inside the binary.
	values, err := manifest("kagent-values.yaml")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "kagent-values-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(values); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := a.Run.Run("helm", "upgrade", "--install", "kagent",
		"oci://ghcr.io/kagent-dev/kagent/helm/kagent",
		"--version", version, "--namespace", "kagent",
		"--kube-context", a.Cfg.KubeContext, "-f", tmp.Name()); err != nil {
		return err
	}
	return a.kubectlRun("-n", "kagent", "wait", "--for=condition=Ready", "pods", "--all", "--timeout=420s")
}

// ---- the agents -----------------------------------------------------------

// liveModelConfig reads an agent's current modelConfig.
//
// Re-applying the committed YAML must not silently drop governance (or any
// preset switch) from a live agent, so the current value is captured first
// and a non-default one is restored after the apply, with a warning. Only a
// NotFound (fresh cluster) may skip the capture — ANY other read failure
// aborts rather than risk silently un-governing an agent.
// isNotFound reports whether a kubectl failure was "the object is not there",
// as opposed to "the cluster could not be reached" or "you may not read it".
//
// The distinction is the whole point of the capture-then-restore dance: a
// fresh cluster legitimately has no agent yet, but an unreachable API server
// must NEVER be read as "nothing to preserve" — that is how a re-apply
// silently un-governs a live agent.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "NotFound") || strings.Contains(message, `" not found`)
}

func (a *App) liveModelConfig(agent string) (string, error) {
	out, err := a.kubectlCapture("-n", "kagent", "get", "agent", agent,
		"-o", "jsonpath={.spec.declarative.modelConfig}")
	if err != nil {
		if isNotFound(err) {
			return "", nil
		}
		// Deliberately NOT treated as absence, however it reads. The most
		// common cause is that kagent is not installed yet — the Agent CRD
		// does not exist, so kubectl says "the server doesn't have a resource
		// type", not NotFound — and applying an Agent to that cluster would
		// fail a moment later anyway. Name the likely cause; do not guess.
		// The hint is on its own line, as every other operator-facing refusal
		// in this package is: these are printed to a terminal, not wrapped by
		// a caller.
		return "", fmt.Errorf("cannot read %s's live modelConfig (refusing to risk un-governing it): %w\n"+
			"  if kagent is not installed on this cluster yet, that is `kmx up`", agent, err)
	}
	return strings.TrimSpace(out), nil
}

// desiredModelConfig applies the preservation rule: a live non-default
// modelConfig wins over the committed one.
func (a *App) desiredModelConfig(agent, current string) (string, bool) {
	if current != "" && current != config.KeylessModelConfig {
		a.notef("NOTE: %s was on modelConfig %q — preserving it ('make use PRESET=ollama' resets)", agent, current)
		return current, true
	}
	return config.KeylessModelConfig, false
}

func (a *App) patchModelConfig(agent, modelConfig string) error {
	patch := fmt.Sprintf(`{"spec":{"declarative":{"modelConfig":%q}}}`, modelConfig)
	return a.kubectlRun("-n", "kagent", "patch", "agent", agent, "--type", "merge", "-p", patch)
}

func (a *App) waitAgentReady(agent string) error {
	return a.kubectlRun("-n", "kagent", "wait",
		`--for=jsonpath={.status.conditions[?(@.type=="Ready")].status}=True`,
		"agent/"+agent, "--timeout=300s")
}

func (a *App) stepAgent() error {
	current, err := a.liveModelConfig("hello-world")
	if err != nil {
		return err
	}
	desired, changed := a.desiredModelConfig("hello-world", current)
	if err := a.apply("hello-world.yaml"); err != nil {
		return err
	}
	if changed {
		if err := a.patchModelConfig("hello-world", desired); err != nil {
			return err
		}
	}
	return a.waitAgentReady("hello-world")
}

// agentJSON is the sliver of an Agent kmx reads back.
type agentJSON struct {
	Spec struct {
		Declarative struct {
			ModelConfig string          `json:"modelConfig"`
			Tools       json.RawMessage `json:"tools"`
		} `json:"declarative"`
	} `json:"spec"`
}

// stepToolsAgent applies the tools agent, preserving both a non-default
// modelConfig and a live gateway wiring.
//
// The gateway restore is P4c's governance-preservation guard: once
// `make govern-tools` has pointed hello-tools at the kaimahi-tools seam,
// re-applying the committed YAML would point it back at the ungoverned
// server. Re-applying a manifest must never be the thing that un-governs an
// agent.
func (a *App) stepToolsAgent() error {
	// The RemoteMCPServer the chart publishes has to be accepted before an
	// Agent can wire to it.
	if err := a.kubectlRun("-n", "kagent", "wait",
		`--for=jsonpath={.status.conditions[?(@.type=="Accepted")].status}=True`,
		"remotemcpserver/kagent-tool-server", "--timeout=300s"); err != nil {
		return err
	}

	var live agentJSON
	raw, err := a.kubectlCapture("-n", "kagent", "get", "agent", "hello-tools", "-o", "json")
	switch {
	case isNotFound(err):
		// Fresh cluster: nothing to preserve.
	case err != nil:
		return fmt.Errorf("cannot read hello-tools' live tool wiring (refusing to risk un-governing it): %w", err)
	default:
		if err := json.Unmarshal([]byte(raw), &live); err != nil {
			return fmt.Errorf("cannot parse hello-tools: %w", err)
		}
	}

	// Is the live agent wired to the GATEWAY? Every entry is inspected, not
	// just the first: an agent with a second tool that happens to sort ahead
	// of the governed one would otherwise read as ungoverned and be quietly
	// pointed back at the unaudited server. And a decode failure is an
	// error, not a "no": failing to understand the live wiring is exactly
	// when this code must not act.
	governedByGateway := false
	if len(live.Spec.Declarative.Tools) > 0 {
		var tools []struct {
			McpServer struct {
				Name string `json:"name"`
			} `json:"mcpServer"`
		}
		if err := json.Unmarshal(live.Spec.Declarative.Tools, &tools); err != nil {
			return fmt.Errorf("cannot read hello-tools' live tool wiring (refusing to risk un-governing it): %w", err)
		}
		for _, tool := range tools {
			if tool.McpServer.Name == "kaimahi-tools" {
				governedByGateway = true
				break
			}
		}
	}

	desired, changed := a.desiredModelConfig("hello-tools", live.Spec.Declarative.ModelConfig)
	if err := a.apply("tools-agent.yaml"); err != nil {
		return err
	}
	if changed {
		if err := a.patchModelConfig("hello-tools", desired); err != nil {
			return err
		}
	}
	if governedByGateway {
		a.notef("NOTE: hello-tools was governed via kaimahi-tools — restoring gateway wiring ('make ungovern-tools' opts out)")
		patch := fmt.Sprintf(`{"spec":{"declarative":{"tools":%s}}}`, string(live.Spec.Declarative.Tools))
		if err := a.kubectlRun("-n", "kagent", "patch", "agent", "hello-tools", "--type", "merge", "-p", patch); err != nil {
			return err
		}
	}
	return a.waitAgentReady("hello-tools")
}
