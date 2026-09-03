package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/app"
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
