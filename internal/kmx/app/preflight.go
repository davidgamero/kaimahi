package app

import (
	"fmt"
	"os/exec"
	"strings"
)

type dependency struct {
	name, why, install string
	probe              []string
}

var (
	depKubectl = dependency{"kubectl", "to read and write Kubernetes resources", "https://kubernetes.io/docs/tasks/tools/", []string{"version", "--client"}}
	depKind    = dependency{"kind", "to manage the local Kubernetes cluster", "https://kind.sigs.k8s.io/docs/user/quick-start/#installation", []string{"version"}}
	depHelm    = dependency{"helm", "to install kagent", "https://helm.sh/docs/intro/install/", []string{"version"}}
)

func (a *App) engineDependency() dependency {
	install := "https://docs.docker.com/get-docker/"
	if a.Cfg.ContainerEngine == "podman" {
		install = "https://podman.io/docs/installation"
	}
	return dependency{a.Cfg.ContainerEngine, "as the selected kind container engine", install, []string{"info"}}
}

// preflight checks every declared dependency and reports every problem in one
// response. No command should use a dependency before this returns nil.
func (a *App) preflight(dependencies ...dependency) error {
	seen := map[string]bool{}
	var problems []string
	for _, dep := range dependencies {
		if seen[dep.name] {
			continue
		}
		seen[dep.name] = true
		path, err := exec.LookPath(dep.name)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s is not on PATH — kmx needs it %s.\n  install: %s", dep.name, dep.why, dep.install))
			continue
		}
		if len(dep.probe) == 0 {
			continue
		}
		if _, err := a.Run.Capture(path, dep.probe...); err != nil {
			problems = append(problems, fmt.Sprintf("%s was found at %s but is unusable: %v\n  install: %s", dep.name, path, err, dep.install))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	noun := "dependency"
	if len(problems) != 1 {
		noun = "dependencies"
	}
	return fmt.Errorf("preflight: %d missing or unusable %s:\n\n- %s", len(problems), noun, strings.Join(problems, "\n- "))
}
