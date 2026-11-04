package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentfile"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/runtime"
)

type agentFileTestsMetadata struct {
	State        string `json:"state"`
	Revision     string `json:"revision,omitempty"`
	SelectionKey string `json:"selectionKey,omitempty"`
	Explicit     bool   `json:"explicit,omitempty"`
}

type agentFileValidation struct {
	Valid            bool                   `json:"valid"`
	Name             string                 `json:"name"`
	Title            string                 `json:"title"`
	Model            string                 `json:"model,omitempty"`
	AgentRevision    string                 `json:"agentRevision"`
	SnapshotRevision string                 `json:"snapshotRevision"`
	Tests            agentFileTestsMetadata `json:"tests"`
	Cases            int                    `json:"cases"`
	Tools            int                    `json:"tools"`
}

type agentFileInspectionCase struct {
	ID         string                        `json:"id"`
	Input      string                        `json:"input"`
	Mocks      *agentfile.Mocks              `json:"mocks,omitempty"`
	Assertions []runtime.EvaluationAssertion `json:"assertions"`
}

type agentFileInspection struct {
	agentFileValidation
	Purpose      string                    `json:"purpose,omitempty"`
	Instructions string                    `json:"instructions"`
	ToolNames    []string                  `json:"toolNames"`
	Examples     []agentFileInspectionCase `json:"examples"`
}

func newAgentFileValidateCommand(state *commandState) *cobra.Command {
	return newAgentFileMetadataCommand(state, false)
}

func newAgentFileInspectCommand(state *commandState) *cobra.Command {
	return newAgentFileMetadataCommand(state, true)
}

func newAgentFileMetadataCommand(state *commandState, content bool) *cobra.Command {
	name, summary := "validate", "Validate an agent and selected tests offline"
	if content {
		name, summary = "inspect", "Print an agent and selected tests offline"
	}
	var tests, output string
	cmd := &cobra.Command{
		Use:   name + " FILE",
		Short: summary,
		Long:  summary + ". Uses the same default or explicit test-file selection as file Run/Test. No configuration, state, provider, model, command, or network access.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if state.contextFlag != "" || cmd.Flags().Changed("container-engine") {
				return fmt.Errorf("%s is offline; omit cluster selectors", name)
			}
			if output != "text" && output != "json" {
				return fmt.Errorf("%s output must be text or json", name)
			}
			if cmd.Flags().Changed("tests") && strings.TrimSpace(tests) == "" {
				return fmt.Errorf("%s requires nonempty --tests", name)
			}
			pair, err := agentfile.LoadPair(args[0], tests)
			if err != nil {
				return err
			}
			if content {
				value := inspectAgentFilePair(pair)
				if output == "json" {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(value)
				}
				return writeAgentFileInspection(cmd, value)
			}
			value := validateAgentFilePair(pair)
			if output == "json" {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(value)
			}
			return writeAgentFileValidation(cmd, value)
		},
	}
	cmd.Flags().StringVar(&tests, "tests", "", "Tests file override (FILE)")
	cmd.Flags().StringVarP(&output, "output", "o", "text", name+" output: text or json")
	return cmd
}

func validateAgentFilePair(pair *agentfile.Pair) agentFileValidation {
	tests := agentFileTestsMetadata{State: pair.Tests.State, Revision: pair.Tests.Revision, Explicit: pair.Tests.Selection.Explicit}
	if pair.Tests.Selection.Path != "" {
		tests.SelectionKey = pair.Tests.Selection.Key
	}
	examples := pair.Agent.Examples
	if pair.Tests.Suite != nil {
		examples = pair.Tests.Suite.Examples
	}
	return agentFileValidation{Valid: true, Name: pair.Agent.Identity.ID, Title: pair.Agent.Title, Model: pair.Agent.Model,
		AgentRevision: pair.Agent.Revision, SnapshotRevision: pair.SnapshotRevision, Tests: tests, Cases: len(examples), Tools: len(pair.Agent.Tools)}
}

func inspectAgentFilePair(pair *agentfile.Pair) agentFileInspection {
	value := agentFileInspection{agentFileValidation: validateAgentFilePair(pair), Purpose: pair.Agent.Identity.Purpose,
		Instructions: pair.Agent.Instructions, ToolNames: append([]string(nil), pair.Agent.Tools...)}
	examples := pair.Agent.Examples
	if pair.Tests.Suite != nil {
		examples = pair.Tests.Suite.Examples
	}
	value.Examples = make([]agentFileInspectionCase, 0, len(examples))
	for _, example := range examples {
		value.Examples = append(value.Examples, agentFileInspectionCase{ID: example.ID, Input: example.Input, Mocks: example.Mocks, Assertions: example.Assertions})
	}
	return value
}

func writeAgentFileValidation(cmd *cobra.Command, value agentFileValidation) error {
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "Valid · %s · %d tests\nAgent: %s\nSnapshot: %s\nTests: %s", value.Name, value.Cases, value.AgentRevision, value.SnapshotRevision, value.Tests.State)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout())
	return err
}

func writeAgentFileInspection(cmd *cobra.Command, value agentFileInspection) error {
	if err := writeAgentFileValidation(cmd, value.agentFileValidation); err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(out, "\nInstructions:\n%s", value.Instructions); err != nil {
		return err
	}
	if !strings.HasSuffix(value.Instructions, "\n") {
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
	}
	for _, example := range value.Examples {
		raw, err := json.MarshalIndent(example, "", "  ")
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "\nTest %s\n%s\n", example.ID, raw); err != nil {
			return err
		}
	}
	return nil
}
