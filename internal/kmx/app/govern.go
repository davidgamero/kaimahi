package app

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/admin"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/config"
)

// GovernOptions are `kmx govern`'s knobs, defaulted to what `make govern`
// uses on kind so the delegating recipe passes nothing surprising.
type GovernOptions struct {
	// Agent is the agent switched onto the governed preset.
	Agent string
	// Preset is the governed ModelConfig to switch it to.
	Preset string
	// Secret is the agent-side Secret the issued token is stored in.
	Secret string
	// SecretNamespace is where that Secret lives.
	SecretNamespace string
}

// Govern issues the governed credential, applies the governed presets, and
// puts the agent behind the plane.
//
// This is `make govern`. What it means, unchanged: the agent is handed a
// Kaimahi-ISSUED opaque token, never an upstream key — the plane stores only
// that token's hash, and the real keys stay with the proxy. From here every
// call the agent makes is authenticated, budget-checked and ledgered.
func (a *App) Govern(credential string, opt GovernOptions) error {
	if err := validCredentialName(credential); err != nil {
		return err
	}
	if opt.Agent == "" || opt.Preset == "" || opt.Secret == "" || opt.SecretNamespace == "" {
		return fmt.Errorf("kmx govern: agent, preset and secret must all be named")
	}
	if err := a.Guard(fmt.Sprintf("govern agent %q through the Kaimahi plane (credential %q)", opt.Agent, credential),
		"kmx govern "+credential); err != nil {
		return err
	}

	client, err := admin.Open(a, a.Cfg.AdminPort, a.Err)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := a.issueCredential(client, credential, opt); err != nil {
		return err
	}

	// Both governed presets are applied on every target — which one the
	// agent is switched to depends on the environment. On kind that is
	// governed-ollama, the keyless one, which is why milestone 2 needs no
	// captured secret anywhere (D28(4)). They are applied by the switch
	// below, which has to watch the preset's generation across the apply.
	presets := []string{"models/governed-ollama.yaml", "models/governed-copilot.yaml"}

	// Only a genuine NotFound may skip the switch. Collapsing every failure
	// into "absent" would print the reassuring NOTE, exit 0, and leave the
	// agent on an UNGOVERNED preset — spending outside the plane. An
	// unreachable API server, an expired credential, an RBAC denial and a
	// wrong context all look exactly like that if you do not look.
	_, err = a.kubectlCapture("-n", config_kagentNamespace, "get", "agent", opt.Agent, "-o", "name")
	switch {
	case err == nil:
		return a.UsePreset(opt.Agent, opt.Preset, presets)
	case isNotFound(err):
		for _, name := range presets {
			if err := a.apply(name); err != nil {
				return err
			}
		}
		// Say what actually happens, not what would be reassuring. `kmx up`
		// creates hello-world on the KEYLESS preset (k8s/hello-world.yaml
		// pins it), so an agent created after this runs UNGOVERNED until
		// govern is run again. The Makefile's managed branch says the same
		// thing for the same reason.
		a.notef("NOTE: agent %s does not exist yet, so nothing was switched. %s is on the cluster,\n"+
			"  but an agent created later starts on the keyless preset %s — re-run\n"+
			"  `kmx govern %s` once %s exists, or it will spend outside the plane.",
			opt.Agent, opt.Preset, config.KeylessModelConfig, credential, opt.Agent)
		return nil
	default:
		return fmt.Errorf("cannot tell whether agent %s exists (refusing to leave it ungoverned): %w", opt.Agent, err)
	}
}

