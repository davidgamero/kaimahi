// Command kmx is one entry point for the developer journey this repository
// has shipped since P1: a cluster, a model, kagent, two agents, a
// conversation — and the same journey CI runs, because the Makefile now
// delegates to this binary rather than keeping a second copy of it.
//
// Install (the only install path in milestone 1 — nothing is published,
// D26/D27):
//
//	go install github.com/kaimahi-agents/kaimahi/cmd/kmx@<sha>
//
// kmx does not duplicate kagent's CLI. `kmx agent chat` is a passthrough to
// `kagent invoke`, and the pinned kagent binary is fetched and
// checksum-verified exactly as the Makefile fetches it. What kmx owns is the
// part kagent's CLI does not: bringing up this project's cluster, this
// project's model and agents, and doing it behind a context guard.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/app"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
)

const usage = `kmx — create and run governed agents on Kubernetes.

USAGE
  kmx <command> [flags]

COMMANDS
  ctx [<context>]              show, or select, the kube context kmx acts on
  up                           kind cluster + Ollama + the model + kagent + the agents
  agent create <name>          scaffold agents/<name>.yaml and apply it
  agent chat <name> [message]  ask an agent one question (via ` + "`kagent invoke`" + `)
  status                       agents, modelconfigs and pods
  down                         delete the kind cluster kmx created
  version                      print the pinned versions kmx installs

GLOBAL FLAGS
  --context <name>   act on this kube context for one command (beats KUBE_CTX)

ENVIRONMENT (the Makefile's names, with the Makefile's defaults)
  KIND_CLUSTER      cluster to create/delete            (kaimahi-p1)
  KUBE_CTX          context to act on                   (kind-$KIND_CLUSTER)
  CONTAINER_ENGINE  docker | podman                     (docker)
  KAGENT_VERSION    pinned kagent chart and CLI         (0.9.12)
  MODEL             model pulled into Ollama            (qwen2.5:3b)
  CHAT_PORT         local port for the controller       (8083)
  KAIMAHI_CONFIRM   confirm a non-kind context by name  (unset)

NOT IN THIS MILESTONE
  The governance plane, ` + "`govern`" + `, secret capture, AKS and the probes stay in
  the Makefile. See docs/kmx.md.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(argv []string) error {
	// A global --context may appear before or after the command, so it is
	// pulled out of the argument list rather than parsed by a FlagSet that
	// would have to be threaded through every subcommand.
	argv, contextFlag, err := extractContext(argv)
	if err != nil {
		return err
	}
	if len(argv) == 0 {
		fmt.Print(usage)
		return nil
	}

	command, args := argv[0], argv[1:]
	switch command {
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	case "version":
		fmt.Printf("kmx (kaimahi milestone 1)\n  kagent   %s\n  model    %s\n",
			config.DefaultKagentVersion, config.DefaultModel)
		return nil
	}

	cfg, err := config.Load(contextFlag)
	if err != nil {
		return err
	}
	a := app.New(cfg)

	switch command {
	case "ctx":
		if len(args) > 1 {
			return errors.New("usage: kmx ctx [<context>]")
		}
		if len(args) == 0 {
			return a.Ctx("")
		}
		return a.Ctx(args[0])

	case "up":
		step := ""
		fs := newFlagSet("up")
		fs.StringVar(&step, "step", "", "run one step only: "+joinSteps())
		if err := fs.Parse(args); err != nil {
			return err
		}
		return a.Up(step)

	case "status":
		return a.Status()

	case "down":
		return a.Down()

	case "agent":
		return agentCommand(a, args)

	default:
		return fmt.Errorf("kmx: unknown command %q. Run `kmx help`.", command)
	}
}

func agentCommand(a *app.App, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: kmx agent create <name> | kmx agent chat <name> [message]")
	}
	switch args[0] {
	case "create":
		return agentCreate(a, args[1:])
	case "chat":
		rest := args[1:]
		if len(rest) == 0 {
			return errors.New("usage: kmx agent chat <name> [message]")
		}
		name := rest[0]
		message := ""
		if len(rest) > 1 {
			message = joinArgs(rest[1:])
		}
		return a.Chat(name, message)
	default:
		return app.RefuseUnknownAgentVerb(args[0], a.Cfg.KubeContext)
	}
}

func agentCreate(a *app.App, args []string) error {
	var opt app.CreateOptions
	fs := newFlagSet("agent create")
	fs.StringVar(&opt.Namespace, "namespace", "", "namespace for the Agent (kagent)")
	fs.StringVar(&opt.Description, "description", "", "one-line description")
	fs.StringVar(&opt.ModelConfig, "model", "", "ModelConfig to think with (default: the plane's governed preset if it exists, else the keyless one)")
	fs.StringVar(&opt.Instructions, "instructions", "", "file whose contents become the agent's system message")
	fs.StringVar(&opt.Tools, "tools", "", "MCP wiring as <server>:<tool>[,<tool>...] — the allowlist is mandatory")
	fs.StringVar(&opt.Out, "out", "", "where to write the manifest (default agents/<name>.yaml; '-' for stdout)")
	fs.BoolVar(&opt.NoApply, "no-apply", false, "write the manifest and stop")
	fs.BoolVar(&opt.DryRun, "dry-run", false, "server-side dry run against the live CRDs instead of applying")
	names, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(names) != 1 {
		return errors.New("usage: kmx agent create <name> [flags]")
	}
	opt.Name = names[0]
	return a.CreateAgent(opt)
}
