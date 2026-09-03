package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/admin"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/app"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
)

// extractContext pulls a global `--context <name>` (or `--context=<name>`,
// and the single-dash spellings) out of the argument list wherever it
// appears.
//
// It is global rather than per-subcommand because it decides WHERE a command
// lands, and an operator who has just been shown a guard banner naming the
// wrong cluster should be able to append `--context ...` to the command they
// already typed without first learning which side of the verb it goes on.
func extractContext(argv []string) ([]string, string, error) {
	var kept []string
	value := ""
	seen := false
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		name, inline, hasInline := strings.Cut(arg, "=")
		if name != "--context" && name != "-context" {
			kept = append(kept, arg)
			continue
		}
		seen = true
		if hasInline {
			value = inline
			continue
		}
		if i+1 >= len(argv) {
			return nil, "", fmt.Errorf("--context needs a context name")
		}
		i++
		value = argv[i]
	}
	// An empty --context must be an error, not a silent fall-through to
	// KUBE_CTX or the default: `--context=$CTX` with an unset CTX is someone
	// aiming at a cluster they cannot name, and guessing on their behalf is
	// exactly what the guard exists to prevent.
	if seen && strings.TrimSpace(value) == "" {
		return nil, "", fmt.Errorf("--context needs a context name")
	}
	return kept, value, nil
}

// joinArgs joins the remaining words of `kmx agent chat <name> ...` into one
// message, so a question does not have to be quoted.
func joinArgs(args []string) string { return strings.Join(args, " ") }

func joinSteps() string { return strings.Join(app.UpSteps, ", ") }

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("kmx "+name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

// parseInterspersed parses flags that may appear on either side of the
// positional arguments.
//
// The flag package stops at the first non-flag word, so `kmx agent create
// billing --tools server:tool` would otherwise leave the flags unparsed and
// report a usage error about the arguments it had just refused to read —
// which is exactly what it did, in the first end-to-end run of this command.
// Nobody writes `kmx agent create --tools server:tool billing`; the name goes
// first. So: parse, take the positional that stopped it, parse the rest, and
// repeat.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		rest = fs.Args()[1:]
	}
}

// The argument parsing for milestone 3's verbs.
//
// These are functions rather than inline cases for one reason: Go's `flag`
// stops at the first non-flag word, and milestone 1 shipped a command that
// silently dropped every flag written after the positional and then reported
// a usage error about the arguments it had just refused to read. That is a
// defect an operator finds, not a compiler — so each verb's parsing is a
// function with a test per argument ORDER.

// parseUse reads `kmx use <preset> [--agent <name>]`.
func parseUse(args []string) (string, app.UseOptions, error) {
	var opt app.UseOptions
	fs := newFlagSet("use")
	fs.StringVar(&opt.Agent, "agent", config.DefaultAgent, "the agent to switch")
	rest, err := parseInterspersed(fs, args)
	if err != nil {
		return "", opt, err
	}
	if len(rest) != 1 {
		return "", opt, errors.New("usage: kmx use <preset> [--agent <name>]")
	}
	return rest[0], opt, nil
}

// parseBudget reads `kmx budget [<credential>] [--cents n|-] [--tokens n|-]`.
//
// Neither flag means BOTH caps are cleared, which is what `make budget` with
// no CAP_* does and what CI relies on. "-" spells the same thing, so a recipe
// passing an empty variable through does not have to know whether the
// operator set it.
func parseBudget(args []string, fallback string) (string, *int64, *int64, error) {
	fs := newFlagSet("budget")
	cents := fs.String("cents", "-", "monthly cap in cents ('-' for none)")
	tokens := fs.String("tokens", "-", "monthly cap in tokens ('-' for none)")
	names, err := parseInterspersed(fs, args)
	if err != nil {
		return "", nil, nil, err
	}
	credential := fallback
	switch len(names) {
	case 0:
	case 1:
		credential = names[0]
	default:
		return "", nil, nil, errors.New("usage: kmx budget [<credential>] [--cents n|-] [--tokens n|-]")
	}
	capCents, err := admin.ParseCap("cents cap", *cents)
	if err != nil {
		return "", nil, nil, err
	}
	capTokens, err := admin.ParseCap("token cap", *tokens)
	if err != nil {
		return "", nil, nil, err
	}
	return credential, capCents, capTokens, nil
}

