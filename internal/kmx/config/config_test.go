package config

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// kmx and the Makefile are ONE implementation of the journey, which only
// stays true if they agree about the cluster name, the pinned kagent
// version, the model and the chat port. Those values live in two files, so
// read the Makefile and refuse to let them drift.
func TestDefaultsMatchTheMakefile(t *testing.T) {
	makefile, err := os.ReadFile(filepath.Join("..", "..", "..", "Makefile"))
	if err != nil {
		t.Fatalf("cannot read the Makefile: %v", err)
	}
	for _, tc := range []struct{ variable, want string }{
		{"KIND_CLUSTER", DefaultKindCluster},
		{"KAGENT_VERSION", DefaultKagentVersion},
		{"MODEL", DefaultModel},
		{"CHAT_PORT", DefaultChatPort},
		{"AGENT", DefaultAgent},
		{"TASK", DefaultTask},
		{"CONTAINER_ENGINE", DefaultContainerEngine},
	} {
		re := regexp.MustCompile(`(?m)^` + tc.variable + `\s*\?=\s*(.*?)\s*$`)
		m := re.FindSubmatch(makefile)
		if m == nil {
			t.Errorf("%s has no `?=` default in the Makefile any more", tc.variable)
			continue
		}
		if got := string(m[1]); got != tc.want {
			t.Errorf("%s: Makefile says %q, kmx says %q — one of them moved", tc.variable, got, tc.want)
		}
	}
}

// Context resolution is the difference between acting on the cluster the
// operator meant and acting on whatever was left in the environment, so
// assert the whole precedence chain.
func TestContextResolutionOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KMX_HOME", home)
	t.Setenv("KIND_CLUSTER", "mine")
	t.Setenv("KUBE_CTX", "")
	t.Setenv("CONTAINER_ENGINE", "")

	// 4. nothing set anywhere: kind-<KIND_CLUSTER>.
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if c.KubeContext != "kind-mine" || c.ContextSource != "KIND_CLUSTER" {
		t.Errorf("bare default: got %q from %q, want kind-mine from KIND_CLUSTER", c.KubeContext, c.ContextSource)
	}

	// 3. a context selected by `kmx ctx` beats the default.
	if _, err := WriteSelectedContext("kind-selected"); err != nil {
		t.Fatal(err)
	}
	if c, err = Load(""); err != nil {
		t.Fatal(err)
	}
	if c.KubeContext != "kind-selected" || c.ContextSource != "kmx ctx" {
		t.Errorf("selected: got %q from %q", c.KubeContext, c.ContextSource)
	}

	// 2. KUBE_CTX beats the selection (this is how make delegates).
	t.Setenv("KUBE_CTX", "kind-fromenv")
	if c, err = Load(""); err != nil {
		t.Fatal(err)
	}
	if c.KubeContext != "kind-fromenv" || c.ContextSource != "KUBE_CTX" {
		t.Errorf("env: got %q from %q", c.KubeContext, c.ContextSource)
	}

	// 1. --context beats everything.
	if c, err = Load("kind-flag"); err != nil {
		t.Fatal(err)
	}
	if c.KubeContext != "kind-flag" || c.ContextSource != "--context" {
		t.Errorf("flag: got %q from %q", c.KubeContext, c.ContextSource)
	}
}

// An unknown engine is refused rather than defaulted: the Makefile errors on
// it, and silently falling back to docker would create a cluster the
// operator's kind cannot see.
func TestUnknownContainerEngineIsRefused(t *testing.T) {
	t.Setenv("KMX_HOME", t.TempDir())
	t.Setenv("CONTAINER_ENGINE", "containerd")
	if _, err := Load(""); err == nil {
		t.Fatal("an unknown CONTAINER_ENGINE must be refused")
	}
}

func TestPodmanCarriesTheKindProvider(t *testing.T) {
	t.Setenv("KMX_HOME", t.TempDir())
	t.Setenv("CONTAINER_ENGINE", "podman")
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	env := c.KindEnv()
	if len(env) != 1 || env[0] != "KIND_EXPERIMENTAL_PROVIDER=podman" {
		t.Errorf("podman must carry KIND_EXPERIMENTAL_PROVIDER, got %v", env)
	}
	t.Setenv("CONTAINER_ENGINE", "docker")
	if c, err = Load(""); err != nil {
		t.Fatal(err)
	}
	if len(c.KindEnv()) != 0 {
		t.Errorf("docker must set no kind provider, got %v", c.KindEnv())
	}
}
