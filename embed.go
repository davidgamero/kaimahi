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

// Manifests holds the runtime manifests kmx applies: the Ollama model
// server, kagent's helm values, and the two agents. The files are named
// individually rather than embedding `k8s` wholesale, so the plane's
// manifests — milestone 2's job, not kmx's (D27) — cannot silently ride
// along in the binary.
//
//go:embed k8s/ollama.yaml k8s/kagent-values.yaml k8s/hello-world.yaml k8s/tools-agent.yaml
var Manifests embed.FS
