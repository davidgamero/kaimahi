package agentkit

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestMountedImageOmitsSelectedModelAndUsesRuntimeConfigABI(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sample")
	if err := agentsuite.CreateHTTPSource(root, agentsuite.CreateRequest{Name: "sample", Instructions: "immutable instructions", Inference: agentsuite.InferenceRequirements{API: "openai-chat-completions-v1", ContextTokens: 8192, OutputTokens: 1024}}); err != nil {
		t.Fatal(err)
	}
	plan, err := agentsuite.ResolveSandboxPlan(root, agentsuite.BuildSelection{})
	if err != nil {
		t.Fatal(err)
	}
	var captured agentImage
	builder := New(Options{SuiteReference: "registry.example/source@" + testDigest, SuiteDigest: testDigest, exporter: ociExporterFunc(func(_ context.Context, image agentImage, _ io.Writer) error { captured = image; return nil })})
	if _, err := builder.Build(context.Background(), *plan, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !captured.MountedConfig || captured.Agent.Model.Name != "" || captured.Agent.Model.BaseURL != "" {
		t.Fatal("model selection entered build")
	}
	definition, raw, err := agentBuildDefinition(context.Background(), captured, &ocispec.Platform{OS: "linux", Architecture: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	var image ocispec.Image
	if err := json.Unmarshal(raw, &image); err != nil {
		t.Fatal(err)
	}
	if strings.Join(image.Config.Cmd, " ") != "--config /run/agentsuite/agent.json --protocol openai" {
		t.Fatal(image.Config.Cmd)
	}
	for _, op := range definition.Def {
		if bytes.Contains(op, []byte("/agent/agent.yaml")) {
			t.Fatal("baked model ABI generated")
		}
	}
	builder.options.ModelBaseURL = "https://model.example/v1"
	if _, err := builder.Build(context.Background(), *plan, io.Discard); err == nil {
		t.Fatal("model build override accepted")
	}
}
