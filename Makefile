# Thin glue over kind/AKS + helm + kubectl + the kagent CLI. No Kaimahi CLI
# here — kagent already ships one (see docs/getting-started.md for the full story).
#
# TARGET selects the environment (P5b). kind is the default and its
# behaviour is unchanged: every kind command, context, and manifest is
# exactly what it was before this file learned about anything else.
#
#   make up                      # kind, as always
#   TARGET=aks make ...          # a managed cluster (docs/aks.md)
#
# KUBE_CTX is now overridable, which is the whole point of the lane — and
# also its one new hazard, since `make down` can suddenly name a cluster
# somebody cares about. Every MUTATING target below therefore depends on
# `guard` (scripts/kube-guard.sh): it prints where the action is going,
# and demands explicit confirmation for anything that is not a local kind
# cluster. Fail closed — no confirmation, no action.
TARGET         ?= kind

# `up` is the default goal, as it has always been. Stated explicitly
# because it is no longer the first rule in the file: make takes the first
# non-dot target, which is now `guard`, so a bare `make` would otherwise
# print a banner and exit 0 — a no-op that looks like a successful run.
.DEFAULT_GOAL := up

KIND_CLUSTER   ?= kaimahi-p1
AKS_CLUSTER    ?= kaimahi
KAGENT_VERSION ?= 0.9.12
MODEL          ?= qwen2.5:3b
AGENT          ?= hello-world
TASK           ?= Hello! Who are you and where are you running?
KAGENT         ?= bin/kagent

OS   := $(shell uname -s | tr A-Z a-z)
ARCH := $(shell uname -m | sed -e s/x86_64/amd64/ -e s/aarch64/arm64/)

# The plane image. The tag moves with the phase so a stale side-loaded
# image can never satisfy a newer manifest silently (P4b deviation 6).
PLANE_IMAGE_REPO ?= kaimahi-proxy
PLANE_IMAGE_TAG  ?= p8

# ---- environment-dependent settings --------------------------------------
# Everything that genuinely differs between kind and a managed cluster is
# collected here, so the recipes below stay readable.
ifeq ($(TARGET),kind)
KUBE_CTX         ?= kind-$(KIND_CLUSTER)
# Side-loaded local image; `Never` is deliberate — see k8s/plane/proxy.yaml.
PLANE_IMAGE      ?= $(PLANE_IMAGE_REPO):$(PLANE_IMAGE_TAG)
PLANE_TARGET     := kind
# The keyless in-cluster model is the default everywhere on kind.
AGENT_MODELCONFIG ?= hello-world-model
GOVERNED_PRESET  ?= governed-ollama
# P7a: the proxy leaves the cluster only when Copilot is enabled
# (k8s/egress-copilot.yaml). Not on kind by default — the probe asserts
# the proxy is internet-free. Set to 1 after `make plane-copilot-secret`.
COPILOT_EGRESS   ?= 0
else ifeq ($(TARGET),aks)
KUBE_CTX         ?= $(AKS_CLUSTER)
# Built in Azure by `az acr build` and PULLED — a private ACR (D15), never
# a public image.
PLANE_IMAGE      ?= $(ACR_NAME).azurecr.io/$(PLANE_IMAGE_REPO):$(PLANE_IMAGE_TAG)
PLANE_TARGET     := registry
# D15: Copilot-only on AKS. No Ollama is deployed there, so the agent goes
# straight onto the governed Copilot preset rather than the ollama one.
AGENT_MODELCONFIG ?= governed-copilot
GOVERNED_PRESET  ?= governed-copilot
# Copilot-only (D15): the proxy's 443 allowance is always applied here.
COPILOT_EGRESS   ?= 1
else
$(error unknown TARGET '$(TARGET)' — expected 'kind' or 'aks')
endif

PLANE_PULL_POLICY ?= IfNotPresent
KUBECTL        := kubectl --context $(KUBE_CTX)
CRED           ?= hello-world
CRED_TOOLS     ?= hello-tools
TOOLS          ?= k8s_get_resources
# P5a: the Slack seam has its own credential, agent and allowlist. The
# read-only tool is allowlisted from the start; POSTING is not — it is
# the action a human approves (make approvals / make approve).
CRED_SLACK     ?= hello-slack
# SLACK_TOOLS is the gateway ALLOWLIST — the authority. Posting is absent
# from it deliberately; an approval is what admits the call.
SLACK_TOOLS    ?= conversations_history
SLACK_POST_TOOL := conversations_add_message
# SLACK_AGENT_TOOLS is the agent's SELECTION (kagent wires discovered ∩
# toolNames). It names the posting tool so a grant can take effect
# without editing the agent; while the tool is not allowlisted it is not
# projected, not discovered, and not in the agent's hands.
SLACK_AGENT_TOOLS ?= $(SLACK_TOOLS),$(SLACK_POST_TOOL)
# TOOLS as a JSON string array for the Agent patch, so the agent's
# toolNames stay aligned with the gateway allowlist ("-" -> empty).
comma          := ,
TOOLNAMES_JSON  = $(if $(filter -,$(TOOLS)),,"$(subst $(comma),"$(comma)",$(TOOLS))")
SLACK_TOOLNAMES_JSON = $(if $(filter -,$(SLACK_AGENT_TOOLS)),,"$(subst $(comma),"$(comma)",$(SLACK_AGENT_TOOLS))")

.PHONY: up cluster ollama model kagent agent tools-agent chat down status guard \
	model-secret copilot-secret use use-ollama \
	plane plane-image plane-secrets govern budget ledger plane-copilot-secret \
	govern-tools ungovern-tools tool-allow tool-allowlist tool-audit \
	approvals approve deny request grants approval-audit \
	slack-secret slack-mcp govern-slack slack-allow slack-audit \
	slack-post slack-down aks-cluster aks-creds aks-down \
	netpol-verify egress-copilot egress-copilot-off \
	inbound-credential inbound-secret inbound-fire inbound-audit \
	inbound-expose inbound-unexpose exposure-scan

# guard: the context-safety net every MUTATING target depends on. Prints
# the target context/namespaces; demands explicit confirmation for
# anything that is not a local kind cluster; fails closed. Make runs it
# once per invocation, so a single `make up` asks at most once.
#
# Unguarded: status, ledger, audits and the approvals lists (they read),
# and `chat`. Calling chat "read-only" would be wrong — it spends budget,
# writes a ledger row, and can burn a grant. It is unguarded because the
# line being drawn is not "mutates" but "can be aimed somewhere
# unintended": chat runs through $(KUBECTL), which carries an explicit
# --context, so it lands wherever the rest of the invocation was already
# going to land. The scripts/tool-*-probe.sh scripts mutate the same
# governance state and ARE guarded, because they run outside make and
# would otherwise follow whatever `kubectl config current-context` says —
# which `az aks get-credentials` rewrites. Mutation is why anyone cares;
# an inherited context is what makes it a surprise.
# MAKECMDGOALS is empty for a bare `make`, which would print an action-less
# banner; name the default goal instead.
guard:
	@KUBE_CTX='$(KUBE_CTX)' KUBE_NS='$(GUARD_NS)' \
		bash scripts/kube-guard.sh '$(if $(MAKECMDGOALS),$(MAKECMDGOALS),$(.DEFAULT_GOAL)) [TARGET=$(TARGET)]'

GUARD_NS ?= kagent, kaimahi, ollama

