# Image execution in a shared AgentSuite workspace

Native and image-backed members use the same `workspace.yaml`, member YAML,
instruction files, authored cases and prepared OCI cache from the native workspace.
The initial scope remains one tool-free member. Native members retain exact model
selection and require zero images. Explicit `image-http` members declare inference
capabilities and require one platform image.

```sh
kmx agent create greeter --workspace ./my-suite --execution image-http \
  --instructions ./instructions.md --context-tokens 8192 --output-tokens 1024
kmx suite push --workspace ./my-suite "$REGISTRY/team:v1" --builder "$BUILDX_BUILDER"
kmx agent lift --release "$SUITE_DIGEST_REF" --environment target.json --plan
KAIMAHI_CONFIRM="$CONTEXT" kmx agent lift --release "$SUITE_DIGEST_REF" --environment target.json
kmx console --workspace ./my-suite --registry "$REGISTRY/team:v1" --builder "$BUILDX_BUILDER"
```

Commands discover the nearest `workspace.yaml` by walking from the current
directory toward the filesystem root. `--workspace` selects an exact root and
never falls back to another workspace. Invalid/unreadable markers stop discovery.
`suite push REFERENCE`, `suite run` and the console work from member subdirectories.
Existing two-positional extracted-directory pushes retain their meaning.
Named `agent create` inside a discovered workspace uses offline authoring; the
current one-member writer refuses an existing member. Outside a workspace the
existing live create behavior remains; use explicit `--workspace` to start one.

The console lists the selected shared workspace, not a directory of generated
`agentsuite.json` trees. `b` uses prepared publication, `l` reviews a release lift,
Enter applies it, `s` observes placements, `d` selects a placement and `c` invokes
its image. Each chat request is independent. Creation uses the same workspace
writer. Exact-model native members are not converted to capability-based images.

## Exact preparation and publication

Preparation captures bounded, allowlisted source/instruction/case bytes into the
private KMX state directory. Publication copies that exact descriptor graph,
with the same optional/required unsigned provenance policy as native workspace
push. Image build uses the extracted prepared graph, not mutable authored files.
Case changes alter the complete OCI manifest identity even if the public graph
digest stays the same. Image bindings commit to that complete suite identity.

Publication and placement records live below `KMX_HOME` (or the standard KMX state
directory), never under an authored `.kmx` directory. Old draft generated-source
trees are not auto-migrated or overwritten. Retain them for inspection and author
a new explicit workspace to use this flow.

## One release-lift command

`kmx agent lift --release registry/repository@sha256:...` is deploy-only and does
not require a workspace, source directory, builder or registry write credentials.
The existing positional `agent lift <bundle-dir>` operation is unchanged.
`suite lift`, `suite create` and `suite publish` are removed from this draft.

The image adapter requires the packaged `image-http` declaration and complete
environment member-image bindings, validates their source linkage and prepares
every member before writes. Native release dispatch can join the same typed
`PrepareReleaseLift`/`ApplyReleaseLift` boundary; it is not implemented by the
image adapter. Runtime overrides and bundle-only gate flags are refused in image
release mode rather than ignored. `--environment` remains an image binding input
pending the shared destination/evidence contract.

The environment selects context, cluster UID, namespace, image references, named
inputs and operator-declared model/endpoint capabilities. Targets, namespaces,
model endpoints and Secrets must already exist. Member execution must match the
artifact. Model selection changes configuration/plan identity and rolls out the
same image. Signer and promotion-policy checks remain open; this is not a gated
production promotion claim.

## Testing and remaining integration

The opt-in [smoke runner](../scripts/ci/suite-lift-smoke.py) creates source through
`agent create`, publishes with `suite push`, exercises `agent lift --release` with
two model bindings and invokes the deployed image. Its
[controlled model](../scripts/ci/suite-model.py) checks model, credential and
instruction inputs. The separate [fixture generator](../scripts/sample-suite.py)
is test support, not another workspace format.

The updated flow was exercised on kind and private container registry/AKS targets.
The same image served both model selections without rebuilding. Contract work
remains in #356/#345/#346/#350/#374, with native packaged evaluation in #442 and
suite workflow contracts in #443. Local records are not operation-ID recovery,
portable image association or authenticated promotion evidence. The `suitedeploy`
package remains proposal scaffolding, not the executable deployment engine.
