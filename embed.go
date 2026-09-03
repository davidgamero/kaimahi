// Package kaimahi exists for one reason: it is the only place in the tree
// from which a `go:embed` directive can reach k8s/.
//
// kmx is installed with `go install github.com/kaimahi-agents/kaimahi/cmd/kmx@<sha>`
// and then run from a directory that is not a clone — there is no k8s/ on
// disk to `kubectl apply -f`. Embed patterns are resolved relative to the
// source file's own directory and may not climb out of it, so the manifests
// have to be embedded by a file at the module root. That is this file, and
// that is all it does.
package kaimahi

import "embed"

// Manifests holds the manifests kmx applies: the runtime (the Ollama model
// server, kagent's helm values, the two agents), the governance plane
// (milestone 2), the model presets `kmx govern` and `kmx use` apply, and the
// governed RemoteMCPServer `kmx tools govern` puts the tools agent behind.
//
// The files are named individually rather than embedding `k8s` wholesale, so
// that what kmx carries is a decision rather than a side effect of a
// directory listing: the Slack, GitHub, inbound and egress manifests belong
// to families kmx does not own (they are entangled with secret capture,
// which D27 keeps in scripts) and must not ride along.
//
// `k8s/models/` IS embedded whole as of milestone 3, because `kmx use` is
// `make use` and `make use PRESET=anthropic` has always been a documented
// flow — a `kmx use` that handled only the keyless presets would be a
// regression the delegating recipe would inherit. This does not put a
// credential anywhere near kmx: a preset is a ModelConfig that NAMES a
// Secret (`apiKeySecret`), it never carries a key, and minting that Secret
// stays where D27 put it — `make model-secret`, `make copilot-secret`, the
// scripts. kmx still accepts a credential in no form at all.
//
// `plane/` itself is NOT here and cannot be: it carries its own go.mod, and
// go:embed refuses to cross a module boundary ("cannot embed directory: in
// different module"). That is exactly why `kmx plane` FETCHES the plane's
// source from the public Go proxy at kmx's own revision and builds it, and
// why the manifest that deploys it can nevertheless travel in the binary.
//
//go:embed k8s/ollama.yaml k8s/kagent-values.yaml k8s/hello-world.yaml k8s/tools-agent.yaml
//go:embed k8s/kaimahi-tools.yaml
//go:embed k8s/plane/namespace.yaml k8s/plane/postgres.yaml k8s/plane/proxy.yaml
//go:embed k8s/plane/upstreams.yaml k8s/plane/network-policy.yaml
//go:embed k8s/models
var Manifests embed.FS
