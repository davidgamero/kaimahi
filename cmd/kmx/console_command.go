package main

import (
	"github.com/kaimahi-agents/kaimahi/internal/kmx/app"
	"github.com/spf13/cobra"
)

func newConsoleCommand(state *commandState) *cobra.Command {
	var opt app.AgentTUIOptions
	var attestations bool
	cmd := &cobra.Command{Use: "console", Short: "Open the live local / remote AgentSuite console", Args: cobra.NoArgs,
		Long: "Live suites on the left local and right remote, with source review, evaluation, publication and reviewed Lift.\nNative Orka and explicit image-http profiles use the shared release engine. Standalone agents are extraction inputs only.\nR opens recent read-only Orka runs (caller must list/get Tasks and get the kube-system Namespace).\nUse ? for keys, t for contexts, a for AKS/ACR discovery and selection. ACR roles are candidates, not proof of publisher or node pull access.\nUse i for source/case review, p for explicit publication, e for full evaluation and l for reviewed Lift. Image bindings remain author-owned environment files.\nNo demo mode or automatic teardown."}
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if err := validateContainerEngineFlag(cmd, args); err != nil {
			return err
		}
		if opt.Deployment != "" {
			return app.ValidateSuiteDeploymentID(opt.Deployment)
		}
		return nil
	}
	cmd.Long += "\nCreation (n): name → inference/model/instructions → Create agent, using Orka by default. Final review offers Change execution and Change location; the folder defaults to the agent name. Inference pickers offer Ready Providers or existing Foundry deployments, with advanced manual entry. Subscription selection starts in fuzzy search across name/GUID and prefers the last-used available subscription; arrows navigate results. Discovery does not provision cluster credentials. Backspace at start-of-text/pickers and Alt+Left navigate back, preserving drafts."
	cmd.Flags().StringVar(&opt.LocalContext, "local-context", "", "local kind context (configured/saved preference by default)")
	cmd.Long += "\nGuided Lift selects destination, namespace, inference and registry, offers explicit prerequisite setup/publication/evaluation, then reviews the exact shared plan. Image bindings are generated from selections. Offline layouts and external bindings remain Advanced. Connectivity is not push/pull authorization."
	cmd.Long += "\nThe kind list shows only observed Ready agents and verified Ready image placements in that context. w switches to a separate local source-files view; open/create enters authoring. Ready does not imply a running worker Pod or a fresh answer."
	cmd.Long += "\nLists default to recent edits/creation first. S chooses sort order; f fuzzy-finds agents/models in the focused pane. Numbered rows show inference and matching/total counts. Missing source hints are reported separately from live agents."
	cmd.Long += "\nLift (l) offers fuzzy published-version selection, newest locally known first; manual digest entry is advanced. Selection never bypasses artifact/evidence validation."
	cmd.Long += "\nMouse clicks select tabs/rows/actions and wheel scrolls; confirmations stay explicit. Independent Orka agents offer interactive c/chat. Foundry creation review offers confirmed create-only Provider/Secret setup; new keys come from a named host environment variable, never a text entry."
	cmd.Long += "\nEmpty panes offer New agent with Enter. t searches contexts for the focused side; local offers existing kind access and confirmed local initialization, remote n discovers AKS clusters and confirms private access retrieval. Unavailable placement records do not hide contexts."
	cmd.Long += "\nLocal Copilot CLI creation uses your installed CLI/login with discovered models. Workspace u/c run/chat on the host; source edits require review and H reconnect. No cluster Task or evaluation evidence is produced."
	cmd.Long += "\nFoundry resources/deployments and Providers accept immediate fuzzy typing. AKS/ACR and named destination fields use fuzzy completion; Enter selects the highlighted match."
	cmd.Flags().StringVar(&opt.RemoteContext, "remote-context", "", "remote context (configured/saved preference by default)")
	cmd.Flags().StringVar(&opt.Namespace, "namespace", app.OrkaNamespace, "namespace to observe")
	cmd.Flags().StringVar(&opt.Workspace, "workspace", "", "initial suite workspace")
	cmd.Flags().StringVar(&opt.Deployment, "deployment", "", "retained native or image placement ID")
	cmd.Flags().StringVar(&opt.Suite.Registry, "registry", "", "image publication registry/repository reference")
	cmd.Flags().StringVar(&opt.Suite.Build.Builder, "builder", "", "Docker buildx builder")
	cmd.Flags().BoolVar(&opt.Suite.PlainHTTP, "plain-http", false, "explicit HTTP registry transport")
	cmd.Flags().BoolVar(&attestations, "attestations", true, "request image SBOM/provenance")
	cmd.Flags().BoolVar(&opt.Suite.Build.RequireAttestations, "require-attestations", false, "require image attestations")
	_ = cmd.RegisterFlagCompletionFunc("local-context", completeContexts)
	_ = cmd.RegisterFlagCompletionFunc("remote-context", completeContexts)
	cmd.RunE = appRun(state, func(a *app.App) error {
		opt.Suite.Workspace = opt.Workspace
		opt.Suite.Build.DisableAttestations = !attestations
		return a.AgentTUI(opt)
	})
	return cmd
}