# Port-forward the kagent controller, WAIT FOR IT, then run $(1) against
# that forward. Factored out because `chat` and `slack-post` had the same
# recipe and the same defect.
#
# The old form was `port-forward ... >/dev/null 2>&1 & sleep 3` and then
# an invoke that trusted the CLI's default localhost:8083. Three problems,
# and P5b makes them reachable: running a kind and a managed verification
# at once is now a first-class workflow (docs/aks.md), and the
# ports collide.
#   1. If the bind failed because ANOTHER cluster's forward already held
#      8083, the error went to /dev/null and `kagent invoke` connected to
#      that forward instead — returning a real, plausible reply from the
#      wrong cluster. It does NOT fail closed: the controller on that
#      forward answers happily. (Demonstrated while reviewing this lane.)
#      --context cannot protect this path, because the aiming happens at
#      the socket, not at kubectl.
#   2. `sleep 3` is a guess, not a readiness check.
#   3. The port was hardcoded, so the runbook's "move the ports" advice for
#      concurrent clusters could not be applied here at all.
# Now: an overridable CHAT_PORT, an explicit --kagent-url so the CLI cannot
# fall back to a port we did not open, and a wait for kubectl's own
# "Forwarding from" line that fails loudly if it never appears.
CHAT_PORT ?= 8083
# The CLI defaults to localhost:8083; name the port we actually opened so
# it can never fall back to someone else's.
KAGENT_INVOKE = $(KAGENT) --kagent-url http://127.0.0.1:$(CHAT_PORT) invoke

# $(1) is the agent whose Service must be servable; $(2) is the command;
# $(3) optionally narrows which transport failures are retried (see below).
#
# Before invoking, prove the agent is actually SERVABLE — do not infer it.
#
# `use` already waits (`wait_switched`: kagent reconcile, `rollout status`,
# the single-pod wait; then the Agent's Ready condition) and none of it
# is sufficient during a preset-switch rollout: CI failed here twice with
#   dial tcp <clusterIP>:8080: connect: connection refused
# because at the moment of the call the Service had no ready backend (the
# old pod removed, the new one not yet propagated) — kube-proxy REJECTs
# that, so it looks like a broken agent rather than a race. Checking the
# endpoint list is also too weak: it can read ready one instant and be
# empty the next.
#
# So make the check the same thing the caller needs: fetch the agent's own
# A2A card THROUGH the Service, via the API server's service proxy. That
# resolves endpoints server-side and returns a real HTTP body, so it fails
# while there is no ready backend and succeeds only once the agent answers.
# (The kagent readiness probe uses the same path.) One kubectl call — no
# extra port, no second forward.
#
# This replaces the `sleep 3` the recipe used to rely on. That sleep was
# quietly doing this job: it is why `main` passes and why removing it
# surfaced the race. Padding is not a readiness check.
#
# The probe alone is NOT sufficient, and it is worth being precise about
# why: the API server's service proxy resolves the endpoint and connects to
# the POD directly, so it can succeed while kube-proxy has not yet
# programmed the ClusterIP the controller dials. Only the controller can
# answer "can I reach the agent", so the invoke below additionally retries
# a bounded number of times on exactly that error. It cannot mask a real
# outage: after the retries the original output and exit status are
# emitted unchanged, and a transport-error reply still fails
# verify-chat.py. Note kagent exits 0 on this error, so the retry keys on
# the message, not the status.
#
# The race has more than one symptom, and the first fix only caught one of
# them. `connection refused` is kube-proxy REJECTing when the Service has
# no ready backend; but once a backend IS programmed and the pod tears the
# connection down before answering, the controller reports instead:
#   failed to send HTTP request: Post "http://<agent>.kagent:8080": EOF
# That is the same race one moment later, and it reddened main after P5b
# merged. Retry both.
#
# The predicate is anchored to the controller's WHOLE error line, not to
# transport text anywhere in the output. The output being matched is the
# combined stdout+stderr, and stdout carries the A2A task JSON — including
# the model's own reply. An agent asked to explain one of these errors
# (the FAQ documents them) would echo the words and, with a loose match,
# trigger a second invoke: duplicate spend, and for tool calls a burned
# grant. kagent prints the failure as one line, "Error invoking session:
# <wrapped error>", ending in Go's net error; anchor both ends so a line
# that starts with `{` can never match.
#
# Two classes, because they are not equally safe to retry:
#   REFUSED  — kube-proxy REJECTed; nothing reached the agent. Always safe.
#   AMBIGUOUS — EOF / connection reset: the request may have reached the
#              agent and been acted on before the connection dropped.
# `chat` retries both (re-asking a question is acceptable). Anything whose
# task performs a non-idempotent action — slack-post, which POSTS to a
# channel under a USES-bounded grant — retries only the refused class:
# a retry after an ambiguous failure could post twice. Pass the class as
# $(3); it defaults to both.
CHAT_ERROR_LINE  = ^Error invoking session: .*failed to send HTTP request: Post "[^"]*": 
CHAT_REFUSED     = dial tcp [^ ]*: connect: connection refused
CHAT_AMBIGUOUS   = EOF|(read|write) tcp [^ ]*: (read|write): connection reset by peer
CHAT_RETRYABLE      = '$(CHAT_ERROR_LINE)($(CHAT_REFUSED)|$(CHAT_AMBIGUOUS))$$'
CHAT_RETRYABLE_SAFE = '$(CHAT_ERROR_LINE)($(CHAT_REFUSED))$$'
define kagent_forward
agent_ok=; \
for _ in $$(seq 1 120); do \
	if $(KUBECTL) -n kagent get --raw \
		'/api/v1/namespaces/kagent/services/$(1):8080/proxy/.well-known/agent-card.json' \
		>/dev/null 2>&1; then agent_ok=1; break; fi; \
	sleep 1; \
done; \
if [ -z "$$agent_ok" ]; then \
	echo "agent '$(1)' is not answering through its Service after 120s — refusing to invoke" >&2; \
	echo "  (invoking now would fail with a transport error from the controller)" >&2; \
	exit 1; \
fi; \
pf_out=$$(mktemp); \
$(KUBECTL) -n kagent port-forward --address 127.0.0.1 \
	svc/kagent-controller $(CHAT_PORT):8083 >"$$pf_out" 2>&1 & \
pf=$$!; trap 'kill $$pf 2>/dev/null; rm -f "$$pf_out"' EXIT; \
ready=; \
for _ in $$(seq 1 80); do \
	if grep -q "Forwarding from 127.0.0.1:$(CHAT_PORT)" "$$pf_out" 2>/dev/null; then ready=1; break; fi; \
	kill -0 $$pf 2>/dev/null || break; \
	sleep 0.25; \
done; \
if [ -z "$$ready" ]; then \
	echo "port-forward to kagent-controller never came up on 127.0.0.1:$(CHAT_PORT):" >&2; \
	sed 's/^/  /' "$$pf_out" >&2; \
	echo "  Refusing to invoke: if another cluster's forward holds this port," >&2; \
	echo "  the task would have run THERE. Use CHAT_PORT=<free port>." >&2; \
	exit 1; \
fi; \
out=$$(mktemp); rc=0; \
for attempt in 1 2 3 4; do \
	rc=0; $(2) >"$$out" 2>&1 || rc=$$?; \
	grep -Eq $(if $(3),$(3),$(CHAT_RETRYABLE)) "$$out" || break; \
	if [ "$$attempt" != 4 ]; then \
		echo "kagent could not reach agent '$(1)' yet (transport error); retry $$attempt/3 in 5s" >&2; \
		sleep 5; \
	fi; \
done; \
cat "$$out"; rm -f "$$out"; \
exit $$rc
endef

