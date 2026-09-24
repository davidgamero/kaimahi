package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

type consoleModelInput struct {
	input    textinput.Model
	accepted bool
}

func (m consoleModelInput) Init() tea.Cmd { return m.input.Focus() }
func (m consoleModelInput) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "esc", "ctrl+c":
			return m, tea.Quit
		case "enter":
			m.accepted = true
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}
func (m consoleModelInput) View() tea.View {
	return tea.NewView("Model override (empty uses Provider default)\n\n" + m.input.View() + "\n\n<enter> continue · <esc> cancel")
}

func (a *App) consoleEditInference(agent agentTUIAgent) error {
	ctx := a.operationContext()
	kind, configs := "agents.core.orka.ai", "providers.core.orka.ai"
	if agent.Runtime == "kagent" {
		kind, configs = "agents.kagent.dev", "modelconfigs.kagent.dev"
	} else if agent.Runtime != "orka" {
		return fmt.Errorf("unsupported inference runtime")
	}
	raw, err := a.orkaCapture(ctx, nil, "-n", agent.Namespace, "get", kind, agent.Name, "-o", "json")
	if err != nil {
		return err
	}
	var object struct {
		Metadata struct{ ResourceVersion string }
		Spec     struct {
			Model       map[string]any
			Declarative json.RawMessage
		}
	}
	if err = json.Unmarshal(raw, &object); err != nil {
		return err
	}
	if object.Metadata.ResourceVersion == "" {
		return fmt.Errorf("Agent has no resourceVersion")
	}
	raw, err = a.orkaCapture(ctx, nil, "-n", agent.Namespace, "get", configs, "-o", "json")
	if err != nil {
		return err
	}
	var list objectList[struct {
		Metadata struct{ Name string }
		Spec     struct{ Type, Provider, DefaultModel, Model string }
	}]
	if err = json.Unmarshal(raw, &list); err != nil {
		return err
	}
	if len(list.Items) == 0 {
		return fmt.Errorf("no %s available in %s", configs, agent.Namespace)
	}
	var items []chatPickerItem
	for _, c := range list.Items {
		items = append(items, chatPickerItem{name: c.Metadata.Name, detail: valueOr(c.Spec.Type, c.Spec.Provider) + " · " + valueOr(c.Spec.DefaultModel, c.Spec.Model)})
	}
	r := newChatRenderer(a.Out)
	r.enterFullScreen()
	defer r.leaveFullScreen()
	picked, err := runChatPicker(ctx, a.Stdin, a.Out, chatPicker{title: "INFERENCE · " + agent.Name + " · " + a.Cfg.KubeContext, items: items, vim: true, searchEnabled: true, action: "select"})
	if err != nil || !picked.accepted {
		return err
	}
	choice := items[picked.matches()[picked.selection]].name
	model := ""
	if agent.Runtime == "orka" {
		input := textinput.New()
		input.CharLimit = 256
		input.SetWidth(60)
		current, _ := object.Spec.Model["name"].(string)
		input.SetValue(current)
		input.Focus()
		result, err := tea.NewProgram(consoleModelInput{input: input}, tea.WithInput(a.Stdin), tea.WithOutput(a.Out), tea.WithContext(ctx)).Run()
		if err != nil {
			return err
		}
		completed := result.(consoleModelInput)
		if !completed.accepted {
			return nil
		}
		model = strings.TrimSpace(completed.input.Value())
		if err := refuseWizardCredentials(model); err != nil {
			return err
		}
	}
	patch, err := consoleInferencePatch(agent.Runtime, object.Metadata.ResourceVersion, choice, agent.Namespace, model, object.Spec.Model)
	if err != nil {
		return err
	}
	review, err := runChatPicker(ctx, a.Stdin, a.Out, chatPicker{title: fmt.Sprintf("INFERENCE · %s/%s\nContext: %s\nConfiguration: %s\nModel: %s", agent.Namespace, agent.Name, a.Cfg.KubeContext, choice, valueOr(model, "configuration default")), items: []chatPickerItem{{name: "Cancel"}, {name: "Save inference"}}, vim: true, action: "select"})
	if err != nil || !review.accepted || review.selection == 0 {
		return err
	}
	b := &orkaChatBackend{app: a, agent: agent.Name, namespace: agent.Namespace}
	return b.runLiftDeployment(ctx, a, "Save inference", []string{"Save agent configuration"}, func(worker *App) error {
		raw, err := worker.orkaCapture(worker.operationContext(), nil, "-n", agent.Namespace, "patch", kind, agent.Name, "--type=json", "-p", string(patch), "-o", "json")
		if err != nil {
			return err
		}
		if agent.Runtime == "orka" {
			var updated orkaObject
			if err = json.Unmarshal(raw, &updated); err != nil {
				return err
			}
			if updated.Metadata.UID == "" {
				return fmt.Errorf("updated Agent returned no identity")
			}
			waitCtx, cancel := context.WithTimeout(worker.operationContext(), time.Minute)
			defer cancel()
			return worker.waitOrkaReady(waitCtx, agent.Namespace, orkaIdentity{Kind: "Agent", Name: agent.Name, UID: updated.Metadata.UID, Generation: updated.Metadata.Generation})
		}
		return nil
	})
}

func consoleInferencePatch(runtime, version, configuration, namespace, model string, currentModel map[string]any) ([]byte, error) {
	if version == "" || configuration == "" {
		return nil, fmt.Errorf("inference update requires an Agent version and configuration")
	}
	patch := []map[string]any{{"op": "test", "path": "/metadata/resourceVersion", "value": version}}
	switch runtime {
	case "orka":
		updated := map[string]any{}
		for k, v := range currentModel {
			updated[k] = v
		}
		if model != "" {
			updated["name"] = model
		} else {
			delete(updated, "name")
		}
		patch = append(patch, map[string]any{"op": "add", "path": "/spec/providerRef", "value": map[string]string{"name": configuration, "namespace": namespace}}, map[string]any{"op": "add", "path": "/spec/model", "value": updated})
	case "kagent":
		patch = append(patch, map[string]any{"op": "add", "path": "/spec/declarative/modelConfig", "value": configuration})
	default:
		return nil, fmt.Errorf("unsupported inference runtime %q", runtime)
	}
	return json.Marshal(patch)
}
