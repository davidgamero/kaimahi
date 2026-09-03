package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/app"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
)

var topLevelCompletions = []string{
	"ctx", "up", "agent", "plane", "govern", "ledger", "grants", "audit", "status", "down", "version", "help", "completion", "--context",
}

var completionOutput io.Writer = os.Stdout

func printCompletionScript(shell string) error {
	var script string
	switch shell {
	case "bash":
		script = `# bash completion for kmx
_kmx_complete() {
  local -a args
  args=("${COMP_WORDS[@]:1:COMP_CWORD}")
  mapfile -t COMPREPLY < <(command kmx __complete "${args[@]}" 2>/dev/null)
  if ((${#COMPREPLY[@]} == 0)); then
    mapfile -t COMPREPLY < <(compgen -f -- "${COMP_WORDS[COMP_CWORD]}")
  fi
}
complete -F _kmx_complete kmx
`
	case "zsh":
		script = `#compdef kmx
_kmx_complete() {
  local -a candidates
  candidates=("${(@f)$(command kmx __complete "${words[@]:1:$((CURRENT-2))}" "$PREFIX" 2>/dev/null)}")
  candidates=("${(@)candidates:#}")
  if (( ${#candidates[@]} )); then
    compadd -- "${candidates[@]}"
  else
    _files
  fi
}
compdef _kmx_complete kmx
`
	case "fish":
		script = `function __kmx_complete
    set -l words (commandline -opc)
    set -l current (commandline -ct)
    command kmx __complete $words[2..-1] $current 2>/dev/null
end
complete -c kmx -a '(__kmx_complete)'
`
	default:
		return fmt.Errorf("unsupported shell %q — use bash, zsh, or fish", shell)
	}
	fmt.Fprint(completionOutput, script)
	return nil
}

func completeWords(words []string) []string {
	prefix := ""
	committed := words
	if len(words) > 0 {
		prefix = words[len(words)-1]
		committed = words[:len(words)-1]
	}
	contextName, args := completionContext(committed)
	if len(committed) > 0 && (committed[len(committed)-1] == "--context" || committed[len(committed)-1] == "-context") {
		return filterCompletions(kubeContexts(), prefix)
	}
	if strings.HasPrefix(prefix, "--context=") || strings.HasPrefix(prefix, "-context=") {
		flagName, value, _ := strings.Cut(prefix, "=")
		return prefixCompletions(flagName+"=", filterCompletions(kubeContexts(), value))
	}
	if len(args) == 0 {
		return filterCompletions(topLevelCompletions, prefix)
	}

	command := args[0]
	rest := args[1:]
	switch command {
	case "completion":
		return filterCompletions([]string{"bash", "fish", "zsh"}, prefix)
	case "agent":
		if len(rest) == 0 {
			return filterCompletions(append([]string{"list", "create", "chat"}, "--context"), prefix)
		}
		return completeAgent(rest, prefix, contextName)
	case "up":
		return completeFlags(rest, prefix, map[string][]string{"--step": app.UpSteps})
	case "plane":
		return completeFlags(rest, prefix, map[string][]string{"--step": app.PlaneSteps, "--source": nil})
	case "status":
		return completeFlags(rest, prefix, map[string][]string{"-o": {"table", "json", "yaml"}, "--output": {"table", "json", "yaml"}})
	case "govern":
		return completeFlags(rest, prefix, map[string][]string{"--agent": liveAgents(contextName), "--preset": nil, "--secret": nil, "--secret-namespace": nil})
	case "audit":
		if len(nonFlagWords(rest)) == 0 {
			return filterCompletions([]string{"tool", "approval", "--context"}, prefix)
		}
	case "ctx":
		return filterCompletions(kubeContexts(), prefix)
	}
	if strings.HasPrefix(prefix, "-") {
		return filterCompletions([]string{"--context"}, prefix)
	}
	return nil
}