# The `up` journey differs by environment. On kind it is unchanged. On AKS
# there is no Ollama (D15: Copilot-only), and governance has to exist
# BEFORE the agents do, because the agents go straight onto the governed
# Copilot preset — there is no keyless model for them to start on.
ifeq ($(TARGET),kind)
UP_STEPS := cluster ollama model kagent agent tools-agent status
else
# The Copilot credential is minted BEFORE the plane is deployed, not
# after. The proxy mounts kaimahi-copilot-token as an OPTIONAL secret
# volume, so a pod that starts before the Secret exists comes up with an
# empty mount and every governed Copilot call fails closed with "upstream
# credential unavailable" until kubelet gets round to projecting it.
# Minting first makes the first chat on a fresh cluster work. (kind never
# hit this: its governed demo path is ollama, which needs no upstream
# credential.)
UP_STEPS := cluster kagent plane-copilot-secret plane govern agent \
	tools-agent govern-tools status
endif

## up: everything from an empty machine to ready agents (hello-world + tools)
#
# `up` deliberately does NOT list `guard` itself — each step below does,
# and make runs it once per invocation, so the prompt still happens
# exactly once. Guarding here too would break the headline case: on a
# genuinely empty Azure subscription the AKS context does not exist yet,
# and an absent NON-kind context is precisely what the guard refuses as a
# typo. The first step (`cluster`) is what brings that context into
# existence; every step after it is guarded, by which time there is a real
# context to check. (`kind` is unaffected either way: its first step is
# `cluster: guard`, so the guard still runs before anything is touched.)
up: $(UP_STEPS)

ifeq ($(TARGET),kind)
cluster: guard
	kind get clusters 2>/dev/null | grep -qx '$(KIND_CLUSTER)' || \
		kind create cluster --name $(KIND_CLUSTER)
else
## cluster (TARGET=aks): resource group + private ACR + AKS, via the az CLI
cluster: aks-cluster
endif

# Ollama is the kind path's keyless model server. On AKS it is deliberately
# not deployed (D15): the keyless path is already proven on kind by CI on
# every PR, and AKS's job is proving the plane runs on a managed cluster
# with a real model. Refuse loudly rather than half-deploying it.
#
# The non-kind forms carry no `guard`: they touch nothing, so making the
# operator confirm a cluster before being told the target does not apply
# there is pure friction — and friction is what teaches people to type
# past confirmations.
ifeq ($(TARGET),kind)
ollama: guard
	$(KUBECTL) apply -f k8s/ollama.yaml
	$(KUBECTL) -n ollama rollout status deploy/ollama --timeout=300s

model: guard
	$(KUBECTL) -n ollama exec deploy/ollama -- ollama pull $(MODEL)
else
ollama:
	@echo 'ollama is not deployed on TARGET=$(TARGET) — the managed path is' >&2
	@echo 'Copilot-only (D15). See docs/aks.md.' >&2
	@exit 1

model:
	@echo 'no Ollama on TARGET=$(TARGET) — nothing to pull (D15).' >&2
	@exit 1
endif

kagent: guard
	helm upgrade --install kagent-crds \
		oci://ghcr.io/kagent-dev/kagent/helm/kagent-crds \
		--version $(KAGENT_VERSION) --namespace kagent --create-namespace \
		--kube-context $(KUBE_CTX)
	helm upgrade --install kagent \
		oci://ghcr.io/kagent-dev/kagent/helm/kagent \
		--version $(KAGENT_VERSION) --namespace kagent \
		--kube-context $(KUBE_CTX) -f k8s/kagent-values.yaml
	$(KUBECTL) -n kagent wait --for=condition=Ready pods --all --timeout=420s

# Re-applying the committed YAML must not silently drop governance (or
# any preset switch) from a live agent: capture the current modelConfig
# first and restore a non-default one after the apply, with a warning.
# Only a NotFound (fresh cluster) may skip the capture — any other read
# failure aborts rather than risk silently un-governing.
#
# P5b generalises the same mechanism one step. The committed artifact
# names the keyless ollama ModelConfig, which does not exist on a
# Copilot-only managed cluster, so the desired config is:
#   a live non-default one (preserve it, as before)  else
#   $(AGENT_MODELCONFIG)  — hello-world-model on kind (identical to the
#   previous behaviour: the patch branch is simply never taken), and the
#   governed Copilot preset on AKS.
# k8s/hello-world.yaml is still never mutated.
agent: guard
	@current=""; \
	if out=$$($(KUBECTL) -n kagent get agent hello-world \
		-o jsonpath='{.spec.declarative.modelConfig}' 2>&1); then \
		current=$$out; \
	elif ! printf '%s' "$$out" | grep -q 'NotFound'; then \
		echo "cannot read hello-world's live modelConfig (refusing to risk un-governing it): $$out" >&2; exit 1; \
	fi; \
	desired='$(AGENT_MODELCONFIG)'; \
	if [ -n "$$current" ] && [ "$$current" != hello-world-model ]; then \
		desired=$$current; \
		echo "NOTE: hello-world was on modelConfig '$$current' — preserving it ('make use PRESET=ollama' resets)" >&2; \
	fi; \
	$(KUBECTL) apply -f k8s/hello-world.yaml && \
	if [ "$$desired" != hello-world-model ]; then \
		$(KUBECTL) -n kagent patch agent hello-world --type merge \
			-p "{\"spec\":{\"declarative\":{\"modelConfig\":\"$$desired\"}}}"; \
	fi
	$(KUBECTL) -n kagent wait \
		--for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True \
		agent/hello-world --timeout=300s

## tools-agent: the P3 tools-enabled agent (kagent-tools MCP server comes
## from the kagent helm install; this applies the Agent wired to it)
# Same desired-modelConfig treatment as `agent` above — but note this one
# IS a behavioural delta on kind, not just a generalisation: previously
# `tools-agent` never read modelConfig, so re-applying always reset
# hello-tools to the committed value. It now preserves a live non-default
# one. That is deliberate and matches P4c's governance-preservation guard
# (which already covers hello-tools' gateway wiring, just below); it is
# unreachable in every documented kind workflow, because nothing switches
# hello-tools' model — `make use` and `make govern` only touch
# hello-world. It matters on AKS, where hello-tools must come up on the
# governed Copilot preset.
tools-agent: guard
	$(KUBECTL) -n kagent wait \
		--for=jsonpath='{.status.conditions[?(@.type=="Accepted")].status}'=True \
		remotemcpserver/kagent-tool-server --timeout=300s
	@server=""; tools=""; current=""; \
	if out=$$($(KUBECTL) -n kagent get agent hello-tools -o json 2>&1); then \
		server=$$(printf '%s' "$$out" | python3 -c 'import json,sys; t=(json.load(sys.stdin)["spec"].get("declarative") or {}).get("tools") or []; print((t[0].get("mcpServer") or {}).get("name","") if t else "")') || exit 1; \
		tools=$$(printf '%s' "$$out" | python3 -c 'import json,sys; print(json.dumps((json.load(sys.stdin)["spec"].get("declarative") or {}).get("tools") or []))') || exit 1; \
		current=$$(printf '%s' "$$out" | python3 -c 'import json,sys; print((json.load(sys.stdin)["spec"].get("declarative") or {}).get("modelConfig",""))') || exit 1; \
	elif ! printf '%s' "$$out" | grep -q 'NotFound'; then \
		echo "cannot read hello-tools' live tool wiring (refusing to risk un-governing it): $$out" >&2; exit 1; \
	fi; \
	desired='$(AGENT_MODELCONFIG)'; \
	if [ -n "$$current" ] && [ "$$current" != hello-world-model ]; then desired=$$current; fi; \
	$(KUBECTL) apply -f k8s/tools-agent.yaml && \
	if [ "$$desired" != hello-world-model ]; then \
		$(KUBECTL) -n kagent patch agent hello-tools --type merge \
			-p "{\"spec\":{\"declarative\":{\"modelConfig\":\"$$desired\"}}}"; \
	fi && \
	if [ "$$server" = kaimahi-tools ] && [ -n "$$tools" ]; then \
		echo "NOTE: hello-tools was governed via kaimahi-tools — restoring gateway wiring ('make ungovern-tools' opts out)" >&2; \
		$(KUBECTL) -n kagent patch agent hello-tools --type merge \
			-p "{\"spec\":{\"declarative\":{\"tools\":$$tools}}}"; \
	fi
	$(KUBECTL) -n kagent wait \
		--for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True \
		agent/hello-tools --timeout=300s

