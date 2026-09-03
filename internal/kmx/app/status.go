package app

import "fmt"

// Status prints what `make status` prints.
//
// Unguarded, like the Makefile's target: it reads, and it reads through the
// explicit --context, so it cannot be aimed anywhere the rest of the
// invocation was not already going.
func (a *App) Status() error {
	if err := a.preflight(depKubectl); err != nil {
		return err
	}
	fmt.Fprintf(a.Err, "# context: %s (from %s)\n", a.Cfg.KubeContext, a.Cfg.ContextSource)
	if err := a.kubectlRun("-n", "kagent", "get", "agents,modelconfigs"); err != nil {
		return err
	}
	return a.kubectlRun("-n", "kagent", "get", "pods")
}
