// Command kmx is one entry point for the journey this repository has shipped
// since P1: a cluster, a model, kagent, two agents, a conversation, the
// governance plane in front of them — and the same journey CI runs, because
// the Makefile's kind path delegates to this binary rather than keeping a
// second copy of it.
//
// Install (the only install path — nothing is published, D26/D27/D28):
//
//	go install github.com/kaimahi-agents/kaimahi/cmd/kmx@<sha>
//
// kmx does not duplicate kagent's CLI. `kmx agent chat` is a passthrough to
// `kagent invoke`, and the pinned kagent binary is fetched and
// checksum-verified exactly as the Makefile fetches it. What kmx owns is the
// part kagent's CLI does not: bringing up this project's cluster, this
// project's model and agents, putting them behind this project's governance
// plane, and doing all of it behind a context guard.
package main

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/app"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/planebuild"
)

const usage = `kmx — create and run governed agents on Kubernetes.

USAGE
  kmx <command> [flags]

COMMANDS
  ctx [<context>]              show, or select, the kube context kmx acts on
  up                           kind cluster + Ollama + the model + kagent + the agents
  agent create <name>          scaffold agents/<name>.yaml and apply it
  agent chat <name> [message]  ask an agent one question (via ` + "`kagent invoke`" + `)
                               add --json for the raw A2A task; piped output
                               is always raw; --interactive keeps a session open
  plane                        deploy the governance plane (proxy + ledger)
  govern [<credential>]        issue the credential and put an agent behind the plane
  ledger [<credential>]        the spend ledger and month-to-date totals
  grants [<credential>]        grants, with liveness
  audit tool|approval [<cred>] the enforcement points' audit trails
  status                       grouped agent/model wiring and runtime health
  down                         delete the kind cluster kmx created
  version                      print the pinned versions kmx installs

GLOBAL FLAGS
  --context <name>   act on this kube context for one command (beats KUBE_CTX)

ENVIRONMENT (the names the Makefile and the scripts already use)
  KIND_CLUSTER      cluster to create/delete            (kaimahi-p1)
  KUBE_CTX          context to act on                   (kind-$KIND_CLUSTER)
  CONTAINER_ENGINE  docker | podman                     (docker)
  KAGENT_VERSION    pinned kagent chart and CLI         (0.9.12)
  MODEL             model pulled into Ollama            (qwen2.5:3b)
  CHAT_PORT         local controller port; unset = free port automatically
  ADMIN_PORT        local port for the plane's admin    (19091)
  CRED              credential govern issues, ledger reads  (hello-world)
  KAIMAHI_CONFIRM   confirm a non-kind context by name  (unset)

NOT IN THIS MILESTONE
  Budgets, approvals, backup/restore, the Slack/GitHub/inbound connectors,
  secret capture, AKS and the probes stay in the Makefile. See docs/kmx.md.
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
		// The plane's revision is kmx's own — that is the contract of the
		// clone-free path — so print what `kmx plane` would fetch, or why
		// it cannot. An operator asking "which plane will this deploy?"
		// should not have to run a deploy to find out.
		revision := "unknown (kmx plane needs --source <checkout>)"
		if rev, err := planebuild.Revision(debug.ReadBuildInfo()); err == nil {
			revision = rev
		}
		fmt.Printf("kmx (kaimahi milestone 2)\n  kagent   %s\n  model    %s\n  plane    %s, built from %s\n",
			config.DefaultKagentVersion, config.DefaultModel, app.PlaneImage, revision)
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

	case "plane":
		var opt app.PlaneOptions
		fs := newFlagSet("plane")
		fs.StringVar(&opt.Step, "step", "", "run one step only: "+strings.Join(app.PlaneSteps, ", "))
		fs.StringVar(&opt.Source, "source", "", "build the plane from this checkout instead of fetching it ('-' forces the fetch)")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("usage: kmx plane [--step <step>] [--source <path>]")
		}
		return a.Plane(opt)

	case "govern":
		opt := app.GovernOptions{
			Agent:           config.DefaultAgent,
			Preset:          config.GovernedModelConfig,
			Secret:          config.GovernedSecret,
			SecretNamespace: config.DefaultNamespace,
		}
		fs := newFlagSet("govern")
		fs.StringVar(&opt.Agent, "agent", opt.Agent, "the agent to put behind the plane")
		fs.StringVar(&opt.Preset, "preset", opt.Preset, "the governed ModelConfig to switch it to")
		fs.StringVar(&opt.Secret, "secret", opt.Secret, "agent-side Secret the issued token is stored in")
		fs.StringVar(&opt.SecretNamespace, "secret-namespace", opt.SecretNamespace, "namespace for that Secret")
		names, err := parseInterspersed(fs, args)
		if err != nil {
			return err
		}
		credential := a.Cfg.Credential
		switch len(names) {
		case 0:
		case 1:
			credential = names[0]
		default:
			return errors.New("usage: kmx govern <credential> [flags]")
		}
		return a.Govern(credential, opt)

	case "ledger":
		credential, err := optionalCredential("ledger", args, a.Cfg.Credential)
		if err != nil {
			return err
		}
		return a.Ledger(credential)

	case "grants":
		// Unlike the ledger, grants default to ALL credentials: a grant is
		// authority someone was given, and the question an operator asks is
		// "what is live anywhere", not "what is live for this one".
		credential, err := optionalCredential("grants", args, "")
		if err != nil {
			return err
		}
		return a.Grants(credential)

	case "audit":
		if len(args) == 0 {
			return errors.New("usage: kmx audit tool|approval [<credential>]")
		}
		credential, err := optionalCredential("audit "+args[0], args[1:], "")
		if err != nil {
			return err
		}
		return a.Audit(args[0], credential)

	case "status":
		fs := newFlagSet("status")
		output := fs.String("o", "table", "output: table|json|yaml")
		fs.StringVar(output, "output", "table", "output: table|json|yaml")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("usage: kmx status [-o table|json|yaml]")
		}
		return a.StatusWithOptions(app.StatusOptions{Output: *output})

	case "down":
		return a.Down()

	case "agent":
		return agentCommand(a, args)

	default:
		return fmt.Errorf("kmx: unknown command %q. Run `kmx help`.", command)
	}
}

// optionalCredential reads the one optional positional a read view takes.
func optionalCredential(command string, args []string, fallback string) (string, error) {
	switch len(args) {
	case 0:
		return fallback, nil
	case 1:
		return args[0], nil
	default:
		return "", fmt.Errorf("usage: kmx %s [<credential>]", command)
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
		fs := newFlagSet("agent chat")
		asJSON := fs.Bool("json", false, "print the raw A2A task instead of the readable view")
		interactive := fs.Bool("interactive", false, "keep one session open for back-and-forth chat")
		session := fs.String("session", "", "resume this kagent session")
		// parseInterspersed, not fs.Parse: flag stops at the first
		// non-flag argument, so `agent chat a "hi" --json` would silently
		// append "--json" to the QUESTION and print the readable view
		// anyway. `agent create` already had this right.
		rest, err := parseInterspersed(fs, args[1:])
		if err != nil {
			return err
		}
		if len(rest) == 0 {
			return errors.New("usage: kmx agent chat [--json] [--interactive] [--session <id>] <name> [message]")
		}
		name := rest[0]
		message := ""
		if len(rest) > 1 {
			message = joinArgs(rest[1:])
		}
		a.ChatJSON(*asJSON)
		return a.ChatWithOptions(app.ChatOptions{Agent: name, Task: message, Interactive: *interactive, Session: *session})
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