## chat: one question to an agent via the kagent CLI (override with TASK=...,
## AGENT=hello-tools for the P3 tools agent)
chat: $(KAGENT)
	@$(call kagent_forward,$(AGENT),$(KAGENT_INVOKE) --agent $(AGENT) --task "$(TASK)")

## model-secret: store an API key as a K8s Secret, stdin-only (paste, Enter, Ctrl-D).
# The key never touches argv, env listings, YAML, or logs; tr strips the
# trailing newline so it doesn't corrupt the Authorization header.
model-secret: guard
	@test -n "$(NAME)" || { echo 'usage: make model-secret NAME=<preset>-api-key' >&2; exit 1; }
	@echo 'Paste the API key, press Enter, then Ctrl-D:' >&2
	@tr -d '\n' | $(KUBECTL) -n kagent create secret generic $(NAME) \
		--from-file=api-key=/dev/stdin

## copilot-secret: GitHub device login (cached), then mint a short-lived
## Copilot API token and store it as the github-copilot-token Secret.
## Fail-closed, token bytes only in pipes/0600 files — see the script.
copilot-secret: guard
	@KUBECTL="$(KUBECTL)" bash scripts/copilot-secret.sh

# Wait until an agent's switch has fully landed: kagent has reconciled the
# Agent's current generation, the Deployment has rolled, and it is served
# by EXACTLY ONE pod carrying the current pod-template-hash. $(1) is the
# agent name. Replaces the bare `rollout status` every switch used to do.
#
# Three waits, because each of the obvious signals is wrong on its own:
#
# 1. kagent reconciles the Agent ASYNCHRONOUSLY. Straight after the patch,
#    `rollout status` can run before the controller has rewritten the
#    Deployment and report "successfully rolled out" about the OLD
#    template. So first wait for status.observedGeneration to reach the
#    Agent's generation. (The Ready condition is useless here: it stays
#    True, at the old observedGeneration, for the whole rollout.)
# 2. `rollout status` is then the right rollout signal, but with maxSurge
#    1 / maxUnavailable 0 it returns the moment the NEW pod is Ready —
#    while the OLD pod, still on the previous ModelConfig, is Terminating
#    (the ReplicaSet controller stops counting a pod as soon as it has a
#    deletionTimestamp, so the rollout looks complete). A chat in that
#    window can land on the old pod: a governed chat once completed with
#    an EMPTY ledger because the ungoverned pod answered it. "Governed"
#    must mean the ungoverned pod is gone, not outnumbered.
# 3. So finally wait for the pod set. The current hash is read from the
#    ReplicaSet whose revision matches the Deployment's (kagent stamps a
#    config-hash on the pod template, so a ModelConfig switch that changes
#    anything cuts a new ReplicaSet; one that changes nothing — e.g.
#    hello-world-model → ollama, identical specs — rolls nothing and
#    passes straight through). The pod listing is compared as a whole: it
#    equals the hash only when there is one pod and it is that pod.
#    Terminating pods still list, which is the point.
#
# Bounded: after `rollout status` the remaining work is the old pod's
# termination grace, so 120s is generous; on timeout the pod list is
# printed and the target fails rather than handing a half-switched agent
# to the next step. Wait 1 keys on the AGENT's generation, so a switch
# that changes only a referenced object's content (the preset's YAML
# edited, the agent already on it) passes it at once; `use` covers that
# case itself, before calling here — see the recipe.
define wait_switched
gen=$$($(KUBECTL) -n kagent get agent/$(1) -o jsonpath='{.metadata.generation}') \
	&& [ -n "$$gen" ] || { echo "cannot read agent/$(1)'s generation" >&2; exit 1; }; \
$(KUBECTL) -n kagent wait --for=jsonpath='{.status.observedGeneration}'=$$gen \
	agent/$(1) --timeout=120s >/dev/null || exit 1; \
$(KUBECTL) -n kagent rollout status deploy/$(1) --timeout=180s || exit 1; \
rev=$$($(KUBECTL) -n kagent get deploy/$(1) \
	-o jsonpath='{.metadata.annotations.deployment\.kubernetes\.io/revision}') \
	&& [ -n "$$rev" ] || { echo "cannot read deploy/$(1)'s revision" >&2; exit 1; }; \
hash=$$($(KUBECTL) -n kagent get rs -l kagent=$(1) \
	-o jsonpath='{range .items[*]}{.metadata.annotations.deployment\.kubernetes\.io/revision} {.metadata.labels.pod-template-hash}{"\n"}{end}' \
	| awk -v r="$$rev" '$$1==r{print $$2}'); \
[ -n "$$hash" ] || { echo "no ReplicaSet at revision $$rev for deploy/$(1)" >&2; exit 1; }; \
single=; \
for _ in $$(seq 1 60); do \
	pods=$$($(KUBECTL) -n kagent get pods -l kagent=$(1) \
		-o jsonpath='{range .items[*]}{.metadata.labels.pod-template-hash}{"\n"}{end}') || exit 1; \
	if [ "$$pods" = "$$hash" ]; then single=1; break; fi; \
	sleep 2; \
done; \
if [ -z "$$single" ]; then \
	echo "deploy/$(1): still not exactly one pod on template $$hash after 120s:" >&2; \
	$(KUBECTL) -n kagent get pods -l kagent=$(1) -o wide >&2; \
	exit 1; \
fi
endef

