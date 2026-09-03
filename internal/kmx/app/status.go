package app

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// StatusOptions controls human or kubectl-native status output.
type StatusOptions struct {
	Output string
}

type objectList[T any] struct {
	Items []T `json:"items"`
}

type statusCondition struct {
	Type, Status string
}

type agentStatus struct {
	Metadata struct{ Name string } `json:"metadata"`
	Spec     struct {
		Declarative struct {
			ModelConfig string `json:"modelConfig"`
			Tools       []struct {
				MCPServer struct{ Name string } `json:"mcpServer"`
			} `json:"tools"`
		} `json:"declarative"`
	} `json:"spec"`
	Status struct{ Conditions []statusCondition } `json:"status"`
}

type modelStatus struct {
	Metadata struct{ Name string }                  `json:"metadata"`
	Spec     struct{ Provider, Model string }       `json:"spec"`
	Status   struct{ Conditions []statusCondition } `json:"status"`
}

type podStatus struct {
	Metadata struct{ Name string } `json:"metadata"`
	Status   struct {
		Phase             string
		Conditions        []statusCondition
		ContainerStatuses []struct {
			RestartCount int `json:"restartCount"`
		} `json:"containerStatuses"`
	} `json:"status"`
}

func condition(conditions []statusCondition, name string) string {
	for _, value := range conditions {
		if value.Type == name {
			if value.Status == "True" {
				return "yes"
			}
			return "no"
		}
	}
	return "-"
}

func table(out io.Writer, headers []string, rows [][]string) {
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = len(header)
	}
	for _, row := range rows {
		for i, value := range row {
			if len(value) > widths[i] {
				widths[i] = len(value)
			}
		}
	}
	for rowIndex, row := range append([][]string{headers}, rows...) {
		fmt.Fprint(out, "  ")
		for i, value := range row {
			if i > 0 {
				fmt.Fprint(out, "  ")
			}
			fmt.Fprintf(out, "%-*s", widths[i], value)
		}
		if rowIndex < len(rows) {
			fmt.Fprintln(out)
		}
	}
	fmt.Fprintln(out)
}

