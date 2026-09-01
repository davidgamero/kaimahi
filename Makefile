# Thin glue over kind + helm + kubectl + the kagent CLI. No Kaimahi CLI here —
# kagent already ships one (see docs/P1-RUNBOOK.md for the full story).

KIND_CLUSTER   ?= kaimahi-p1
KUBE_CTX       := kind-$(KIND_CLUSTER)
KAGENT_VERSION ?= 0.9.12
MODEL          ?= qwen2.5:3b
AGENT          ?= hello-world
TASK           ?= Hello! Who are you and where are you running?
KAGENT         ?= bin/kagent
KUBECTL        := kubectl --context $(KUBE_CTX)

OS   := $(shell uname -s | tr A-Z a-z)
ARCH := $(shell uname -m | sed -e s/x86_64/amd64/ -e s/aarch64/arm64/)

PLANE_IMAGE    ?= kaimahi-proxy:p4c
CRED           ?= hello-world
CRED_TOOLS     ?= hello-tools
TOOLS          ?= k8s_get_resources
# TOOLS as a JSON string array for the Agent patch, so the agent's
# toolNames stay aligned with the gateway allowlist ("-" -> empty).
comma          := ,
TOOLNAMES_JSON  = $(if $(filter -,$(TOOLS)),,"$(subst $(comma),"$(comma)",$(TOOLS))")

.PHONY: up cluster ollama model kagent agent tools-agent chat down status \
	model-secret copilot-secret use use-ollama \
	plane plane-image plane-secrets govern budget ledger plane-copilot-secret \
	govern-tools ungovern-tools tool-allow tool-allowlist tool-audit \
	approvals approve deny request grants approval-audit

## up: everything from an empty machine to ready agents (hello-world + tools)
up: cluster ollama model kagent agent tools-agent status

cluster:
	kind get clusters 2>/dev/null | grep -qx '$(KIND_CLUSTER)' || \
		kind create cluster --name $(KIND_CLUSTER)

ollama:
	$(KUBECTL) apply -f k8s/ollama.yaml
	$(KUBECTL) -n ollama rollout status deploy/ollama --timeout=300s

model:
	$(KUBECTL) -n ollama exec deploy/ollama -- ollama pull $(MODEL)

kagent:
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
agent:
	@current=$$($(KUBECTL) -n kagent get agent hello-world \
		-o jsonpath='{.spec.declarative.modelConfig}' 2>/dev/null || true); \
	$(KUBECTL) apply -f k8s/hello-world.yaml && \
	if [ -n "$$current" ] && [ "$$current" != hello-world-model ]; then \
		echo "NOTE: hello-world was on modelConfig '$$current' — preserving it ('make use PRESET=ollama' resets)" >&2; \
		$(KUBECTL) -n kagent patch agent hello-world --type merge \
			-p "{\"spec\":{\"declarative\":{\"modelConfig\":\"$$current\"}}}"; \
	fi
	$(KUBECTL) -n kagent wait \
		--for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True \
		agent/hello-world --timeout=300s

## tools-agent: the P3 tools-enabled agent (kagent-tools MCP server comes
## from the kagent helm install; this applies the Agent wired to it)
tools-agent:
	$(KUBECTL) -n kagent wait \
		--for=jsonpath='{.status.conditions[?(@.type=="Accepted")].status}'=True \
		remotemcpserver/kagent-tool-server --timeout=300s
	@server=$$($(KUBECTL) -n kagent get agent hello-tools \
		-o jsonpath='{.spec.declarative.tools[0].mcpServer.name}' 2>/dev/null || true); \
	tools=$$($(KUBECTL) -n kagent get agent hello-tools -o json 2>/dev/null \
		| python3 -c 'import json,sys; print(json.dumps(json.load(sys.stdin)["spec"]["declarative"]["tools"]))' \
		2>/dev/null || true); \
	$(KUBECTL) apply -f k8s/tools-agent.yaml && \
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
model-secret:
	@test -n "$(NAME)" || { echo 'usage: make model-secret NAME=<preset>-api-key' >&2; exit 1; }
	@echo 'Paste the API key, press Enter, then Ctrl-D:' >&2
	@tr -d '\n' | $(KUBECTL) -n kagent create secret generic $(NAME) \
		--from-file=api-key=/dev/stdin

