package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

type configurableChatBackend interface {
	Configure(context.Context, string, *chatRenderer) (bool, error)
}

// Configure runs only between turns, when no task or input reader owns stdin.
func (b *orkaChatBackend) Configure(ctx context.Context, command string, renderer *chatRenderer) (bool, error) {
	if !isInteractiveTerminal(b.app.Stdin) || !isInteractiveTerminal(b.app.Out) {
		return false, fmt.Errorf("%s requires an interactive terminal", command)
	}
	renderer.finish()
	if command == "/agent" {
		return b.pickAgent(ctx)
	}
	return false, b.pickTools(ctx, renderer)
}

func (b *orkaChatBackend) pickAgent(ctx context.Context) (bool, error) {
	raw, err := b.app.orkaCapture(ctx, nil, "-n", b.namespace, "get", "agents.core.orka.ai", "-o", "json")
	if err != nil {
		return false, err
	}
	var list struct {
		Items []struct {
			Metadata struct{ Name, Namespace string }
			Spec     struct{ Runtime json.RawMessage }
		}
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return false, err
	}
	var items []chatPickerItem
	for _, agent := range list.Items {
		// This backend executes AI Tasks, not external CLI-runtime agents.
		if len(agent.Spec.Runtime) > 0 && string(agent.Spec.Runtime) != "null" {
			continue
		}
		detail := agent.Metadata.Namespace
		if agent.Metadata.Name == b.agent {
			detail += " (current)"
		}
		items = append(items, chatPickerItem{name: agent.Metadata.Name, detail: detail})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].name < items[j].name })
	m, err := runChatPicker(ctx, b.app.Stdin, b.app.Out, chatPicker{title: "CONNECT TO AGENT", items: items})
	if err != nil || !m.accepted {
		return false, err
	}
	selected := m.items[m.matches()[m.selection]].name
	raw, err = b.app.orkaCapture(ctx, nil, "-n", b.namespace, "get", "agents.core.orka.ai", selected, "-o", "json")
	if err != nil {
		return false, err
	}
	var object orkaObject
	if err := json.Unmarshal(raw, &object); err != nil || object.Metadata.UID == "" || object.Metadata.Generation < 1 {
		return false, fmt.Errorf("selected Agent returned no valid identity")
	}
	waitCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if err := b.app.waitOrkaReady(waitCtx, b.namespace, orkaIdentity{Kind: "Agent", Name: selected, UID: object.Metadata.UID, Generation: object.Metadata.Generation}); err != nil {
		return false, err
	}
	b.agent = selected
	return true, nil
}

func (b *orkaChatBackend) pickTools(ctx context.Context, renderer *chatRenderer) error {
	raw, err := b.app.orkaCapture(ctx, nil, "-n", b.namespace, "get", "agents.core.orka.ai", b.agent, "-o", "json")
	if err != nil {
		return err
	}
	var agent struct {
		Metadata struct{ ResourceVersion string }
		Spec     struct{ Tools []map[string]any }
	}
	if err := json.Unmarshal(raw, &agent); err != nil {
		return err
	}
	if agent.Metadata.ResourceVersion == "" {
		return fmt.Errorf("Agent has no resourceVersion")
	}
	known := map[string]chatPickerItem{}
	for _, ref := range agent.Spec.Tools {
		name, _ := ref["name"].(string)
		known[name] = chatPickerItem{name: name, enabled: ref["enabled"] != false, detail: "configured"}
	}
	raw, err = b.app.orkaCapture(ctx, nil, "-n", b.namespace, "get", "tools.core.orka.ai", "-o", "json")
	if err != nil {
		return err
	}
	var list struct {
		Items []struct {
			Metadata struct{ Name string }
			Spec     struct{ Description string }
		}
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return err
	}
	for _, tool := range list.Items {
		item := known[tool.Metadata.Name]
		item.name, item.detail = tool.Metadata.Name, tool.Spec.Description
		known[item.name] = item
	}
	for _, name := range []string{"recall_memory", "remember", "propose_memory", "search_transcript"} {
		known[name] = chatPickerItem{name: name, enabled: true, locked: true, detail: "always enabled by Orka v0.1.3"}
	}
	var items []chatPickerItem
	for _, item := range known {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].name < items[j].name })
	m, err := runChatPicker(ctx, b.app.Stdin, b.app.Out, chatPicker{title: "TOOLS · " + b.agent, items: items, multiple: true})
	if err != nil || !m.accepted {
		return err
	}
	patch, err := chatToolSelectionPatch(agent.Metadata.ResourceVersion, agent.Spec.Tools, m.items)
	if err != nil {
		return err
	}
	if patch == nil {
		return nil
	}
	renderer.operation("TOOLS", "", colorBlue, "Saving tool selection; waiting for the Agent configuration to become Ready...")
	waitCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	raw, err = b.app.orkaCapture(waitCtx, nil, "-n", b.namespace, "patch", "agents.core.orka.ai", b.agent, "--type=json", "-p", string(patch), "-o", "json")
	if err != nil {
		return err
	}
	var updated orkaObject
	if err := json.Unmarshal(raw, &updated); err != nil || updated.Metadata.UID == "" {
		return fmt.Errorf("updated Agent returned no identity")
	}
	if err := b.app.waitOrkaReady(waitCtx, b.namespace, orkaIdentity{Kind: "Agent", Name: b.agent, UID: updated.Metadata.UID, Generation: updated.Metadata.Generation}); err != nil {
		return err
	}
	renderer.operation("TOOLS", "", colorGreen, "Tool selection saved for "+b.agent+". The next message uses the updated tools.")
	return nil
}

func chatToolSelectionPatch(version string, current []map[string]any, items []chatPickerItem) ([]byte, error) {
	selected := map[string]bool{}
	for _, item := range items {
		if !item.locked {
			selected[item.name] = item.enabled
		}
	}
	var refs []map[string]any
	changed := false
	for _, ref := range current {
		copyRef := map[string]any{}
		for k, v := range ref {
			copyRef[k] = v
		}
		name, _ := ref["name"].(string)
		if enabled, ok := selected[name]; ok {
			if enabled != (ref["enabled"] != false) {
				changed = true
				copyRef["enabled"] = enabled
			}
			delete(selected, name)
		}
		refs = append(refs, copyRef)
	}
	for _, item := range items {
		if selected[item.name] {
			refs = append(refs, map[string]any{"name": item.name})
			changed = true
		}
	}
	if !changed {
		return nil, nil
	}
	return json.Marshal([]map[string]any{
		{"op": "test", "path": "/metadata/resourceVersion", "value": version},
		{"op": "add", "path": "/spec/tools", "value": refs},
	})
}