// parseApprove reads `kmx approve <id> [--ttl 10m] [--uses 1] [--amount n]`.
func parseApprove(args []string) (id string, ttl, uses, amount *int64, err error) {
	fs := newFlagSet("approve")
	ttlFlag := fs.String("ttl", "-", "expiry, e.g. 90, 90s, 5m, 2h, 1d")
	usesFlag := fs.String("uses", "-", "how many times the grant may be used")
	amountFlag := fs.String("amount", "-", "tokens or cents (budget requests only)")
	ids, err := parseInterspersed(fs, args)
	if err != nil {
		return "", nil, nil, nil, err
	}
	if len(ids) != 1 {
		return "", nil, nil, nil, errors.New("usage: kmx approve <id> [--ttl 10m] [--uses 1] [--amount n]")
	}
	if ttl, err = admin.ParseTTL(*ttlFlag); err != nil {
		return "", nil, nil, nil, err
	}
	if uses, err = admin.ParseCap("uses", *usesFlag); err != nil {
		return "", nil, nil, nil, err
	}
	if amount, err = admin.ParseCap("amount", *amountFlag); err != nil {
		return "", nil, nil, nil, err
	}
	return ids[0], ttl, uses, amount, nil
}

// parseRenew reads `kmx credential renew <name> [--ttl 720h]`. An absent
// --ttl takes the plane's default lifetime; there is no way to ask for
// "never", because a credential with no expiry is the legacy class and
// that class only shrinks.
func parseRenew(args []string) (name string, ttl *int64, err error) {
	fs := newFlagSet("credential renew")
	ttlFlag := fs.String("ttl", "-", "new lifetime from now, e.g. 90s, 5m, 2h, 30d")
	rest, err := parseInterspersed(fs, args)
	if err != nil {
		return "", nil, err
	}
	if len(rest) != 1 {
		return "", nil, errors.New("usage: kmx credential renew <name> [--ttl 720h]")
	}
	if err := admin.ValidCredentialName(rest[0]); err != nil {
		return "", nil, err
	}
	if ttl, err = admin.ParseTTL(*ttlFlag); err != nil {
		return "", nil, err
	}
	return rest[0], ttl, nil
}

// parseRequest reads
// `kmx request <tool|budget|inbound> <subject> [--credential <name>] [--args <json>]`.
//
// The kind and the subject are positional because they are what the request
// IS; the credential is a flag because its default depends on the kind — tool
// requests are filed against the tools credential, everything else against
// the chat one, which is the Makefile's REQ_CRED rule.
func parseRequest(args []string, credential, toolsCredential string) (string, string, string, map[string]any, error) {
	fs := newFlagSet("request")
	name := fs.String("credential", "", "the credential the request is filed against")
	argsJSON := fs.String("args", "", "the tool call's arguments, as a JSON object (tool requests only)")
	rest, err := parseInterspersed(fs, args)
	if err != nil {
		return "", "", "", nil, err
	}
	if len(rest) != 2 {
		return "", "", "", nil, errors.New(
			"usage: kmx request <tool|budget|inbound> <subject> [--credential <name>] [--args <json>]")
	}
	kind, subject := rest[0], rest[1]
	filedBy := strings.TrimSpace(*name)
	if filedBy == "" {
		filedBy = credential
		if kind == "tool" {
			filedBy = toolsCredential
		}
	}

	// --args names the CALL to pre-approve (P12). It is validated here so a
	// typo fails before the admin port sees it; the plane computes the
	// digest with the gateway's own code, so this request and the agent's
	// retry are provably the same call. UseNumber so a large integer
	// argument is not re-encoded through a float.
	var callArgs map[string]any
	if strings.TrimSpace(*argsJSON) != "" {
		decoder := json.NewDecoder(strings.NewReader(*argsJSON))
		decoder.UseNumber()
		if err := decoder.Decode(&callArgs); err != nil || callArgs == nil {
			return "", "", "", nil, fmt.Errorf("invalid --args (want a JSON object): %s", *argsJSON)
		}
		// A trailing token means the operator quoted only half of it.
		if decoder.More() {
			return "", "", "", nil, fmt.Errorf("invalid --args (want ONE JSON object): %s", *argsJSON)
		}
	}
	return filedBy, kind, subject, callArgs, nil
}

