package app

import (
	"context"
	"errors"
	"testing"
)

func TestWorkspaceLiftPresentationConsentCannotBypassGuard(t *testing.T) {
	a, opt, state, _, _, writes := workspaceLiftFixture(t)
	called := false
	opt.Confirm = func(_ context.Context, p WorkspaceLiftPlan) (string, error) {
		called = true
		if p.SuiteDigest == "" || p.Disclosure == "" || p.DestinationServer == "" {
			t.Fatal("UI received incomplete consent disclosure")
		}
		return "different-context", nil
	}
	p, err := PrepareWorkspaceLift(t.Context(), opt)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.LiftWorkspace(t.Context(), p)
	if err == nil || !called || len(state.creates) > 0 || *writes != 0 {
		t.Fatalf("guard bypass/callback missing: %v called=%t", err, called)
	}
}

func TestWorkspaceLiftPresentationConsentCancellation(t *testing.T) {
	a, opt, _, _, _, _ := workspaceLiftFixture(t)
	opt.Confirm = func(context.Context, WorkspaceLiftPlan) (string, error) { return "", context.Canceled }
	p, err := PrepareWorkspaceLift(t.Context(), opt)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.LiftWorkspace(t.Context(), p)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

func TestWorkspaceLiftRetainsIndependentInvocationEvidence(t *testing.T) {
	a, opt, _, _, _, _ := workspaceLiftFixture(t)
	opt.RetainPlacement = true
	opt.Confirm = func(context.Context, WorkspaceLiftPlan) (string, error) { return "kind-test", nil }
	p, err := PrepareWorkspaceLift(t.Context(), opt)
	if err != nil {
		t.Fatal(err)
	}
	r, err := a.LiftWorkspace(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	if r.Deployment == "" {
		t.Fatal("successful opaque placement not retained")
	}
	d, err := ReadSuiteDeployment(r.Deployment)
	if err != nil {
		t.Fatal(err)
	}
	if d.Options.Evidence == "" || d.Options.Reference != opt.Repository+"@"+r.Plan.SuiteDigest {
		t.Fatal("retained input lost exact artifact/evidence")
	}
	if _, _, _, err := a.observeSuiteDeployment(t.Context(), d.ID); err != nil {
		t.Fatal("retained evidence unusable after pipeline closes:", err)
	}
}
