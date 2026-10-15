package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRetainedPlacementRefusesEvidenceEditsAndPublicFile(t *testing.T) {
	for _, scenario := range []string{"evidence", "permissions", "record"} {
		t.Run(scenario, func(t *testing.T) {
			a, _, opt, _ := releaseApplyFixture(t, "")
			p, err := a.PrepareReleaseLift(t.Context(), opt)
			if err != nil {
				t.Fatal(err)
			}
			r, err := a.ApplyReleaseLift(t.Context(), p, p.Summary().PlanDigest)
			if err != nil {
				t.Fatal(err)
			}
			id, err := a.RetainSuiteDeployment(t.Context(), p, r)
			if err != nil {
				t.Fatal(err)
			}
			dir, _ := deploymentDirectory()
			path := filepath.Join(dir, id+".json")
			switch scenario {
			case "evidence":
				if err = os.WriteFile(opt.Evidence, []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "permissions":
				if err = os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "record":
				d, err := ReadSuiteDeployment(id)
				if err != nil {
					t.Fatal(err)
				}
				d.Placement.Agent.UID = "replacement"
				raw, _ := json.Marshal(d)
				if err = os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = a.SuiteDeploymentStatus(t.Context(), id); err == nil {
				t.Fatal("changed retained inputs accepted")
			}
		})
	}
}

func TestRetainedNativePlacementRechecksInFreshApp(t *testing.T) {
	a, f, opt, _ := releaseApplyFixture(t, "")
	p, err := a.PrepareReleaseLift(t.Context(), opt)
	if err != nil {
		t.Fatal(err)
	}
	r, err := a.ApplyReleaseLift(t.Context(), p, p.Summary().PlanDigest)
	if err != nil {
		t.Fatal(err)
	}
	id, err := a.RetainSuiteDeployment(t.Context(), p, r)
	if err != nil {
		t.Fatal(err)
	}
	other := *a
	s, err := other.SuiteDeploymentStatus(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if s.SuiteDigest != r.Summary.SuiteDigest {
		t.Fatal("identity lost")
	}
	dir, _ := deploymentDirectory()
	info, err := os.Stat(filepath.Join(dir, id+".json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("not private", err)
	}
	nativeMetadata(f.objects["namespace/orka-system"])["uid"] = "replacement"
	if _, err = other.SuiteDeploymentStatus(t.Context(), id); err == nil {
		t.Fatal("changed physical target accepted")
	}
}
func TestRetainRefusesSerializedSummary(t *testing.T) {
	a, _, opt, _ := releaseApplyFixture(t, "")
	p, err := a.PrepareReleaseLift(t.Context(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.RetainSuiteDeployment(t.Context(), p, ReleaseLiftResult{State: "ready", Summary: p.Summary()}); err == nil {
		t.Fatal("public summary authorized retention")
	}
}

func TestIndependentAgentInvocationRefusesSuiteOwnership(t *testing.T) {
	a, _, opt, _ := releaseApplyFixture(t, "")
	p, err := a.PrepareReleaseLift(t.Context(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.refuseIndependentSuiteAgent(t.Context(), p.summary.Target.Namespace, p.summary.Member); err == nil {
		t.Fatal("independent chat accepted suite-owned Agent")
	}
}