func (a *App) statusJSON(namespace, resource string, optional bool, target any) error {
	raw, err := a.kubectlCapture("-n", namespace, "get", resource, "-o", "json")
	if err != nil {
		if optional && isNotFound(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal([]byte(raw), target)
}

func podSummary(pods []podStatus) (ready, restarts int, rows [][]string) {
	sort.Slice(pods, func(i, j int) bool { return pods[i].Metadata.Name < pods[j].Metadata.Name })
	for _, pod := range pods {
		isReady := condition(pod.Status.Conditions, "Ready") == "yes"
		if isReady {
			ready++
		}
		podRestarts := 0
		for _, container := range pod.Status.ContainerStatuses {
			podRestarts += container.RestartCount
		}
		restarts += podRestarts
		rows = append(rows, []string{pod.Metadata.Name, map[bool]string{true: "yes", false: "no"}[isReady], pod.Status.Phase, fmt.Sprint(podRestarts)})
	}
	return
}

func statusReady(allAgents, allModels bool, kReady, kTotal, oReady, oTotal, pReady, pTotal int) bool {
	ready := allAgents && allModels && kTotal > 0 && kReady == kTotal
	if oTotal > 0 {
		ready = ready && oReady == oTotal
	}
	if pTotal > 0 {
		ready = ready && pReady == pTotal
	}
	return ready
}

// Status prints a grouped human view or kubectl-native JSON/YAML.
func (a *App) StatusWithOptions(opt StatusOptions) error {
	format := strings.ToLower(strings.TrimSpace(opt.Output))
	if format == "" || format == "table" {
		return a.statusTable()
	}
	if format != "json" && format != "yaml" {
		return fmt.Errorf("status output %q is not supported — use table, json, or yaml", opt.Output)
	}
	fmt.Fprintf(a.Err, "# context: %s (from %s)\n", a.Cfg.KubeContext, a.Cfg.ContextSource)
	return a.kubectlRun("-n", "kagent", "get", "agents,modelconfigs,pods", "-o", format)
}

func (a *App) Status() error { return a.StatusWithOptions(StatusOptions{}) }

func (a *App) statusTable() error {
	var agents objectList[agentStatus]
	var models objectList[modelStatus]
	var kagentPods, ollamaPods, planePods objectList[podStatus]
	if err := a.statusJSON("kagent", "agents", false, &agents); err != nil {
		return err
	}
	if err := a.statusJSON("kagent", "modelconfigs", false, &models); err != nil {
		return err
	}
	if err := a.statusJSON("kagent", "pods", false, &kagentPods); err != nil {
		return err
	}
	if err := a.statusJSON("ollama", "pods", true, &ollamaPods); err != nil {
		return err
	}
	if err := a.statusJSON("kaimahi", "pods", true, &planePods); err != nil {
		return err
	}

	agentRows := make([][]string, 0, len(agents.Items))
	allAgents := len(agents.Items) > 0
	for _, agent := range agents.Items {
		servers := make([]string, 0, len(agent.Spec.Declarative.Tools))
		for _, tool := range agent.Spec.Declarative.Tools {
			if tool.MCPServer.Name != "" {
				servers = append(servers, tool.MCPServer.Name)
			}
		}
		ready, accepted := condition(agent.Status.Conditions, "Ready"), condition(agent.Status.Conditions, "Accepted")
		allAgents = allAgents && ready == "yes" && accepted == "yes"
		agentRows = append(agentRows, []string{agent.Metadata.Name, ready, accepted, agent.Spec.Declarative.ModelConfig, valueOr(strings.Join(servers, ","), "none")})
	}
	sort.Slice(agentRows, func(i, j int) bool { return agentRows[i][0] < agentRows[j][0] })

	modelRows := make([][]string, 0, len(models.Items))
	allModels := len(models.Items) > 0
	for _, model := range models.Items {
		accepted := condition(model.Status.Conditions, "Accepted")
		allModels = allModels && accepted == "yes"
		modelRows = append(modelRows, []string{model.Metadata.Name, model.Spec.Provider, model.Spec.Model, accepted})
	}
	sort.Slice(modelRows, func(i, j int) bool { return modelRows[i][0] < modelRows[j][0] })

	kReady, kRestarts, podRows := podSummary(kagentPods.Items)
	oReady, oRestarts, _ := podSummary(ollamaPods.Items)
	pReady, pRestarts, _ := podSummary(planePods.Items)
	overall := statusReady(allAgents, allModels,
		kReady, len(kagentPods.Items), oReady, len(ollamaPods.Items), pReady, len(planePods.Items))

	fmt.Fprintln(a.Out, "Kaimahi status")
	fmt.Fprintf(a.Out, "  context: %s (from %s)\n", a.Cfg.KubeContext, a.Cfg.ContextSource)
	if overall {
		fmt.Fprintf(a.Out, "  result:  ready (%d agents available)\n", len(agents.Items))
	} else {
		fmt.Fprintln(a.Out, "  result:  attention required")
	}
	fmt.Fprintln(a.Out, "\nAgents")
	table(a.Out, []string{"NAME", "READY", "ACCEPTED", "MODEL CONFIG", "TOOL SERVER"}, agentRows)
	fmt.Fprintln(a.Out, "  Ready = can serve requests; Accepted = kagent accepted the configuration.")
	fmt.Fprintln(a.Out, "\nModels")
	table(a.Out, []string{"CONFIG", "PROVIDER", "MODEL", "ACCEPTED"}, modelRows)
	fmt.Fprintln(a.Out, "\nRuntime")
	fmt.Fprintf(a.Out, "  kagent:     %d/%d pods ready, %d restarts\n", kReady, len(kagentPods.Items), kRestarts)
	if len(ollamaPods.Items) > 0 {
		fmt.Fprintf(a.Out, "  ollama:     %d/%d pods ready, %d restarts\n", oReady, len(ollamaPods.Items), oRestarts)
	} else {
		fmt.Fprintln(a.Out, "  ollama:     not installed")
	}
	if len(planePods.Items) > 0 {
		fmt.Fprintf(a.Out, "  governance: %d/%d pods ready, %d restarts\n", pReady, len(planePods.Items), pRestarts)
	} else {
		fmt.Fprintln(a.Out, "  governance: not installed (run `kmx plane` for budgets and audit)")
	}
	fmt.Fprintln(a.Out, "\nRuntime pods")
	table(a.Out, []string{"NAME", "READY", "PHASE", "RESTARTS"}, podRows)
	fmt.Fprintln(a.Out, "\nNext")
	if overall {
		fmt.Fprintln(a.Out, "  kmx agent chat hello-world")
	} else {
		fmt.Fprintf(a.Out, "  kubectl --context %s -n kagent get agents,pods\n", a.Cfg.KubeContext)
	}
	return nil
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
