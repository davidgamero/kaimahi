package app

import (
	"context"
	"errors"
	"strings"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/cliui"
)

// Host execution is a private workspace preference, not a packaged runtime,
// cluster placement or evaluation receipt. Credentials remain with Copilot.
type suiteCopilotBinding struct {
	Environment agentTUIEnvironment
	Digest      string
}

func (a *App) suiteCopilotModels(ctx context.Context, env agentTUIEnvironment) ([]suiteCreateChoice, error) {
	if env.Name == "" {
		return nil, errors.New("select a local kind context for Copilot host execution")
	}
	if err := env.app(a).requireLocalHostInference(ctx); err != nil {
		return nil, err
	}
	path := detectCopilotCLI()
	if path == "" {
		return nil, errors.New("Copilot CLI is not installed; install it and run copilot login")
	}
	models, err := copilotModels(ctx, path)
	if err != nil {
		return nil, err
	}
	choices := make([]suiteCreateChoice, 0, len(models))
	for _, model := range models {
		choices = append(choices, suiteCreateChoice{label: model.Model, value: model.Model, detail: "Local Copilot CLI · existing host login · tool-free independent turns"})
	}
	return choices, nil
}

func (a *App) checkSuiteCopilot(ctx context.Context, e suiteConsoleEntry) (*suiteWorkspaceReview, string, error) {
	if e.Copilot == nil || e.Root == "" || e.Deployment != "" || e.Image || e.Copilot.Environment.Name == "" {
		return nil, "", errors.New("select a local Copilot workspace, not a cluster placement")
	}
	r, err := a.reviewSuiteWorkspace(ctx, e.Root)
	if err != nil {
		return nil, "", err
	}
	if r.image != nil || r.digest() != e.Copilot.Digest {
		return nil, "", errors.New("Copilot workspace changed; review source and reconnect Copilot with H before sending")
	}
	choices, err := a.suiteCopilotModels(ctx, e.Copilot.Environment)
	if err != nil {
		return nil, "", err
	}
	found := false
	for _, c := range choices {
		if c.value == r.model() {
			found = true
			break
		}
	}
	if !found {
		return nil, "", errors.New("authored model is no longer available from the local Copilot CLI")
	}
	return r, detectCopilotCLI(), nil
}

func (a *App) runSuiteCopilot(ctx context.Context, e suiteConsoleEntry, prompt string) (string, error) {
	if strings.TrimSpace(prompt) == "" || len(prompt) > 1<<20 {
		return "", errors.New("a nonempty bounded prompt is required")
	}
	r, path, err := a.checkSuiteCopilot(ctx, e)
	if err != nil {
		return "", err
	}
	answer, err := copilotPromptModel(ctx, path, r.model(), string(r.instructions())+"\n\nUser message:\n"+prompt)
	if err != nil {
		return "", err
	}
	if err := r.checkCurrent(ctx); err != nil {
		return "", err
	}
	if err := e.Copilot.Environment.app(a).requireLocalHostInference(ctx); err != nil {
		return "", err
	}
	return answer, nil
}

type suiteCopilotChat struct {
	app   *App
	entry suiteConsoleEntry
}

func (b *suiteCopilotChat) Agent() string { return b.entry.Name }
func (b *suiteCopilotChat) Connect(ctx context.Context, r *chatRenderer) ([]cliui.Field, error) {
	review, _, err := b.app.checkSuiteCopilot(ctx, b.entry)
	if err != nil {
		return nil, err
	}
	r.statusStart(review.member(), "local Copilot CLI")
	return []cliui.Field{{Label: "Execution", Value: "Host Copilot CLI; no cluster Task"}, {Label: "Model", Value: review.model()}, {Label: "Evidence", Value: "Development conversation only; not native evaluation or Lift proof"}}, nil
}
func (b *suiteCopilotChat) Send(ctx context.Context, prompt string, r *chatRenderer) error {
	answer, err := b.app.runSuiteCopilot(ctx, b.entry, prompt)
	if err != nil {
		return err
	}
	r.beginAssistant(b.Agent())
	r.assistant(b.Agent(), answer, true)
	return nil
}