## copilot-secret: GitHub device login (cached), then mint a short-lived
## Copilot API token and store it as the github-copilot-token Secret.
## Fail-closed, token bytes only in pipes/0600 files — see the script.
copilot-secret:
	@KUBECTL="$(KUBECTL)" bash scripts/copilot-secret.sh

## use: switch the hello-world agent to a model preset from k8s/models/
# (e.g. make use PRESET=anthropic). Hosted presets need their Secret first
# (make model-secret) — and remember: P2 spend is ungoverned until P4.
use:
	@test -n "$(PRESET)" || { echo 'usage: make use PRESET=<name from k8s/models/>' >&2; exit 1; }
	$(KUBECTL) apply -f k8s/models/$(PRESET).yaml
	$(KUBECTL) -n kagent patch agent hello-world --type merge \
		-p '{"spec":{"declarative":{"modelConfig":"$(PRESET)"}}}'
	$(KUBECTL) -n kagent rollout status deploy/hello-world --timeout=180s
	$(KUBECTL) -n kagent wait \
		--for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True \
		agent/hello-world --timeout=300s

## use-ollama: switch back to the keyless in-cluster model
use-ollama:
	$(MAKE) use PRESET=ollama

## ---- P4a: the governance plane (docs/P4A-RUNBOOK.md) ----

## plane: build + deploy the Kaimahi proxy and its Postgres ledger
plane: plane-image plane-secrets
	$(KUBECTL) apply -f k8s/plane/
	$(KUBECTL) -n kaimahi rollout status deploy/kaimahi-postgres --timeout=300s
	$(KUBECTL) -n kaimahi rollout status deploy/kaimahi-proxy --timeout=300s

plane-image:
	docker build -t $(PLANE_IMAGE) plane/
	kind load docker-image $(PLANE_IMAGE) --name $(KIND_CLUSTER)

plane-secrets:
	@KUBECTL="$(KUBECTL)" bash scripts/plane-secrets.sh

## govern: issue the Kaimahi credential (opaque token -> agent-side
## Secret), apply the governed presets, switch hello-world through the
## proxy. The agent never sees a real upstream key.
govern:
	@KUBECTL="$(KUBECTL)" bash scripts/plane-admin.sh issue $(CRED)
	$(MAKE) use PRESET=governed-ollama
	$(KUBECTL) apply -f k8s/models/governed-copilot.yaml

## budget: set monthly caps for a credential, e.g.
##   make budget CAP_CENTS=100 CAP_TOKENS=-     (- or empty = no cap)
budget:
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
govern-tools:
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
ungovern-tools:
	$(KUBECTL) apply -f k8s/tools-agent.yaml
	$(KUBECTL) -n kagent rollout status deploy/hello-tools --timeout=180s

## tool-allow: replace the tools credential's allowlist, e.g.
##   make tool-allow TOOLS=k8s_get_resources,k8s_get_events
##   make tool-allow TOOLS=-        (empty: nothing callable)
tool-allow:
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
approve:
	@test -n "$(ID)" || { echo 'usage: make approve ID=<uuid> [TTL=60s] [USES=1] [AMOUNT=n]' >&2; exit 1; }
	@KUBECTL="$(KUBECTL)" bash scripts/plane-admin.sh approve "$(ID)" \
		"$(if $(TTL),$(TTL),-)" "$(if $(USES),$(USES),-)" "$(if $(AMOUNT),$(AMOUNT),-)"

## deny: deny a pending request
deny:
	@test -n "$(ID)" || { echo 'usage: make deny ID=<uuid>' >&2; exit 1; }
	@KUBECTL="$(KUBECTL)" bash scripts/plane-admin.sh deny "$(ID)"

## request: file an approval request explicitly, e.g.
##   make request KIND=tool SUBJECT=k8s_get_events
##   make request KIND=budget SUBJECT=tokens CRED=hello-world
request:
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
plane-copilot-secret:
	@KUBECTL="$(KUBECTL)" COPILOT_SECRET_NAMESPACE=kaimahi \
		COPILOT_SECRET_NAME=kaimahi-copilot-token \
		bash scripts/copilot-secret.sh

status:
	$(KUBECTL) -n kagent get agents,modelconfigs
	$(KUBECTL) -n kagent get pods

down:
	kind delete cluster --name $(KIND_CLUSTER)

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
