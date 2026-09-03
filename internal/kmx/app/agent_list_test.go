package app

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestAgentListRowsAreSortedAndShowWiring(t *testing.T) {
	var agents objectList[agentStatus]
	if err := json.Unmarshal([]byte(`{"items":[
  {"metadata":{"name":"zeta"},"spec":{"declarative":{"modelConfig":"model-z","tools":[]}},"status":{"conditions":[{"Type":"Ready","Status":"False"}]}},
  {"metadata":{"name":"alpha"},"spec":{"declarative":{"modelConfig":"model-a","tools":[{"mcpServer":{"name":"tools"}}]}},"status":{"conditions":[{"Type":"Ready","Status":"True"},{"Type":"Accepted","Status":"True"}]}}
]}`), &agents); err != nil {
		t.Fatal(err)
	}
	rows := agentListRows(agents.Items)
	if rows[0][0] != "alpha" || rows[0][1] != "yes" || rows[0][2] != "yes" || rows[0][3] != "model-a" || rows[0][4] != "tools" {
		t.Fatalf("unexpected first row: %v", rows[0])
	}
	if rows[1][0] != "zeta" || rows[1][4] != "none" {
		t.Fatalf("unexpected second row: %v", rows[1])
	}
}

func TestAgentListOutputValidation(t *testing.T) {
	a := &App{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	if err := a.ListAgents("toml"); err == nil {
		t.Fatal("unsupported agent list output was accepted")
	}
}
