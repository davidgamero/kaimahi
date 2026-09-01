# Thin glue over kind/AKS + helm + kubectl + the kagent CLI. No Kaimahi CLI
# here — kagent already ships one (see docs/P1-RUNBOOK.md for the full story).
#
# TARGET selects the environment (P5b). kind is the default and its
# behaviour is unchanged: every kind command, context, and manifest is
# exactly what it was before this file learned about anything else.
#
#   make up                      # kind, as always
#   TARGET=aks make ...          # a managed cluster (docs/P5B-RUNBOOK.md)
#
# KUBE_CTX is now overridable, which is the whole point of the lane — and
# also its one new hazard, since `make down` can suddenly name a cluster
# somebody cares about. Every MUTATING target below therefore depends on
# `guard` (scripts/kube-guard.sh): it prints where the action is going,
# and demands explicit confirmation for anything that is not a local kind
# cluster. Fail closed — no confirmation, no action.
TARGET         ?= kind

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
PLANE_IMAGE_TAG  ?= p5b

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
	slack-post slack-down aks-cluster aks-creds aks-down

# guard: the context-safety net every MUTATING target depends on. Prints
# the target context/namespaces; demands explicit confirmation for
# anything that is not a local kind cluster; fails closed. Read-only
# targets (chat, status, ledger, audits, approvals lists) deliberately do
# NOT depend on it — they cannot change a cluster, and adding a prompt to
# them would be noise. Make runs it once per invocation, so a single
# `make up` asks at most once.
guard:
	@KUBE_CTX='$(KUBE_CTX)' KUBE_NS='$(GUARD_NS)' \
		bash scripts/kube-guard.sh '$(MAKECMDGOALS) [TARGET=$(TARGET)]'

GUARD_NS ?= kagent, kaimahi, ollama

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
up: guard $(UP_STEPS)

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
ollama: guard
ifneq ($(TARGET),kind)
	@echo 'ollama is not deployed on TARGET=$(TARGET) — the managed path is' >&2
	@echo 'Copilot-only (D15). See docs/P5B-RUNBOOK.md.' >&2
	@exit 1
endif
	$(KUBECTL) apply -f k8s/ollama.yaml
	$(KUBECTL) -n ollama rollout status deploy/ollama --timeout=300s

model: guard
ifneq ($(TARGET),kind)
	@echo 'no Ollama on TARGET=$(TARGET) — nothing to pull (D15).' >&2
	@exit 1
endif
	$(KUBECTL) -n ollama exec deploy/ollama -- ollama pull $(MODEL)

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
	@$(KUBECTL) -n kagent port-forward svc/kagent-controller 8083:8083 >/dev/null 2>&1 & \
	pf=$$!; trap 'kill $$pf 2>/dev/null' EXIT; sleep 3; \
	$(KAGENT) invoke --agent $(AGENT) --task "$(TASK)"

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

## use: switch the hello-world agent to a model preset from k8s/models/
# (e.g. make use PRESET=anthropic). Hosted presets need their Secret first
# (make model-secret) — and remember: P2 spend is ungoverned until P4.
use: guard
	@test -n "$(PRESET)" || { echo 'usage: make use PRESET=<name from k8s/models/>' >&2; exit 1; }
	$(KUBECTL) apply -f k8s/models/$(PRESET).yaml
	$(KUBECTL) -n kagent patch agent hello-world --type merge \
		-p '{"spec":{"declarative":{"modelConfig":"$(PRESET)"}}}'
	$(KUBECTL) -n kagent rollout status deploy/hello-world --timeout=180s
	$(KUBECTL) -n kagent wait \
		--for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True \
		agent/hello-world --timeout=300s

## use-ollama: switch back to the keyless in-cluster model
# The confirmation is passed down deliberately: reaching this line means
# the guard above already asked about THIS context and was answered, so
# the sub-make must not ask a second time for the same action.
use-ollama: guard
	$(MAKE) use PRESET=ollama KAIMAHI_CONFIRM='$(KUBE_CTX)'

