package main

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/app"
)

func newAgentCommand(state *commandState) *cobra.Command {
	group := &cobra.Command{Use: "agent", Short: "Create, inspect, edit, and chat with agents"}
	group.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return errors.New("usage: kmx agent list | kmx agent create [<name>] | kmx agent edit <name> | kmx agent chat <name> [message]")
		}
		a, err := state.application()
		if err != nil {
			return err
		}
		return app.RefuseUnknownAgentVerb(args[0], a.Cfg.KubeContext)
	}
	group.AddCommand(newAgentListCommand(state), newAgentCreateCommand(state), newAgentEditCommand(state), newAgentChatCommand(state))
	return group
}

func newAgentListCommand(state *commandState) *cobra.Command {
	var output string
	cmd := &cobra.Command{Use: "list", Short: "List agents and active wiring", Args: cobra.NoArgs}
	cmd.Flags().StringVarP(&output, "output", "o", "table", "output: table|json|yaml")
	_ = cmd.RegisterFlagCompletionFunc("output", staticCompletion([]string{"table", "json", "yaml"}))
	cmd.RunE = appRun(state, func(a *app.App) error { return a.ListAgents(output) })
	return cmd
}

func newAgentCreateCommand(state *commandState) *cobra.Command {
	var opt app.CreateOptions
	cmd := &cobra.Command{Use: "create [name]", Short: "Scaffold and optionally apply an Agent", Args: usageArgs(0, 1, "kmx agent create [<name>] [flags]")}
	cmd.Flags().StringVar(&opt.Namespace, "namespace", "", "Agent namespace (default kagent)")
	cmd.Flags().StringVar(&opt.Description, "description", "", "one-line description")
	cmd.Flags().StringVar(&opt.ModelConfig, "model", "", "ModelConfig to think with")
	cmd.Flags().StringVar(&opt.Instructions, "instructions", "", "file containing the system message")
	cmd.Flags().StringVar(&opt.Tools, "tools", "", "MCP wiring: <server>:<tool>[,<tool>...]")
	cmd.Flags().StringVar(&opt.Out, "out", "", "manifest output path ('-' for stdout)")
	cmd.Flags().BoolVar(&opt.NoApply, "no-apply", false, "write the manifest and stop")
	cmd.Flags().BoolVar(&opt.DryRun, "dry-run", false, "server-side validation without applying")
	cmd.RunE = appRun(state, func(a *app.App) error {
		if len(cmd.Flags().Args()) == 0 {
			return a.CreateAgentInteractive(opt)
		}
		opt.Name = cmd.Flags().Arg(0)
		return a.CreateAgent(opt)
	})
	return cmd
}

func newAgentEditCommand(state *commandState) *cobra.Command {
	var file string
	cmd := &cobra.Command{Use: "edit <name>", Short: "Edit and validate local Agent source", Args: usageArgs(1, 1, "kmx agent edit <name> [--file <path>]")}
	cmd.Flags().StringVar(&file, "file", "", "local Agent manifest")
	cmd.ValidArgsFunction = completeLocalAgents
	cmd.RunE = appRun(state, func(a *app.App) error { return a.EditAgent(cmd.Flags().Arg(0), file) })
	return cmd
}

func newAgentChatCommand(state *commandState) *cobra.Command {
	var asJSON, interactive bool
	var session string
	cmd := &cobra.Command{Use: "chat <name> [message...]", Short: "Chat with an Agent", Args: usageArgs(1, -1, "kmx agent chat [--json] [--interactive] [--session <id>] <name> [message]")}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print raw A2A task")
	cmd.Flags().BoolVar(&interactive, "interactive", false, "keep one streamed session open")
	cmd.Flags().StringVar(&session, "session", "", "resume this kagent session")
	cmd.ValidArgsFunction = completeLiveAgents
	cmd.RunE = appRun(state, func(a *app.App) error {
		args := cmd.Flags().Args()
		a.ChatJSON(asJSON)
		return a.ChatWithOptions(app.ChatOptions{Agent: args[0], Task: joinArgs(args[1:]), Interactive: interactive, Session: session})
	})
	return cmd
}
