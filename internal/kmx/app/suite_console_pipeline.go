package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type suitePipelineConsent struct {
	plan  WorkspaceLiftPlan
	reply chan string
}

func (m *suiteConsole) startNativePipeline() tea.Cmd {
	g := *m.liftGuide
	if g.entry.Review == nil || g.entry.Review.suite == nil {
		m.status = "Open native source for workspace Lift"
		return nil
	}
	if !g.entry.Reviewed {
		g.stage = "review-pipeline"
		m.liftGuide = &g
		m.showReview(g.entry.Review)
		return nil
	}
	if m.pipelineRequests == nil {
		m.pipelineRequests = make(chan suitePipelineConsent, 1)
	}
	requests := m.pipelineRequests
	return m.jobNamed("Preparing shared workspace Lift", func(ctx context.Context, a *App) suiteConsoleResult {
		if err := g.entry.Review.checkCurrent(ctx); err != nil {
			return suiteConsoleResult{err: err}
		}
		w := g.env.app(a).withRunContext(ctx)
		opt := WorkspaceLiftOptions{Workspace: g.entry.Root, Repository: g.registry + "/" + g.repository, EvalName: g.env.Name, DestinationName: g.env.Name, RetainPlacement: true,
			Evaluation:  SuiteWorkspaceEvaluationOptions{Workspace: g.entry.Root, ToContext: g.env.Name, ToNamespace: g.namespace, Inference: g.provider, JudgeBaseURL: g.judgeURL, JudgeKeyEnv: g.judgeKeyEnv},
			Destination: ReleaseLiftOptions{ToContext: g.env.Name, ToNamespace: g.namespace, Inference: g.provider},
			Confirm: func(ctx context.Context, plan WorkspaceLiftPlan) (string, error) {
				request := suitePipelineConsent{plan: plan, reply: make(chan string, 1)}
				select {
				case requests <- request:
				case <-ctx.Done():
					return "", ctx.Err()
				}
				select {
				case answer := <-request.reply:
					return answer, nil
				case <-ctx.Done():
					return "", ctx.Err()
				}
			},
		}
		prepared, err := PrepareWorkspaceLift(ctx, opt)
		if err != nil {
			return suiteConsoleResult{err: err}
		}
		defer prepared.Close()
		result, err := w.LiftWorkspace(ctx, prepared)
		if err != nil {
			return suiteConsoleResult{err: err, text: fmt.Sprintf("Eval: %s\nPush: %s\nLift: %s", result.Eval.Detail, result.Push.Detail, result.Lift.Detail)}
		}
		e := g.entry
		e.Reference = opt.Repository + "@" + result.Plan.SuiteDigest
		e.Published = publicationHint(e, false)
		if result.Deployment != "" {
			d, readErr := ReadSuiteDeployment(result.Deployment)
			if readErr != nil {
				return suiteConsoleResult{err: readErr}
			}
			e.Deployment = d.ID
			e.Evidence = d.Options.Evidence
			e.Context = g.env.Name
			e.Environment = g.env
			e.ObservedReady = true
			e.Origin = "retained placement"
			if !g.env.Local {
				e.Side = 1
			}
		}
		return suiteConsoleResult{entry: &e, pipelineDone: true, text: fmt.Sprintf("Eval: %s\nPush: %s\nLift: %s\n%s", result.Eval.Detail, result.Push.Detail, result.Lift.Detail, map[bool]string{true: "Chat/run now available", false: "Completion verified read-only; use an existing retained placement for chat/run"}[e.Deployment != ""])}
	})
}

func (m *suiteConsole) updatePipelineConsent(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc", "ctrl+c":
		m.cancel()
		m.cancelling = true
		m.pipelineConsent = nil
	case "enter":
		answer := strings.TrimSpace(m.input.Value())
		if answer == "" {
			m.status = "Type the context(s) shown in the plan to authorize the shared pipeline"
			return m, nil
		}
		select {
		case m.pipelineConsent.reply <- answer:
		default:
			m.status = errors.New("confirmation already submitted").Error()
		}
		m.pipelineConsent = nil
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(key)
		return m, cmd
	}
	return m, nil
}
