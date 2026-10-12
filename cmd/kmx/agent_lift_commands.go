package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/spf13/pflag"
	"oras.land/oras-go/v2/registry"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/app"
)

func newAgentLiftCommand(state *commandState) *cobra.Command {
	var opt app.LiftAgentBundleOptions
	var release, environment string
	cmd := &cobra.Command{
		Use:   "lift [bundle-dir] [--release <ref@digest>]",
		Short: "Lift an agent bundle: Orka reconcile or explicit Kagent create-only deployment",
	}
	cmd.Flags().StringVar(&release, "release", "", "immutable AgentSuite registry reference (deploy-only)")
	cmd.Flags().StringVar(&environment, "environment", "", "image execution target/inference bindings JSON")
	cmd.Args = func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("release") {
			ref, err := registry.ParseReference(release)
			if err != nil {
				return fmt.Errorf("--release requires registry/repository@sha256:digest")
			}
			if err := ref.ValidateReferenceAsDigest(); err != nil || !strings.Contains(release, "@sha256:") {
				return fmt.Errorf("--release requires an immutable SHA256 reference")
			}
			if len(args) != 0 || environment == "" {
				return fmt.Errorf("release lift requires --environment and no bundle argument")
			}
			var invalid string
			cmd.Flags().Visit(func(f *pflag.Flag) {
				switch f.Name {
				case "release", "environment", "plan":
				default:
					invalid = f.Name
				}
			})
			if invalid != "" {
				return fmt.Errorf("--%s belongs to bundle lift, not --release", invalid)
			}
			if state.contextFlag != "" {
				return fmt.Errorf("release target comes from --environment; omit --context")
			}
			return nil
		}
		if cmd.Flags().Changed("environment") {
			return fmt.Errorf("--environment requires --release")
		}
		return usageArgs(1, 1, "kmx agent lift <bundle-dir> [flags]")(cmd, args)
	}
	cmd.Flags().StringVar(&opt.Runtime, "runtime", "", "runtime (default: orka; kagent requires exact --runtime-version)")
	cmd.Flags().StringVar(&opt.RuntimeVersion, "runtime-version", "", "explicit runtime version (kagent: v0.10.3)")
	cmd.Flags().StringVar(&opt.Bindings, "bindings", "", "KagentBindings file (default: bundle bindings.yaml)")
	cmd.Flags().StringVar(&opt.ToContext, "to-context", "", "destination kubeconfig context (Orka: remembered after successful lift; Kagent: always explicit)")
	cmd.Flags().StringVar(&opt.ToNamespace, "to-namespace", "", "destination namespace (Orka: remembered or orka-system; Kagent: must match KagentBindings)")
	cmd.Flags().StringVar(&opt.Inference, "inference", "", "Orka only: existing destination inference Provider: provider:<name>")
	cmd.Flags().StringVar(&opt.RequireEvaluated, "require-evaluated", "", "Orka only: require a passing evaluation receipt for this context label's recorded cluster and namespace")
	cmd.Flags().StringVar(&opt.OverrideGate, "override-gate", "", "Orka only: lift despite a failed evaluation gate and record this reason in the lift receipt")
	cmd.Flags().BoolVar(&opt.Plan, "plan", false, "inspect the selected runtime's lift preflights without writing resources or receipts")
	run := appRun(state, func(a *app.App) error {
		if release != "" {
			env, err := app.ReadSuiteEnvironment(environment)
			if err != nil {
				return err
			}
			prepared, err := a.PrepareReleaseLift(cmd.Context(), release, env)
			if err != nil {
				return err
			}
			if opt.Plan {
				return json.NewEncoder(a.Out).Encode(prepared.Summary())
			}
			if os.Getenv("KAIMAHI_CONFIRM") != env.Context {
				return fmt.Errorf("set KAIMAHI_CONFIRM=%s after reviewing --plan", env.Context)
			}
			receipt, err := a.ApplyReleaseLift(cmd.Context(), release, env, prepared, prepared.Summary().PlanDigest)
			return errors.Join(err, json.NewEncoder(a.Out).Encode(receipt))
		}
		opt.BundleDir = cmd.Flags().Arg(0)
		return a.LiftAgentBundle(opt)
	})
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		for _, flag := range []struct{ name, value string }{{"require-evaluated", opt.RequireEvaluated}, {"override-gate", opt.OverrideGate}} {
			if cmd.Flags().Changed(flag.name) && strings.TrimSpace(flag.value) == "" {
				return fmt.Errorf("--%s requires a non-empty value", flag.name)
			}
		}
		return run(cmd, args)
	}
	return cmd
}
