# Experimental image-backed suite workspace

This draft carries the image-backed lift and console slice from #342, on top of
the inference/build/publication slice #448. #447 tracks the split. The native
workspace (`agent create --workspace`, `suite push --workspace`) remains unchanged.

```sh
mkdir -p suites
kmx suite create suites/greeter --instructions 'Greet briefly.'
kmx suite publish greeter --workspace suites --registry "$REGISTRY/team" --builder "$BUILDX_BUILDER"
kmx suite lift greeter --workspace suites --environment target.json --plan
KAIMAHI_CONFIRM="$CONTEXT" kmx suite lift greeter --workspace suites --environment target.json
kmx suite run greeter --workspace suites --deployment demo --member greeter --prompt Hello
kmx console --workspace suites --registry "$REGISTRY/team" --builder "$BUILDX_BUILDER"
```

The console offers `n` source creation, `b` build/publication, `l` typed lift
review, Enter apply, `s` status, `d` placement selection and `c` single-member
independent chat requests. Environment files explicitly select target identity,
namespace, member images, model/endpoint, operator-declared inference capabilities
and Secret references. Targets, namespaces, model services and Secrets must exist.
Registry publication uses the common OCI transport and current suite provenance
defaults. ACR is one integration, not a registry type in the portable definition.

The executable JSON environment is described by `imagelift.Environment`. The
opt-in [smoke runner](../scripts/ci/suite-lift-smoke.py) produces an example,
changes the model binding without rebuilding, invokes the deployed image and
retains resources for inspection. Its [model fixture](../scripts/ci/suite-model.py)
checks the requested model, credentials and instructions. The alternative
[sample generator](../scripts/sample-suite.py) writes a deterministic test suite.

## Draft integration boundary

This is a carried prototype, not a completed promotion implementation. The
image-source directory/`.kmx` record layout still needs convergence with the
prepared native workspace and candidate/evidence work in #437/#445/#442. The
`suite create/publish` commands are temporary draft surfaces; #404/#301 own their
convergence onto common authoring/push vocabulary. Image deployment uses the
`lift` verb and does not reassign the existing root infrastructure command.

The implementation consumes public AgentSuite types, reuses the member resolver,
and uses the extracted port-forward package. Internal type aliases bridge the
carried adapters while #345/#356 settle one deployment projection. The richer
`suitedeploy` Go types remain proposal scaffolding, not the executable engine.

Still required before promotion: packaged case/image/configuration-bound
evaluation, policy and signer checks, portable member-image association,
operation-ID recovery, shared-resource/native profile handling and sandbox
inventory/binding conformance. Native members can require zero images; this
adapter is explicitly image-only. Local placement records are not release history.

The original #342 tree was exercised on kind and private registry/AKS targets.
Those historical runs do not establish live coverage for this newly split tree.