## use: switch the hello-world agent to a model preset from k8s/models/
# (e.g. make use PRESET=anthropic). Hosted presets need their Secret first
# (make model-secret) — and remember: P2 spend is ungoverned until P4.
#
# One shell for apply + patch, because the wait that follows needs three
# values from BEFORE them. `wait_switched` keys its reconcile wait on the
# Agent's generation, which only moves when the Agent's spec does. Two
# switches leave it still and yet must roll the pods:
#   - the preset's YAML changed and hello-world is already on it (the
#     patch is a no-op; kagent rolls on the ModelConfig change alone);
#   - the preset was just CREATED under a name the Agent already carried.
# Verified 2026-09-02: a content-only ModelConfig change bumps its
# generation and kagent cuts a new Deployment revision within a second —
# without an Agent generation change. So when the ModelConfig's
# generation moved and the Agent's did not, wait (bounded, loud) for the
# Deployment's revision to advance past what it was before the apply;
# only then is `wait_switched`'s rollout/pod check looking at the new
# template. Identical content ("unchanged") moves neither generation and
# takes the fast path. Only a genuine NotFound may leave the "before"
# generation empty; any other read failure aborts.
use: guard
	@test -n "$(PRESET)" || { echo 'usage: make use PRESET=<name from k8s/models/>' >&2; exit 1; }
	@mc0=""; \
	if out=$$($(KUBECTL) -n kagent get modelconfig/$(PRESET) -o jsonpath='{.metadata.generation}' 2>&1); then \
		mc0=$$out; \
	elif ! printf '%s' "$$out" | grep -q 'NotFound'; then \
		echo "cannot read modelconfig/$(PRESET): $$out" >&2; exit 1; \
	fi; \
	agent0=$$($(KUBECTL) -n kagent get agent/hello-world -o jsonpath='{.metadata.generation}') || exit 1; \
	rev0=$$($(KUBECTL) -n kagent get deploy/hello-world \
		-o jsonpath='{.metadata.annotations.deployment\.kubernetes\.io/revision}') || exit 1; \
	echo "$(KUBECTL) apply -f k8s/models/$(PRESET).yaml"; \
	$(KUBECTL) apply -f k8s/models/$(PRESET).yaml || exit 1; \
	echo "$(KUBECTL) -n kagent patch agent hello-world (modelConfig: $(PRESET))"; \
	$(KUBECTL) -n kagent patch agent hello-world --type merge \
		-p '{"spec":{"declarative":{"modelConfig":"$(PRESET)"}}}' || exit 1; \
	mc1=$$($(KUBECTL) -n kagent get modelconfig/$(PRESET) -o jsonpath='{.metadata.generation}') || exit 1; \
	agent1=$$($(KUBECTL) -n kagent get agent/hello-world -o jsonpath='{.metadata.generation}') || exit 1; \
	if [ "$$agent1" = "$$agent0" ] && [ "$$mc1" != "$$mc0" ]; then \
		echo "NOTE: preset '$(PRESET)' changed while hello-world was already on it — waiting for kagent to cut a new revision (was $$rev0)" >&2; \
		rolled=; \
		for _ in $$(seq 1 60); do \
			rev=$$($(KUBECTL) -n kagent get deploy/hello-world \
				-o jsonpath='{.metadata.annotations.deployment\.kubernetes\.io/revision}') || exit 1; \
			if [ -n "$$rev" ] && [ "$$rev" -gt "$${rev0:-0}" ]; then rolled=1; break; fi; \
			sleep 2; \
		done; \
		if [ -z "$$rolled" ]; then \
			echo "deploy/hello-world: revision still $$rev0 after 120s — kagent did not roll for the changed preset; refusing to call it switched" >&2; \
			exit 1; \
		fi; \
	fi
	@$(call wait_switched,hello-world)
	$(KUBECTL) -n kagent wait \
		--for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True \
		agent/hello-world --timeout=300s

## use-ollama: switch back to the keyless in-cluster model
# The confirmation is passed down deliberately: reaching this line means
# the guard above already asked about THIS context and was answered, so
# the sub-make must not ask a second time for the same action.
use-ollama: guard
	$(MAKE) use PRESET=ollama KAIMAHI_CONFIRM='$(KUBE_CTX)'

## ---- P4a: the governance plane (docs/spend.md) ----

## plane: build + deploy the Kaimahi proxy and its Postgres ledger
plane: guard plane-image plane-secrets
	@KUBECTL="$(KUBECTL)" PLANE_TARGET=$(PLANE_TARGET) \
		PLANE_IMAGE='$(PLANE_IMAGE)' PLANE_PULL_POLICY=$(PLANE_PULL_POLICY) \
		bash scripts/plane-deploy.sh
	$(KUBECTL) -n kaimahi rollout status deploy/kaimahi-postgres --timeout=300s
	@# Always restart: a rebuilt image under the SAME tag leaves the spec
	@# unchanged, so apply alone would keep the old binary running (kind's
	@# imagePullPolicy: Never reuses same-tag images without complaint, and
	@# a registry target with IfNotPresent behaves the same way).
	$(KUBECTL) -n kaimahi rollout restart deploy/kaimahi-proxy
	$(KUBECTL) -n kaimahi rollout status deploy/kaimahi-proxy --timeout=300s

ifeq ($(TARGET),kind)
plane-image:
	docker build -t $(PLANE_IMAGE) plane/
	kind load docker-image $(PLANE_IMAGE) --name $(KIND_CLUSTER)
else
## plane-image (TARGET=aks): build IN Azure with ACR Tasks. No local docker
## build and no `docker push`: the source is uploaded and built by the
## registry, so nothing has to be logged in to a registry locally and no
## image ever leaves the private ACR.
plane-image:
	@test -n "$(ACR_NAME)" || \
		{ echo 'ACR_NAME is required for TARGET=aks (see docs/aks.md)' >&2; exit 1; }
	az acr build --registry $(ACR_NAME) \
		--image $(PLANE_IMAGE_REPO):$(PLANE_IMAGE_TAG) plane/
endif

plane-secrets: guard
	@KUBECTL="$(KUBECTL)" bash scripts/plane-secrets.sh

## govern: issue the Kaimahi credential (opaque token -> agent-side
## Secret), apply the governed presets, switch hello-world through the
## proxy. The agent never sees a real upstream key.
#
# P5b: both governed presets are applied on every target, but which one
# the agent is switched to depends on the environment ($(GOVERNED_PRESET):
# governed-ollama on kind, governed-copilot on AKS where no Ollama exists).
# The switch is also skipped when the agent is not there yet — on a
# managed cluster governance is stood up BEFORE the agents, because the
# agents have no keyless model to start on. On kind the agent always
# exists by this point, so the path taken is the one it always was.
govern: guard
	@KUBECTL="$(KUBECTL)" bash scripts/plane-admin.sh issue $(CRED)
	$(KUBECTL) apply -f k8s/models/governed-ollama.yaml
	$(KUBECTL) apply -f k8s/models/governed-copilot.yaml
	@# Only a genuine NotFound may skip the switch. `>/dev/null 2>&1` would
	@# collapse NotFound with an unreachable API server, an expired
	@# credential, an RBAC denial or a wrong context — every one of which
	@# would print the reassuring NOTE, exit 0, and leave hello-world on an
	@# UNGOVERNED preset, spending outside the plane. Same discrimination
	@# the `agent` target above already applies for the same reason.
	@if out=$$($(KUBECTL) -n kagent get agent hello-world 2>&1); then \
		$(MAKE) use PRESET=$(GOVERNED_PRESET) KAIMAHI_CONFIRM='$(KUBE_CTX)'; \
	elif printf '%s' "$$out" | grep -q 'NotFound'; then \
		echo "NOTE: agent hello-world does not exist yet — it will be created on '$(AGENT_MODELCONFIG)' by 'make agent'" >&2; \
	else \
		echo "cannot tell whether hello-world exists (refusing to leave it ungoverned): $$out" >&2; exit 1; \
	fi

## budget: set monthly caps for a credential, e.g.
##   make budget CAP_CENTS=100 CAP_TOKENS=-     (- or empty = no cap)
budget: guard
	@KUBECTL="$(KUBECTL)" bash scripts/plane-admin.sh budget $(CRED) \
		"$(if $(CAP_CENTS),$(CAP_CENTS),-)" "$(if $(CAP_TOKENS),$(CAP_TOKENS),-)"

## ledger: show the spend ledger (newest first) + month-to-date totals
ledger:
	@KUBECTL="$(KUBECTL)" bash scripts/plane-admin.sh ledger $(CRED)

## ---- P4b: the enforcing MCP gateway (docs/tool-governance.md) ----

## govern-tools: put the tools agent behind the MCP gateway — issue its
## kmh_ credential (agent-side Secret kaimahi-tools-token), set the
## default allowlist, apply the Kaimahi RemoteMCPServer, repoint
## hello-tools at it. `make chat AGENT=hello-tools` then rides the
## gateway: authenticated, allowlisted, audited.
govern-tools: guard
	@KUBECTL="$(KUBECTL)" GOVERNED_SECRET=kaimahi-tools-token \
		bash scripts/plane-admin.sh issue $(CRED_TOOLS)
	@KUBECTL="$(KUBECTL)" bash scripts/plane-admin.sh tool-allow $(CRED_TOOLS) "$(TOOLS)"
	$(KUBECTL) apply -f k8s/kaimahi-tools.yaml
	$(KUBECTL) -n kagent wait \
		--for=jsonpath='{.status.conditions[?(@.type=="Accepted")].status}'=True \
		remotemcpserver/kaimahi-tools --timeout=300s
	$(KUBECTL) -n kagent patch agent hello-tools --type merge \
		-p '{"spec":{"declarative":{"tools":[{"type":"McpServer","mcpServer":{"apiGroup":"kagent.dev","kind":"RemoteMCPServer","name":"kaimahi-tools","toolNames":[$(TOOLNAMES_JSON)]}}]}}}'
	@$(call wait_switched,hello-tools)
	$(KUBECTL) -n kagent wait \
		--for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True \
		agent/hello-tools --timeout=300s

