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
// (milestone 2) and the two governed model presets `kmx govern` applies.
//
// The files are named individually rather than embedding `k8s` wholesale, so
// that what kmx carries is a decision rather than a side effect of a
// directory listing: the Slack, GitHub, inbound and egress manifests are
// milestone 3's (D28(3)) and must not ride along, and neither must the
// hosted-model presets, which need a captured key that kmx accepts in no
// form at all (D27).
//
// `plane/` itself is NOT here and cannot be: it carries its own go.mod, and
// go:embed refuses to cross a module boundary ("cannot embed directory: in
// different module"). That is exactly why `kmx plane` FETCHES the plane's
// source from the public Go proxy at kmx's own revision and builds it, and
// why the manifest that deploys it can nevertheless travel in the binary.
//
//go:embed k8s/ollama.yaml k8s/kagent-values.yaml k8s/hello-world.yaml k8s/tools-agent.yaml
//go:embed k8s/plane/namespace.yaml k8s/plane/postgres.yaml k8s/plane/proxy.yaml
//go:embed k8s/plane/upstreams.yaml k8s/plane/network-policy.yaml
//go:embed k8s/models/governed-ollama.yaml k8s/models/governed-copilot.yaml
var Manifests embed.FS
