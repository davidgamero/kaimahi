package main

import (
	"errors"
	"fmt"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite/workspace"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/app"
	"github.com/spf13/cobra"
	"os"
)

func newConsoleCommand(state *commandState) *cobra.Command {
	var opt app.AgentTUIOptions
	var suite app.SuiteConsoleOptions
	var attestations bool
	cmd := &cobra.Command{
		Use: "console", Short: "Open the interactive workspace for agents and environments", Args: cobra.NoArgs,
		Long: "Browse one local kind and one remote Kubernetes environment side by side.\nThe console lists, creates, chats with and edits native Orka Agents only.\nUse h/j/k/l or arrows to navigate; / opens commands and argument completion.\nR opens recent read-only Orka runs for the selected Agent (caller must list/get Tasks and get the kube-system Namespace).\n/lift offers local Orka agents, remote contexts, and a new AKS environment.\nUse --demo to explore with sample data and no cluster access.",
	}
	cmd.Long += "\n\nInside a workspace.yaml ancestor (or with --workspace), open the shared suite workspace. Image members use agent lift --release; native image requirements remain empty. --registry selects the publication repository:tag."
	cmd.Flags().StringVar(&opt.LocalContext, "local-context", "", "local kind context (default: last TUI selection, configured context, or first local)")
	cmd.Flags().StringVar(&opt.RemoteContext, "remote-context", "", "remote context (default: last TUI selection, last lift target, configured context, or first remote)")
	cmd.Flags().StringVar(&opt.Namespace, "namespace", app.OrkaNamespace, "Orka namespace to display")
	cmd.Flags().StringVar(&opt.Bundles, "bundles", "", "directory holding one agent bundle per agent, compared by b (default: agents)")
	cmd.Flags().BoolVar(&opt.Demo, "demo", false, "use sample agents; no cluster or cloud operations")
	cmd.Flags().StringVar(&suite.Workspace, "workspace", "", "experimental image-suite source workspace")
	cmd.Flags().StringVar(&suite.Registry, "registry", "", "container registry/repository prefix")
	cmd.Flags().BoolVar(&suite.PlainHTTP, "plain-http", false, "development registry HTTP")
	cmd.Flags().StringVar(&suite.Build.Builder, "builder", "", "Docker buildx builder for image suites")
	cmd.Flags().BoolVar(&attestations, "attestations", true, "request image SBOM and provenance")
	cmd.Flags().BoolVar(&suite.Build.RequireAttestations, "require-attestations", false, "require image attestations")
	_ = cmd.RegisterFlagCompletionFunc("local-context", completeContexts)
	_ = cmd.RegisterFlagCompletionFunc("remote-context", completeContexts)
	cmd.RunE = appRun(state, func(a *app.App) error {
		if suite.Workspace == "" && !opt.Demo {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			root, err := workspace.Discover(cwd, "")
			if err == nil {
				suite.Workspace = root
			} else if !errors.Is(err, workspace.ErrNotFound) {
				return err
			}
		}
		if suite.Workspace != "" {
			if opt.Demo {
				return fmt.Errorf("--workspace performs real operations; omit --demo")
			}
			suite.Build.DisableAttestations = !attestations
			return a.SuiteConsole(suite)
		}
		return a.AgentTUI(opt)
	})
	return cmd
}