// parseTools reads the enforcing MCP gateway's verbs.
//
// They are `kmx tools <verb>` rather than four top-level commands because
// they are one subject with four operations on it, and because `kmx govern`
// already means "put an agent behind the plane for MODEL spend" — a
// top-level `kmx govern-tools` would read as a variant of it when it is the
// same idea applied to a different enforcement point. `kmx agent`
// established the noun-then-verb shape in milestone 1.
//
// The returned options always carry a resolved credential, so no caller has
// to remember the CRED_TOOLS fallback.
func parseTools(args []string, toolsCredential string) (string, app.ToolsOptions, []string, error) {
	const usage = "usage: kmx tools govern|allow <tools>|allowlist [<credential>]|ungovern"
	var opt app.ToolsOptions
	if len(args) == 0 {
		return "", opt, nil, errors.New(usage)
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "govern", "ungovern", "allow", "allowlist":
	default:
		return "", opt, nil, fmt.Errorf("kmx tools: unknown verb %q. %s", verb, usage)
	}

	fs := newFlagSet("tools " + verb)
	fs.StringVar(&opt.Credential, "credential", "", "the kmh_ credential the gateway admits (CRED_TOOLS)")
	// `--agent` is govern's only. `ungovern` re-applies ONE committed
	// manifest, which names one agent, so an --agent it could not honour
	// should not be spellable.
	if verb == "govern" {
		fs.StringVar(&opt.Agent, "agent", "", "the agent to repoint")
		fs.StringVar(&opt.Secret, "secret", "", "agent-side Secret the issued token is stored in")
		fs.StringVar(&opt.SecretNamespace, "secret-namespace", "", "namespace for that Secret")
		fs.StringVar(&opt.Tools, "tools", "", "allowlist, comma-separated ('-' for empty: nothing callable)")
	}
	positional, err := parseInterspersed(fs, rest)
	if err != nil {
		return "", opt, nil, err
	}

	switch verb {
	case "govern":
		if len(positional) != 0 {
			return "", opt, nil, errors.New(
				"usage: kmx tools govern [--tools a,b|-] [--credential <name>] [--agent <name>]")
		}
	case "ungovern":
		if len(positional) != 0 {
			return "", opt, nil, errors.New("usage: kmx tools ungovern")
		}
	case "allow":
		if len(positional) != 1 {
			return "", opt, nil, errors.New("usage: kmx tools allow <tool,tool|-> [--credential <name>]")
		}
	case "allowlist":
		switch len(positional) {
		case 0:
		case 1:
			// The credential may be named positionally here, because this
			// is a read of ONE credential and that is its whole argument.
			opt.Credential = positional[0]
		default:
			return "", opt, nil, errors.New("usage: kmx tools allowlist [<credential>]")
		}
	}
	if strings.TrimSpace(opt.Credential) == "" {
		opt.Credential = toolsCredential
	}
	return verb, opt, positional, nil
}

// parseOptionalFile reads a command whose only argument is an optional path.
func parseOptionalFile(command string, args []string) (string, error) {
	fs := newFlagSet(command)
	rest, err := parseInterspersed(fs, args)
	if err != nil {
		return "", err
	}
	if len(rest) > 1 {
		return "", fmt.Errorf("usage: kmx %s [<file>]", command)
	}
	if len(rest) == 1 {
		return rest[0], nil
	}
	return "", nil
}

// parseMetrics reads `kmx metrics [--pod <name>]`.
func parseMetrics(args []string) (string, error) {
	fs := newFlagSet("metrics")
	pod := fs.String("pod", "", "the proxy replica to read (default: the first Ready one)")
	rest, err := parseInterspersed(fs, args)
	if err != nil {
		return "", err
	}
	if len(rest) != 0 {
		return "", errors.New("usage: kmx metrics [--pod <name>]")
	}
	return *pod, nil
}
