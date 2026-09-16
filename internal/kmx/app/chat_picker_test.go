package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestChatToolPickerSearchToggleAndLockedTools(t *testing.T) {
	m := chatPicker{multiple: true, items: []chatPickerItem{
		{name: "k8s-get-resources"}, {name: "remember", enabled: true, locked: true},
	}}
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'k', Text: "k8s"})
	m = updated.(chatPicker)
	if got := m.matches(); len(got) != 1 || got[0] != 0 {
		t.Fatalf("matches=%v", got)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m = updated.(chatPicker)
	if !m.items[0].enabled {
		t.Fatal("tool was not enabled")
	}
	m.query = "remember"
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	if !updated.(chatPicker).items[1].enabled {
		t.Fatal("automatic memory tool was toggled")
	}
}

func TestChatPickerCancelAndCompletionRestoreReader(t *testing.T) {
	for _, keys := range []string{"\x03", "\x1b", "\r"} {
		m, err := runChatPicker(context.Background(), strings.NewReader(keys), &bytes.Buffer{}, chatPicker{items: []chatPickerItem{{name: "demo"}}})
		if keys == "\x03" {
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Ctrl-C error=%v", err)
			}
		} else if err != nil || m.accepted != (keys == "\r") {
			t.Fatalf("keys=%q accepted=%v err=%v", keys, m.accepted, err)
		}
	}
}

func TestChatToolPatchPreservesReferencesAndVersion(t *testing.T) {
	current := []map[string]any{{"name": "one", "enabled": true}, {"name": "two", "enabled": false}}
	items := []chatPickerItem{{name: "one", enabled: false}, {name: "two", enabled: true}, {name: "new", enabled: true}, {name: "remember", enabled: true, locked: true}}
	body, err := chatToolSelectionPatch("42", current, items)
	if err != nil {
		t.Fatal(err)
	}
	var patch []struct {
		Op, Path string
		Value    json.RawMessage
	}
	if err := json.Unmarshal(body, &patch); err != nil {
		t.Fatal(err)
	}
	if len(patch) != 2 || patch[0].Op != "test" || string(patch[0].Value) != `"42"` {
		t.Fatalf("patch=%s", body)
	}
	var refs []map[string]any
	if err := json.Unmarshal(patch[1].Value, &refs); err != nil {
		t.Fatal(err)
	}
	if len(refs) != 3 || refs[0]["enabled"] != false || refs[1]["enabled"] != true || refs[2]["name"] != "new" || current[0]["enabled"] != true {
		t.Fatalf("refs=%v current=%v", refs, current)
	}
	body, err = chatToolSelectionPatch("42", refs, items)
	if err != nil || body != nil {
		t.Fatalf("unchanged selection generated patch=%s err=%v", body, err)
	}
}
