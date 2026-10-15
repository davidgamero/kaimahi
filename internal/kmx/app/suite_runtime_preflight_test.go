package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"
)

func TestSuiteRuntimeCheckUsesArtifactAndOnlyReadsTarget(t *testing.T) {
	for _, mode := range []string{"ready", "absent", "legacy", "unreadable", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			a, f, opt, _ := releaseLiftFixture(t)
			opt.Evidence = ""
			opt.Inference = ""
			capture := a.captureCommand
			a.captureCommand = func(ctx context.Context, cmd *exec.Cmd) error {
				args := strings.Join(cmd.Args, " ")
				if !strings.Contains(args, " get ") {
					t.Fatalf("check attempted non-GET: %s", args)
				}
				if strings.Contains(args, " get deploy ") {
					switch mode {
					case "absent":
						_, err := io.WriteString(cmd.Stdout, `{"items":[]}`)
						return err
					case "malformed":
						_, err := io.WriteString(cmd.Stdout, `{}`)
						return err
					case "unreadable":
						return errors.New("private server diagnostic")
					case "legacy":
						doc := f.objects["deploy/-o"]
						body, _ := json.Marshal(doc)
						var cp map[string]any
						json.Unmarshal(body, &cp)
						cp["items"].([]any)[0].(map[string]any)["metadata"].(map[string]any)["labels"] = map[string]any{"app.kubernetes.io/name": "orka", "control-plane": "controller-manager"}
						return json.NewEncoder(cmd.Stdout).Encode(cp)
					}
				}
				return capture(ctx, cmd)
			}
			r, err := a.CheckSuiteRuntime(t.Context(), opt)
			if err != nil {
				t.Fatal(err)
			}
			want := mode
			if mode == "legacy" {
				want = "incompatible"
			}
			if mode == "unreadable" || mode == "malformed" {
				want = "unknown"
			}
			if r.State != want {
				t.Fatalf("state=%s want=%s", r.State, want)
			}
			if strings.Contains(r.Detail, "private server") {
				t.Fatal("raw diagnostic leaked")
			}
			if mode == "absent" && !strings.Contains(r.Next, "orka install --provider -") {
				t.Fatal("missing separate setup command")
			}
		})
	}
}