## ungovern-tools: restore the P3 wiring (direct to the chart-managed
## tool server, ungoverned) by re-applying the committed Agent YAML
ungovern-tools: guard
	$(KUBECTL) apply -f k8s/tools-agent.yaml
	@$(call wait_switched,hello-tools)

## tool-allow: replace the tools credential's allowlist, e.g.
##   make tool-allow TOOLS=k8s_get_resources,k8s_get_events
##   make tool-allow TOOLS=-        (empty: nothing callable)
tool-allow: guard
	@KUBECTL="$(KUBECTL)" bash scripts/plane-admin.sh tool-allow $(CRED_TOOLS) "$(TOOLS)"

## tool-allowlist: show the tools credential's allowlist
tool-allowlist:
	@KUBECTL="$(KUBECTL)" bash scripts/plane-admin.sh tool-allowlist $(CRED_TOOLS)

## tool-audit: show the tool-call audit trail (newest first)
tool-audit:
	@KUBECTL="$(KUBECTL)" bash scripts/plane-admin.sh tool-audit $(CRED_TOOLS)

## ---- P4c: approvals and time-boxed permits (docs/approvals.md) ----

## approvals: list pending approval requests (denied actions file them
## automatically; `make request` files one explicitly)
approvals:
	@KUBECTL="$(KUBECTL)" bash scripts/plane-admin.sh approvals

## approve: approve a pending request with BOUNDS (at least one of TTL/
## USES required; AMOUNT tokens-or-cents only for budget requests), e.g.
##   make approve ID=<uuid> TTL=60s USES=1
##   make approve ID=<uuid> TTL=5m AMOUNT=100000
approve: guard
	@test -n "$(ID)" || { echo 'usage: make approve ID=<uuid> [TTL=60s] [USES=1] [AMOUNT=n]' >&2; exit 1; }
	@KUBECTL="$(KUBECTL)" bash scripts/plane-admin.sh approve "$(ID)" \
		"$(if $(TTL),$(TTL),-)" "$(if $(USES),$(USES),-)" "$(if $(AMOUNT),$(AMOUNT),-)"

## deny: deny a pending request
deny: guard
	@test -n "$(ID)" || { echo 'usage: make deny ID=<uuid>' >&2; exit 1; }
	@KUBECTL="$(KUBECTL)" bash scripts/plane-admin.sh deny "$(ID)"

## request: file an approval request explicitly, e.g.
##   make request KIND=tool SUBJECT=k8s_get_events
##   make request KIND=budget SUBJECT=tokens CRED=hello-world
request: guard
	@test -n "$(KIND)" && test -n "$(SUBJECT)" || \
		{ echo 'usage: make request KIND=tool|budget SUBJECT=<tool|tokens|cents> [CRED=...]' >&2; exit 1; }
	@KUBECTL="$(KUBECTL)" bash scripts/plane-admin.sh request "$(REQ_CRED)" "$(KIND)" "$(SUBJECT)"

# The filing credential: an explicit CRED= wins; otherwise tool requests
# default to the tools credential and budget requests to the chat one.
REQ_CRED = $(if $(filter command line,$(origin CRED)),$(CRED),$(if $(filter tool,$(KIND)),$(CRED_TOOLS),$(CRED)))

## grants: list grants with liveness (an expired grant is not a grant)
grants:
	@KUBECTL="$(KUBECTL)" bash scripts/plane-admin.sh grants

## approval-audit: the approvals' own audit trail (filed/approved/denied)
approval-audit:
	@KUBECTL="$(KUBECTL)" bash scripts/plane-admin.sh approval-audit

## plane-copilot-secret: mint the Copilot token into the PROXY's
## namespace (real-key custody: the agent-side governed preset never
## holds it). Re-run to rotate; the proxy picks it up without a restart.
plane-copilot-secret: guard
	@KUBECTL="$(KUBECTL)" COPILOT_SECRET_NAMESPACE=kaimahi \
		COPILOT_SECRET_NAME=kaimahi-copilot-token \
		bash scripts/copilot-secret.sh
	@# P7a: enabling Copilot is the moment the proxy needs the internet.
	@# The plane's own boundary (k8s/plane/network-policy.yaml) lets it
	@# reach nothing outside the cluster; this opens TCP 443 out, and
	@# only for the proxy. `make egress-copilot-off` closes it again.
	@# (The script above created the namespace if it did not exist, so
	@# this also works in the AKS ordering, where the token is minted
	@# before the plane is deployed.)
	$(KUBECTL) apply -f k8s/egress-copilot.yaml

status:
	$(KUBECTL) -n kagent get agents,modelconfigs
	$(KUBECTL) -n kagent get pods

ifeq ($(TARGET),kind)
## down: delete the local kind cluster
down: guard
	@# The guard vouches for $(KUBE_CTX); this recipe deletes
	@# $(KIND_CLUSTER). They agree by default, but both are overridable, so
	@# `KUBE_CTX=kind-safe make down KIND_CLUSTER=other` would show a banner
	@# naming one cluster and destroy another. Refuse when the thing
	@# confirmed is not the thing deleted.
	@test '$(KUBE_CTX)' = 'kind-$(KIND_CLUSTER)' || { \
		echo 'refusing: the guard checked context $(KUBE_CTX), but this would' >&2; \
		echo 'delete kind cluster "$(KIND_CLUSTER)" (context kind-$(KIND_CLUSTER)).' >&2; \
		echo 'Set KIND_CLUSTER and KUBE_CTX consistently, or just KIND_CLUSTER.' >&2; \
		exit 1; }
	kind delete cluster --name $(KIND_CLUSTER)
else
## down (TARGET=aks): delete the whole ephemeral resource group
down: aks-down
endif

## ---- P5b: the managed-cluster path (docs/aks.md) ----
#
# Azure identifiers are supplied by the operator and never committed:
#   AKS_RESOURCE_GROUP  required   the group these scripts create/delete
#   ACR_NAME            required   globally-unique private registry name
#   AKS_CLUSTER         optional   cluster + kube-context (default kaimahi)
#   AKS_LOCATION        optional   default westus3
#   AKS_NODE_SIZE       optional   default Standard_B4ms
#   AKS_NODE_COUNT      optional   default 1
#   AKS_NETWORK_POLICY  optional   cilium (default) | azure | calico — the
#                                  policy engine; scripts/aks-up.sh refuses
#                                  a cluster without one (P7a's policies
#                                  would be inert). Set it on the make
#                                  command line or export it in the shell;
#                                  it is deliberately NOT in the recipe's
#                                  explicit list below, because that would
#                                  turn "unset" into an explicit empty
#                                  string, which the script refuses.
# See docs/aks.md for why those defaults, and what a run costs.

