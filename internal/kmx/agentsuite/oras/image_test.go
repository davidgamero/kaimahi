package oras

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	spec "github.com/agent-primitives/suite"
	"github.com/kaimahi-agents/kaimahi/internal/kmx/agentsuite"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"
)

func TestImageBuildSourceBindsSupplementalContent(t *testing.T) {
	server, reg := newTestRegistryServer("", "")
	defer server.Close()
	t.Setenv("DOCKER_CONFIG", t.TempDir())
	source := filepath.Join(t.TempDir(), "source")
	if err := os.CopyFS(source, os.DirFS(filepath.Join("..", "..", "..", "..", "testdata", "agentsuite", "minimal"))); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"eval/case.yaml", "metadata/kmx/profile.json"} {
		path := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ref := strings.TrimPrefix(server.URL, "http://") + "/team/suite:v1"
	executable := filepath.Join(source, "metadata", "helper")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(executable, 0755); err != nil {
		t.Fatal(err)
	}
	pushed, err := PushRegistry(t.Context(), source, ref, true, false, PushOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pinned := strings.TrimSuffix(ref, ":v1") + "@" + pushed.Descriptor.Digest.String()
	reg.mu.RLock()
	uploads, puts := reg.uploads, reg.manifestPuts
	reg.mu.RUnlock()
	output := filepath.Join(t.TempDir(), "build")
	if err := PrepareImageBuildSource(t.Context(), source, pinned, output, true); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(output, "metadata", "helper"))
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatalf("packaged executable mode was not retained: %v %v", info, err)
	}
	if err := os.Chmod(executable, 0644); err != nil {
		t.Fatal(err)
	}
	if err := PrepareImageBuildSource(t.Context(), source, pinned, filepath.Join(t.TempDir(), "mode-drift"), true); err == nil {
		t.Fatal("mode-only source drift accepted")
	}
	if err := os.Chmod(executable, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"eval/case.yaml", "metadata/kmx/profile.json"} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(source, name), []byte("changed"), 0o600); err != nil {
				t.Fatal(err)
			}
			defer os.WriteFile(filepath.Join(source, name), []byte("original"), 0o600)
			if _, err := spec.ValidatePath(source); err != nil {
				t.Fatal("supplemental change must remain public-graph valid", err)
			}
			destination := filepath.Join(t.TempDir(), "refused")
			if err := PrepareImageBuildSource(t.Context(), source, pinned, destination, true); err == nil || !strings.Contains(err.Error(), "content differs") {
				t.Fatalf("supplemental drift accepted: %v", err)
			}
			if _, err := os.Stat(destination); !os.IsNotExist(err) {
				t.Fatal("mismatch created build source", err)
			}
			data, err := os.ReadFile(filepath.Join(output, name))
			if err != nil || string(data) != "original" {
				t.Fatal("local edit changed retained build bytes", err)
			}
		})
	}
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	if uploads != reg.uploads || puts != reg.manifestPuts {
		t.Fatal("build-source preparation wrote to the registry")
	}
}

func TestResolveImageVerifiesContractAndPlatform(t *testing.T) {
	for _, scenario := range []string{"valid", "unmarked", "root", "wrong platform", "ambiguous index", "suite artifact"} {
		t.Run(scenario, func(t *testing.T) {
			store := memory.New()
			ctx := t.Context()
			push := func(media string, value any) ocispec.Descriptor {
				data, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				d := content.NewDescriptorFromBytes(media, data)
				if err := store.Push(ctx, d, bytes.NewReader(data)); err != nil {
					t.Fatal(err)
				}
				return d
			}
			platform := spec.Platform{OS: "linux", Architecture: "amd64"}
			digest := "sha256:" + strings.Repeat("a", 64)
			record := agentsuite.ImageDeployment{SchemaVersion: spec.SpecVersion, MediaType: agentsuite.ImageDeploymentMediaType, Agent: "hello", Platform: platform, SuiteReference: "registry.example/suite@" + digest, SuiteDigest: digest, CompositionDigest: digest, BuildProfile: "default", Execution: spec.ExecutionContract{Kind: spec.ExecutionHTTPV1, Protocol: "openai-chat-v1", Port: 8080, HealthPath: "/healthz", Inputs: []spec.ExecutionInput{}}}
			record.BuildProfileDigest = digest
			label, err := agentsuite.EncodeImageDeployment(record)
			if err != nil {
				t.Fatal(err)
			}
			layer := push(ocispec.MediaTypeImageLayerGzip, "payload")
			config := ocispec.Image{Platform: ocispec.Platform{OS: "linux", Architecture: "amd64"}, Config: ocispec.ImageConfig{User: "1000", Entrypoint: []string{"/agent"}, Labels: map[string]string{agentsuite.ImageDeploymentLabel: label}}, RootFS: ocispec.RootFS{Type: "layers"}}
			config.RootFS.DiffIDs = append(config.RootFS.DiffIDs, layer.Digest)
			if scenario == "unmarked" {
				config.Config.Labels = nil
			}
			if scenario == "root" {
				config.Config.User = "0"
			}
			if scenario == "wrong platform" {
				config.Architecture = "arm64"
			}
			manifest := ocispec.Manifest{Versioned: specs.Versioned{SchemaVersion: 2}, Config: push(ocispec.MediaTypeImageConfig, config), Layers: []ocispec.Descriptor{layer}}
			if scenario == "suite artifact" {
				manifest.ArtifactType = spec.MediaTypeArtifact
			}
			root := push(ocispec.MediaTypeImageManifest, manifest)
			root.Platform = &ocispec.Platform{OS: "linux", Architecture: "amd64"}
			entries := []ocispec.Descriptor{root}
			if scenario == "ambiguous index" {
				entries = append(entries, root)
			}
			index := push(ocispec.MediaTypeImageIndex, ocispec.Index{Versioned: specs.Versioned{SchemaVersion: 2}, Manifests: entries})
			if err := store.Tag(ctx, index, "v1"); err != nil {
				t.Fatal(err)
			}
			result, err := ResolveImage(context.Background(), store, "v1", platform)
			if scenario == "valid" {
				if err != nil || result.Descriptor.Digest != root.Digest {
					t.Fatalf("result=%+v error=%v", result, err)
				}
			} else if err == nil {
				t.Fatal("invalid image accepted")
			}
		})
	}
}
