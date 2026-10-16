package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

type suiteFeedback struct {
	title  string
	text   string
	scroll int
}

func consoleAnswerFeedback(answer string, err error) *suiteFeedback {
	if err != nil {
		return &suiteFeedback{title: "RUN STOPPED", text: err.Error()}
	}
	return &suiteFeedback{title: "AGENT ANSWER", text: answer}
}

func consoleLoadingBar(frame, width int) string {
	width = max(4, min(28, width))
	cells := make([]string, width)
	for i := range cells {
		cells[i] = "─"
	}
	for i := 0; i < 4; i++ {
		cells[(frame+i)%width] = "━"
	}
	return "[" + strings.Join(cells, "") + "]"
}

func (m *suiteConsole) operationLabel() string {
	if m.liftGuide != nil {
		return "Lift · " + m.liftGuide.title
	}
	if m.creation != nil {
		return "Create agent · " + m.creation.step().title
	}
	if m.contexts != nil {
		return "Discovering environment access"
	}
	if m.operationAction != "" {
		return "Working · " + strings.ReplaceAll(m.operationAction, "-", " ")
	}
	return "Loading selected operation"
}

func (m *suiteConsole) jobNamed(label string, fn func(context.Context, *App) suiteConsoleResult) tea.Cmd {
	cmd := m.job(fn)
	m.loadingLabel = label
	return cmd
}

// Read-only live inspection is separate from retained-placement authorization.
// Show only public configuration, with UID checks before/after related reads.
func (a *App) inspectConsoleAgent(ctx context.Context, e suiteConsoleEntry) (string, error) {
	if e.Live == nil {
		return "", errors.New("select an observed agent to inspect")
	}
	w := e.Environment.app(a).withRunContext(ctx)
	if e.Environment.Name == "" {
		w = a.releaseLiftWorker(ctx, ReleaseLiftOptions{ToContext: e.Context})
	}
	read := func() ([]byte, error) {
		return w.orkaCapture(ctx, nil, "-n", e.Live.Namespace, "get", consoleAgentKind, e.Live.Name, "-o", "json")
	}
	raw, err := read()
	if err != nil {
		return "", err
	}
	var agent struct {
		Metadata struct {
			Name, Namespace, UID, ResourceVersion string
			Annotations                           map[string]string
			DeletionTimestamp                     *time.Time
		}
		Spec struct {
			ProviderRef  struct{ Name, Namespace string }
			Model        struct{ Name string }
			SystemPrompt agentTUISystemPrompt
			Tools        []struct{ Name string }
		}
		Status struct{ Ready *bool }
	}
	if json.Unmarshal(raw, &agent) != nil || agent.Metadata.UID == "" || agent.Metadata.UID != e.Live.UID || agent.Metadata.Name != e.Live.Name || agent.Metadata.Namespace != e.Live.Namespace {
		return "", errors.New("agent identity changed; refresh the list before inspecting")
	}
	row := *e.Live
	w.agentTUIPrompt(ctx, e.Live.Namespace, agent.Spec.SystemPrompt, &row)
	again, err := read()
	if err != nil {
		return "", err
	}
	var check struct {
		Metadata struct{ UID, ResourceVersion string }
	}
	if json.Unmarshal(again, &check) != nil || check.Metadata.UID != agent.Metadata.UID || check.Metadata.ResourceVersion != agent.Metadata.ResourceVersion {
		return "", errors.New("agent changed during inspection; refresh and inspect again")
	}
	ownership := "Independent agent"
	if hasOrkaSuiteReservation(agent.Metadata.Annotations) {
		ownership = "Suite-owned agent"
	}
	text := fmt.Sprintf("Agent: %s\nEnvironment: %s\nNamespace: %s\nUID: %s\nOwnership: %s\nReady: %s\nInference: %s\nProvider: %s\n", agent.Metadata.Name, environmentLabel(agentTUIEnvironment{Name: e.Context, Local: e.Environment.Local}), agent.Metadata.Namespace, agent.Metadata.UID, ownership, agentTUIReady(agent.Status.Ready), e.inferenceLabel(), agent.Spec.ProviderRef.Name)
	if agent.Metadata.DeletionTimestamp != nil {
		text += "State: deleting\n"
	}
	if e.Deployment != "" {
		text += "Retained placement: " + e.Deployment + " (not authorized by this inspection)\n"
	} else if hasOrkaSuiteReservation(agent.Metadata.Annotations) {
		text += "\nNo retained placement: this KMX state has no matching deployment record tying the package/evidence to owned resource UIDs. The agent exists in Kubernetes. Read-only inspection works; suite chat/run requires its retained placement from the original Lift state.\n"
	}
	for _, key := range []string{"kaimahi.dev/suite", "kaimahi.dev/suite-digest"} {
		if value := agent.Metadata.Annotations[key]; value != "" {
			text += key + ": " + value + "\n"
		}
	}
	text += "\nInstructions (public configuration):\n"
	if row.PromptError != "" {
		text += row.PromptError
	} else {
		text += row.SystemPrompt
	}
	if len(agent.Spec.Tools) > 0 {
		text += "\n\nTools:"
		for _, tool := range agent.Spec.Tools {
			text += "\n  - " + tool.Name
		}
	}
	return safeTerminal(text), nil
}