func completeAgent(rest []string, prefix, contextName string) []string {
	subcommand := rest[0]
	args := rest[1:]
	switch subcommand {
	case "list":
		return completeFlags(args, prefix, map[string][]string{"-o": {"table", "json", "yaml"}, "--output": {"table", "json", "yaml"}})
	case "chat":
		flags := map[string][]string{"--json": {}, "--interactive": {}, "--session": nil}
		if values := completeFlags(args, prefix, flags); values != nil {
			return values
		}
		if len(nonFlagWordsWithValues(args, flags)) == 0 {
			return filterCompletions(liveAgents(contextName), prefix)
		}
	case "create":
		return completeFlags(args, prefix, map[string][]string{
			"--namespace": nil, "--description": nil, "--model": nil, "--instructions": nil,
			"--tools": nil, "--out": nil, "--no-apply": {}, "--dry-run": {},
		})
	}
	return nil
}

func completeFlags(committed []string, prefix string, flags map[string][]string) []string {
	if len(committed) > 0 {
		previous := committed[len(committed)-1]
		if values, exists := flags[previous]; exists {
			if values == nil {
				return []string{}
			}
			return filterCompletions(values, prefix)
		}
	}
	if name, value, inline := strings.Cut(prefix, "="); inline {
		if values, exists := flags[name]; exists && values != nil {
			return prefixCompletions(name+"=", filterCompletions(values, value))
		}
	}
	if strings.HasPrefix(prefix, "-") {
		names := []string{"--context"}
		for name := range flags {
			names = append(names, name)
		}
		return filterCompletions(names, prefix)
	}
	return nil
}

func completionContext(words []string) (string, []string) {
	contextName := ""
	kept := make([]string, 0, len(words))
	for i := 0; i < len(words); i++ {
		name, value, inline := strings.Cut(words[i], "=")
		if name != "--context" && name != "-context" {
			kept = append(kept, words[i])
			continue
		}
		if inline {
			contextName = value
		} else if i+1 < len(words) {
			i++
			contextName = words[i]
		}
	}
	if contextName == "" {
		if cfg, err := config.Load(""); err == nil {
			contextName = cfg.KubeContext
		}
	}
	return contextName, kept
}

func liveAgents(contextName string) []string {
	if contextName == "" {
		return nil
	}
	values := kubectlCompletion("--context", contextName, "-n", "kagent", "get", "agents", "-o", "name")
	for i, value := range values {
		if _, name, ok := strings.Cut(value, "/"); ok {
			values[i] = name
		}
	}
	return values
}

func kubeContexts() []string {
	return kubectlCompletion("config", "get-contexts", "-o", "name")
}

func kubectlCompletion(args ...string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "kubectl", args...).Output()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var values []string
	for _, line := range strings.Split(string(out), "\n") {
		value := strings.TrimSpace(line)
		if value != "" && !seen[value] {
			seen[value] = true
			values = append(values, value)
		}
	}
	sort.Strings(values)
	return values
}

func nonFlagWords(args []string) []string {
	var values []string
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			values = append(values, arg)
		}
	}
	return values
}

func nonFlagWordsWithValues(args []string, flags map[string][]string) []string {
	var values []string
	for i := 0; i < len(args); i++ {
		name, _, inline := strings.Cut(args[i], "=")
		if choices, exists := flags[name]; exists {
			if choices != nil {
				continue
			}
			if !inline && i+1 < len(args) {
				i++
			}
			continue
		}
		if !strings.HasPrefix(args[i], "-") {
			values = append(values, args[i])
		}
	}
	return values
}

func filterCompletions(values []string, prefix string) []string {
	seen := map[string]bool{}
	var filtered []string
	for _, value := range values {
		if strings.HasPrefix(value, prefix) && !seen[value] {
			seen[value] = true
			filtered = append(filtered, value)
		}
	}
	sort.Strings(filtered)
	return filtered
}

func prefixCompletions(prefix string, values []string) []string {
	for i := range values {
		values[i] = prefix + values[i]
	}
	return values
}

func printCandidates(words []string) {
	for _, candidate := range completeWords(words) {
		fmt.Fprintln(os.Stdout, candidate)
	}
}