## ---- P4a: the governance plane (docs/P4A-RUNBOOK.md) ----

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
		{ echo 'ACR_NAME is required for TARGET=aks (see docs/P5B-RUNBOOK.md)' >&2; exit 1; }
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
	@if $(KUBECTL) -n kagent get agent hello-world >/dev/null 2>&1; then \
		$(MAKE) use PRESET=$(GOVERNED_PRESET) KAIMAHI_CONFIRM='$(KUBE_CTX)'; \
	else \
		echo "NOTE: agent hello-world does not exist yet — it will be created on '$(AGENT_MODELCONFIG)' by 'make agent'" >&2; \
	fi

## budget: set monthly caps for a credential, e.g.
##   make budget CAP_CENTS=100 CAP_TOKENS=-     (- or empty = no cap)
budget: guard
	@KUBECTL="$(KUBECTL)" bash scripts/plane-admin.sh budget $(CRED) \
		"$(if $(CAP_CENTS),$(CAP_CENTS),-)" "$(if $(CAP_TOKENS),$(CAP_TOKENS),-)"

## ledger: show the spend ledger (newest first) + month-to-date totals
ledger:
	@KUBECTL="$(KUBECTL)" bash scripts/plane-admin.sh ledger $(CRED)

## ---- P4b: the enforcing MCP gateway (docs/P4B-RUNBOOK.md) ----

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
	$(KUBECTL) -n kagent rollout status deploy/hello-tools --timeout=180s
	$(KUBECTL) -n kagent wait \
		--for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True \
		agent/hello-tools --timeout=300s

## ungovern-tools: restore the P3 wiring (direct to the chart-managed
## tool server, ungoverned) by re-applying the committed Agent YAML
ungovern-tools: guard
	$(KUBECTL) apply -f k8s/tools-agent.yaml
	$(KUBECTL) -n kagent rollout status deploy/hello-tools --timeout=180s

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

## ---- P4c: approvals and time-boxed permits (docs/P4C-RUNBOOK.md) ----

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

status:
	$(KUBECTL) -n kagent get agents,modelconfigs
	$(KUBECTL) -n kagent get pods

ifeq ($(TARGET),kind)
## down: delete the local kind cluster
down: guard
	kind delete cluster --name $(KIND_CLUSTER)
else
## down (TARGET=aks): delete the whole ephemeral resource group
down: aks-down
endif

## ---- P5b: the managed-cluster path (docs/P5B-RUNBOOK.md) ----
#
# Azure identifiers are supplied by the operator and never committed:
#   AKS_RESOURCE_GROUP  required   the group these scripts create/delete
#   ACR_NAME            required   globally-unique private registry name
#   AKS_CLUSTER         optional   cluster + kube-context (default kaimahi)
#   AKS_LOCATION        optional   default westus3
#   AKS_NODE_SIZE       optional   default Standard_B4ms
# See docs/P5B-RUNBOOK.md for why those defaults, and what a run costs.

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

## ---- P5a: the governed Slack path (docs/P5A-RUNBOOK.md) ----

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
	@$(KUBECTL) -n kagent port-forward svc/kagent-controller 8083:8083 >/dev/null 2>&1 & \
	pf=$$!; trap 'kill $$pf 2>/dev/null' EXIT; sleep 3; \
	$(KAGENT) invoke --agent hello-slack --task "$$KAIMAHI_SLACK_TASK"

## slack-down: remove the P5a demo (agent, gateway seam, MCP server).
## The Secrets are left alone — delete them explicitly to revoke.
slack-down: guard
	-$(KUBECTL) -n kagent delete agent hello-slack
	-$(KUBECTL) -n kagent delete remotemcpserver kaimahi-slack
	-$(KUBECTL) -n kaimahi delete mcpserver kaimahi-slack-mcp
