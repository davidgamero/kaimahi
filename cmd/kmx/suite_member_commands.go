package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/app"
	"github.com/spf13/cobra"
)

func newSuiteExtractCommand(state *commandState) *cobra.Command {
	var o app.SuiteExtractOptions
	var runtime string
	var plan bool
	c := &cobra.Command{Use: "extract <agent>", Short: "Extract a standalone Orka Agent into a new suite workspace; keep the original", Args: cobra.ExactArgs(1)}
	c.Flags().StringVar(&o.Namespace, "namespace", "", "explicit source namespace")
	c.Flags().StringVar(&o.Workspace, "workspace", "", "new workspace directory (must not exist)")
	c.Flags().StringVar(&o.Name, "name", "", "distinct new member name")
	c.Flags().BoolVar(&plan, "plan", false, "read and review source without writing files")
	c.Flags().StringVar(&runtime, "runtime", "orka", "extraction runtime (only native Orka supported)")
	c.PreRunE = func(cmd *cobra.Command, args []string) error {
		if err := validateContainerEngineFlag(cmd, args); err != nil {
			return err
		}
		if runtime != "orka" {
			return fmt.Errorf("extract supports only native Orka")
		}
		if state.contextFlag == "" {
			return fmt.Errorf("extract requires explicit --context")
		}
		o.Agent = args[0]
		return app.ValidateSuiteExtractOptions(o)
	}
	c.RunE = appRun(state, func(a *app.App) error {
		p, err := a.PrepareSuiteExtraction(c.Context(), o)
		if err != nil {
			return err
		}
		if !plan {
			if err = a.ExtractSuite(c.Context(), p); err != nil {
				return err
			}
			p.Summary.State = "extracted; original unchanged; example cases require review; unevaluated"
		}
		return json.NewEncoder(c.OutOrStdout()).Encode(p.Summary)
	})
	return c
}

func newSuiteShowCommand() *cobra.Command {
	var output string
	c := &cobra.Command{Use: "show <workspace>", Short: "Show the prepared suite member and ordered case inventory offline", Args: cobra.ExactArgs(1)}
	c.Flags().StringVarP(&output, "output", "o", "text", "text or json")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if output != "text" && output != "json" {
			return fmt.Errorf("output must be text or json")
		}
		return app.ShowSuiteWorkspace(cmd.Context(), args[0], output, cmd.OutOrStdout())
	}
	return c
}
func newSuiteStatusCommand(state *commandState) *cobra.Command {
	var id, output string
	c := &cobra.Command{Use: "status", Short: "Revalidate a retained native suite placement", Args: cobra.NoArgs}
	c.Flags().StringVar(&id, "deployment", "", "private placement ID")
	c.Flags().StringVarP(&output, "output", "o", "text", "text or json")
	c.PreRunE = func(cmd *cobra.Command, args []string) error {
		if err := validateContainerEngineFlag(cmd, args); err != nil {
			return err
		}
		if state.contextFlag != "" || cmd.Flags().Changed("context") {
			return fmt.Errorf("suite status uses its retained destination, not --context")
		}
		if strings.TrimSpace(id) == "" || output != "text" && output != "json" {
			return fmt.Errorf("status requires --deployment and text/json output")
		}
		return app.ValidateSuiteDeploymentID(id)
	}
	c.RunE = appRun(state, func(a *app.App) error { return a.WriteSuiteDeploymentStatus(c.Context(), id, output) })
	return c
}