## aks-cluster: create the resource group, the PRIVATE ACR, and the AKS
## cluster (with AcrPull for its kubelet identity), then write kubeconfig
aks-cluster:
	@AKS_RESOURCE_GROUP='$(AKS_RESOURCE_GROUP)' ACR_NAME='$(ACR_NAME)' \
		AKS_CLUSTER='$(AKS_CLUSTER)' AKS_LOCATION='$(AKS_LOCATION)' \
		AKS_NODE_SIZE='$(AKS_NODE_SIZE)' AKS_NODE_COUNT='$(AKS_NODE_COUNT)' \
		bash scripts/aks-up.sh

## aks-creds: refresh the kubeconfig entry for an existing AKS cluster
aks-creds:
	@test -n "$(AKS_RESOURCE_GROUP)" || \
		{ echo 'usage: make aks-creds AKS_RESOURCE_GROUP=<rg> [AKS_CLUSTER=<name>]' >&2; exit 1; }
	az aks get-credentials --name $(AKS_CLUSTER) \
		--resource-group $(AKS_RESOURCE_GROUP) --overwrite-existing

## aks-down: DELETE the ephemeral resource group (cluster + registry + all).
## Refuses any group not tagged by scripts/aks-up.sh, and requires an
## explicit confirmation naming the group. This is not best-effort: the
## P5b cluster is meant to be gone when the lane ends.
aks-down:
	@AKS_RESOURCE_GROUP='$(AKS_RESOURCE_GROUP)' AKS_CLUSTER='$(AKS_CLUSTER)' \
		bash scripts/aks-down.sh

# Pinned kagent CLI, checksum-verified. The release .sha256 files embed a
# build path, so compare digests directly.
$(KAGENT):
	mkdir -p bin
	curl -sSfLo $(KAGENT) https://github.com/kagent-dev/kagent/releases/download/v$(KAGENT_VERSION)/kagent-$(OS)-$(ARCH)
	curl -sSfLo $(KAGENT).sha256 https://github.com/kagent-dev/kagent/releases/download/v$(KAGENT_VERSION)/kagent-$(OS)-$(ARCH).sha256
	@sum=$$(if [ "$(OS)" = darwin ]; then shasum -a 256 $(KAGENT); else sha256sum $(KAGENT); fi | cut -d' ' -f1); \
	test "$$sum" = "$$(cut -d' ' -f1 $(KAGENT).sha256)" || \
		{ echo 'kagent CLI checksum mismatch' >&2; rm -f $(KAGENT); exit 1; }
	chmod +x $(KAGENT)

## ---- P5a: the governed Slack path (docs/slack.md) ----

## slack-secret: capture the Slack BOT token stdin-only and store the
## plane-side Secrets. REFUSES unless Slack confirms the channel is
## private and the bot is a member (board rule: never a shared channel).
##   make slack-secret SLACK_CHANNEL=C0XXXXXXXXX
slack-secret: guard
	@test -n "$(SLACK_CHANNEL)" || \
		{ echo 'usage: make slack-secret SLACK_CHANNEL=C0XXXXXXXXX (a PRIVATE test channel)' >&2; exit 1; }
	@KUBECTL="$(KUBECTL)" SLACK_CHANNEL="$(SLACK_CHANNEL)" bash scripts/slack-secret.sh

## slack-mcp: deploy the third-party Slack MCP server in-cluster, in the
## PLANE's namespace, via kagent's MCPServer CRD (digest-pinned). This is
## the first pod here with deliberate internet egress — see the runbook.
slack-mcp: guard
	@$(KUBECTL) -n kaimahi get secret kaimahi-slack-bot >/dev/null 2>&1 || \
		{ echo 'kaimahi-slack-bot missing — run: make slack-secret SLACK_CHANNEL=C0XXXXXXXXX' >&2; exit 1; }
	@# Without the gateway's upstream credential the server still starts,
	@# but every relayed call fails closed at 503 — and a tool-grant use is
	@# consumed BEFORE the forward, so a human approval would be spent on a
	@# message that was never sent. Check it here, not after the fact.
	@$(KUBECTL) -n kaimahi get secret kaimahi-slack-mcp-key >/dev/null 2>&1 || \
		{ echo 'kaimahi-slack-mcp-key missing — re-run: make slack-secret SLACK_CHANNEL=C0XXXXXXXXX' >&2; exit 1; }
	$(KUBECTL) apply -f k8s/slack-mcp.yaml
	$(KUBECTL) -n kaimahi wait \
		--for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True \
		mcpserver/kaimahi-slack-mcp --timeout=300s

## govern-slack: put the Slack demo agent behind the MCP gateway — issue
## its kmh_ credential (agent-side Secret kaimahi-slack-token), set the
## READ-ONLY allowlist, apply the Kaimahi RemoteMCPServer and the agent.
## Posting is deliberately absent from the allowlist.
govern-slack: guard
	@KUBECTL="$(KUBECTL)" GOVERNED_SECRET=kaimahi-slack-token \
		bash scripts/plane-admin.sh issue $(CRED_SLACK)
	@KUBECTL="$(KUBECTL)" bash scripts/plane-admin.sh tool-allow $(CRED_SLACK) "$(SLACK_TOOLS)"
	$(KUBECTL) apply -f k8s/kaimahi-slack.yaml
	$(KUBECTL) -n kagent wait \
		--for=jsonpath='{.status.conditions[?(@.type=="Accepted")].status}'=True \
		remotemcpserver/kaimahi-slack --timeout=300s
	$(KUBECTL) apply -f k8s/slack-agent.yaml
	$(KUBECTL) -n kagent patch agent hello-slack --type merge \
		-p '{"spec":{"declarative":{"tools":[{"type":"McpServer","mcpServer":{"apiGroup":"kagent.dev","kind":"RemoteMCPServer","name":"kaimahi-slack","toolNames":[$(SLACK_TOOLNAMES_JSON)]}}]}}}'
	$(KUBECTL) -n kagent wait \
		--for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True \
		agent/hello-slack --timeout=300s

## slack-allow: replace the Slack credential's allowlist, e.g.
##   make slack-allow SLACK_TOOLS=conversations_history
##   make slack-allow SLACK_TOOLS=-        (empty: nothing callable)
## Widening this is a CONFIG change; the demo widens by APPROVAL instead.
slack-allow: guard
	@KUBECTL="$(KUBECTL)" bash scripts/plane-admin.sh tool-allow $(CRED_SLACK) "$(SLACK_TOOLS)"

## slack-audit: the Slack credential's tool-call audit trail
slack-audit:
	@KUBECTL="$(KUBECTL)" bash scripts/plane-admin.sh tool-audit $(CRED_SLACK)

## slack-post: ask the demo agent to post to the channel. Denied until a
## human approves it; that denial is the point.
##   make slack-post SLACK_CHANNEL=C0XXXXXXXXX [MESSAGE='...']
MESSAGE ?= Kaimahi governance demo: this message required a human approval.
# The task text reaches the recipe through the ENVIRONMENT, not through a
# re-quoted make/shell string: a MESSAGE containing an apostrophe would
# otherwise break out of the single quotes and mangle the task (or the
# recipe). The channel gets the same anchored shape check
# scripts/slack-secret.sh applies, so nothing odd reaches the agent.
slack-post: export KAIMAHI_SLACK_TASK = Post this to Slack channel $(SLACK_CHANNEL): $(MESSAGE)
slack-post: $(KAGENT)
	@test -n "$(SLACK_CHANNEL)" || \
		{ echo 'usage: make slack-post SLACK_CHANNEL=C0XXXXXXXXX [MESSAGE=...]' >&2; exit 1; }
	@case "$(SLACK_CHANNEL)" in \
		[CG][A-Z0-9][A-Z0-9][A-Z0-9][A-Z0-9][A-Z0-9][A-Z0-9][A-Z0-9]*) ;; \
		*) echo 'invalid SLACK_CHANNEL (want a channel ID like C0XXXXXXXXX, not a #name)' >&2; exit 1 ;; \
	esac
	@case "$(SLACK_CHANNEL)" in \
		*[!A-Z0-9]*) echo 'invalid SLACK_CHANNEL (want a channel ID like C0XXXXXXXXX, not a #name)' >&2; exit 1 ;; \
	esac
	@$(call kagent_forward,hello-slack,$(KAGENT_INVOKE) --agent hello-slack --task "$$KAIMAHI_SLACK_TASK",$(CHAT_RETRYABLE_SAFE))