// issueCredential mints the credential and stores its token as the
// agent-side Secret, reconciling the already-issued case as
// scripts/plane-admin.sh does (minus its `GOVERNED_SECRET=-` form, which
// discards the token for P7b's signed hooks — an inbound feature kmx does
// not have).
//
// The token is shown EXACTLY ONCE, at issue time, and cannot be recovered.
// That is what makes both the check before the POST and the 409 branch below
// more than politeness.
func (a *App) issueCredential(client *admin.Client, credential string, opt GovernOptions) error {
	// Whose token is in that Secret? Asked BEFORE issuing, because the
	// answer can forbid the whole operation: `kmx govern demo` while the
	// Secret holds hello-world's token would otherwise mint demo's
	// credential, overwrite the Secret, and destroy the only copy of
	// hello-world's token — leaving a live credential nothing can use. The
	// 409 branch refuses exactly this once the credential already exists;
	// the first issue of a SECOND name has to refuse it too, and refusing
	// before the POST also avoids leaving an orphan credential row behind.
	bound, err := a.boundCredential(opt)
	if err != nil {
		return err
	}
	if bound != "" && bound != credential {
		return a.wrongCredentialError(bound, credential, opt)
	}

	status, body, err := client.Do(http.MethodPost, "/admin/credentials", map[string]string{"name": credential})
	if err != nil {
		return err
	}

	if status == http.StatusConflict {
		return a.reconcileExistingCredential(credential, opt)
	}
	if status != http.StatusCreated {
		return fmt.Errorf("issuing credential %q failed (HTTP %d): %s",
			credential, status, strings.TrimSpace(string(body)))
	}

	token, err := admin.TokenFrom(body)
	if err != nil {
		return err
	}
	// Straight from the reply into the manifest into kubectl's stdin. The
	// token is in this process's memory and in the cluster, and nowhere
	// else: not argv, not the environment, not a file, not a log.
	manifest := secretManifest(opt.Secret, opt.SecretNamespace,
		map[string]string{"api-key": token},
		// Bind the Secret to its credential, so a later issue of a
		// DIFFERENT name detects the mismatch instead of silently reusing
		// this token.
		map[string]string{"kaimahi.dev/credential": credential})
	quiet := *a.Run
	quiet.Echo = false
	fmt.Fprintf(a.Err, "kubectl --context %s -n %s apply -f - # (Secret %s, from the pipe)\n",
		a.Cfg.KubeContext, opt.SecretNamespace, opt.Secret)
	if err := quiet.RunStdin(manifest, "kubectl",
		a.kubectl("-n", opt.SecretNamespace, "apply", "-f", "-")...); err != nil {
		return err
	}
	a.notef("Governed credential %q issued; Secret %s/%s created.", credential, opt.SecretNamespace, opt.Secret)
	a.notef("The plane stores only its hash — the real upstream keys stay with the proxy.")
	return nil
}

// boundCredential returns the credential the agent-side Secret holds the
// token for, or "" when there is no such Secret.
//
// Only a genuine NotFound is "no Secret". Any other read failure aborts: an
// unreadable Secret answered as absent is how the overwrite this check
// exists to prevent would happen anyway.
func (a *App) boundCredential(opt GovernOptions) (string, error) {
	bound, err := a.kubectlCapture("-n", opt.SecretNamespace, "get", "secret", opt.Secret,
		"-o", `jsonpath={.metadata.annotations.kaimahi\.dev/credential}`)
	if err != nil {
		if isNotFound(err) {
			return "", nil
		}
		return "", fmt.Errorf("cannot read Secret %s to tell whose token it holds (refusing to overwrite it blind): %w",
			opt.Secret, err)
	}
	return strings.TrimSpace(bound), nil
}

func (a *App) wrongCredentialError(bound, credential string, opt GovernOptions) error {
	return fmt.Errorf("Secret %s holds the token for credential %q, not %q — refusing.\n"+
		"  That token is the only copy; overwriting it would leave %q live in the plane and unusable.\n"+
		"  Name a different Secret: kmx govern %s --secret <name>",
		opt.Secret, bound, credential, bound, credential)
}

// reconcileExistingCredential decides what an HTTP 409 means, given what the
// agent-side Secret is bound to.
func (a *App) reconcileExistingCredential(credential string, opt GovernOptions) error {
	bound, err := a.boundCredential(opt)
	if err != nil {
		return err
	}
	switch bound {
	case credential:
		a.notef("Credential %q already issued and %s is bound to it; keeping both.", credential, opt.Secret)
		return nil
	case "":
		return fmt.Errorf("credential %q exists in the plane but Secret %s is missing (or unlabeled).\n"+
			"  The token is shown exactly once at issue time and cannot be recovered;\n"+
			"  delete the row and re-run:\n"+
			"    kubectl --context %s -n %s exec deploy/kaimahi-postgres -- \\\n"+
			"      psql -U kaimahi -c \"DELETE FROM credential WHERE name='%s'\"",
			credential, opt.Secret, a.Cfg.KubeContext, admin.Namespace, credential)
	default:
		return a.wrongCredentialError(bound, credential, opt)
	}
}

// validCredentialName is the script's check_name, kept because these names
// are interpolated into JSON and query strings. The plane validates again.
func validCredentialName(name string) error {
	if name == "" {
		return fmt.Errorf("usage: kmx govern <credential>")
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return fmt.Errorf("invalid credential name %q (want [a-z0-9-]+)", name)
		}
	}
	return nil
}