## slack-down: remove the P5a demo (agent, gateway seam, MCP server).
## The Secrets are left alone — delete them explicitly to revoke.
slack-down: guard
	-$(KUBECTL) -n kagent delete agent hello-slack
	-$(KUBECTL) -n kagent delete remotemcpserver kaimahi-slack
	-$(KUBECTL) -n kaimahi delete mcpserver kaimahi-slack-mcp

## ---- P7a: the network boundary (docs/egress.md) ----
#
# The policies themselves need no target: k8s/plane/network-policy.yaml
# ships with `make plane` on every environment. What needs a target is
# PROOF — a NetworkPolicy the CNI ignores is indistinguishable from one
# it enforces until something is shown to be blocked.

## netpol-verify: prove the boundary is ENFORCED, not merely present —
## policed pods demonstrably cannot reach ollama / the internet, against
## a control pod that can, plus an exec into the real Postgres pod.
## Creates and deletes a few BestEffort probe pods (~2 minutes). Runs on
## every PR in CI. The script guards its own context (like the tool
## probes), so no `guard` here — one banner, not two.
netpol-verify:
	@KUBECTL="$(KUBECTL)" COPILOT_EGRESS=$(COPILOT_EGRESS) bash scripts/netpol-probe.sh

## egress-copilot: let the proxy (and only the proxy) reach TCP 443 on
## public addresses — the Copilot upstream. `make plane-copilot-secret`
## applies this for you; it is here for the case where the token was
## minted before the plane existed and the policy needs re-applying.
egress-copilot: guard
	$(KUBECTL) apply -f k8s/egress-copilot.yaml

## egress-copilot-off: close the proxy's internet allowance again. The
## Copilot Secret is left alone; governed Copilot calls then fail closed
## (the proxy cannot dial out), which is the point.
egress-copilot-off: guard
	@# Delete by manifest, not by a name typed here: a renamed policy
	@# would otherwise delete nothing, exit 0, and leave the hole open.
	$(KUBECTL) delete -f k8s/egress-copilot.yaml --ignore-not-found

## ---- P7b: inbound connectors (docs/inbound.md) ----
#
# The plane's one ingress: an external event (a webhook) may trigger a
# kagent agent, on the plane's terms. The hooks live in the committed
# upstreams table (k8s/plane/upstreams.yaml); these targets issue the
# hook's identity, store its signing secret, deliver an event, and read
# the trail. Approving a hook rides the P4c targets unchanged:
# `make approvals` / `make approve ID=... USES=... TTL=...`.
HOOK          ?= demo
CRED_INBOUND  ?= inbound-demo
# Where a BEARER hook's token goes (kaimahi namespace: the caller is
# outside the cluster, not an agent). `-` discards the token: the right
# choice for SIGNED hooks, whose credential is an identity, not a bearer.
INBOUND_SECRET ?= -
EVENT         ?= Reply with exactly the word PONG.

## inbound-credential: issue the hook's plane credential, e.g.
##   make inbound-credential                                  (signed demo hook)
##   make inbound-credential CRED_INBOUND=inbound-bearer INBOUND_SECRET=kaimahi-inbound-token
inbound-credential: guard
	@KUBECTL="$(KUBECTL)" SECRET_NAMESPACE=kaimahi GOVERNED_SECRET='$(INBOUND_SECRET)' \
		bash scripts/plane-admin.sh issue $(CRED_INBOUND)

## inbound-secret: store a hook's signing secret — paste the SOURCE's
## secret on stdin, or GENERATE=1 for a fresh one a Kaimahi-scheme caller
## is then told (see scripts/inbound-secret.sh for retrieval).
inbound-secret: guard
	@KUBECTL="$(KUBECTL)" HOOK=$(HOOK) bash scripts/inbound-secret.sh $(if $(GENERATE),--generate,)

## inbound-fire: deliver one event to a hook and report the plane's
## decision. Unguarded for the same reason `chat` is (it runs through
## the guarded probe script, which resolves and vets its own context).
##   make inbound-fire [HOOK=demo] [EVENT='...'] [AUTH=hmac|bearer|none|forged|stale]
##                     [EXPECT=202] [DELIVERY=<id to resend>]
inbound-fire:
	@KUBECTL="$(KUBECTL)" bash scripts/inbound-probe.sh $(HOOK) "$(EVENT)"

## inbound-audit: the inbound event trail (decisions and outcomes, newest
## first) — every hook, unless HOOK=<name> is given on the command line.
## (HOOK's default serves inbound-fire/inbound-secret; an audit that
## silently narrowed to it would hide the other hooks' events.)
inbound-audit:
	@KUBECTL="$(KUBECTL)" bash scripts/plane-admin.sh inbound-audit \
		$(if $(filter command line,$(origin HOOK)),$(HOOK),)

## ---- P8: the public edge (docs/inbound.md, "Putting it on the internet") ----
#
# The ONLY internet-reachable thing in this repo: a TLS edge in front of
# the inbound bridge, on TARGET=aks only. kind has no public path and
# these targets refuse there rather than pretend. KAIMAHI_DNS_LABEL is an
# Azure identifier (it becomes <label>.<region>.cloudapp.azure.com) and is
# never committed; neither is the public IP the scan reports.
ifeq ($(TARGET),aks)
## inbound-expose: put the inbound bridge on the internet — Caddy edge,
## Let's Encrypt via TLS-ALPN-01, one port. Prints the Slack Request URL.
##   TARGET=aks make inbound-expose KAIMAHI_DNS_LABEL=<unique-label>
inbound-expose: guard
	@KUBECTL="$(KUBECTL)" KAIMAHI_DNS_LABEL='$(KAIMAHI_DNS_LABEL)' AKS_LOCATION='$(AKS_LOCATION)' \
		AKS_RESOURCE_GROUP='$(AKS_RESOURCE_GROUP)' AKS_CLUSTER='$(AKS_CLUSTER)' \
		bash scripts/inbound-expose.sh

## inbound-unexpose: take the edge down (Deployment, Service + public IP,
## the certificate's volume, and the policy allowance). REMOVE the Slack
## app's Request URL too — the name this frees can be claimed by anyone.
inbound-unexpose: guard
	$(KUBECTL) delete -f k8s/inbound-edge.yaml --ignore-not-found
	@echo 'edge removed. Now remove the Request URL / disable Event Subscriptions in the Slack app.' >&2

## exposure-scan: prove the internet-facing surface is exactly the edge
## on 443 — every public IP in the cluster's node resource group is
## connect-scanned on all 65535 TCP ports (IPs masked; REVEAL_IPS=1).
exposure-scan:
	@KUBECTL="$(KUBECTL)" AKS_RESOURCE_GROUP='$(AKS_RESOURCE_GROUP)' AKS_CLUSTER='$(AKS_CLUSTER)' \
		bash scripts/exposure-scan.sh
else
inbound-expose inbound-unexpose exposure-scan:
	@echo 'the public edge exists only on TARGET=aks — a kind cluster has no internet-reachable address,' >&2
	@echo 'and the inbound bridge there is reached by port-forward only (docs/inbound.md).' >&2
	@exit 1
endif
