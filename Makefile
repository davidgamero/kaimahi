# Thin glue over kind + helm + kubectl + the kagent CLI. No Tomte CLI here —
# kagent already ships one (see docs/P1-RUNBOOK.md for the full story).

KIND_CLUSTER   ?= tomte-p1
KUBE_CTX       := kind-$(KIND_CLUSTER)
KAGENT_VERSION ?= 0.9.12
MODEL          ?= qwen2.5:3b
TASK           ?= Hello! Who are you and where are you running?
KAGENT         ?= bin/kagent
KUBECTL        := kubectl --context $(KUBE_CTX)

OS   := $(shell uname -s | tr A-Z a-z)
ARCH := $(shell uname -m | sed -e s/x86_64/amd64/ -e s/aarch64/arm64/)

.PHONY: up cluster ollama model kagent agent chat down status \
	model-secret copilot-secret use use-ollama

## up: everything from an empty machine to a ready agent
up: cluster ollama model kagent agent status

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

agent:
	$(KUBECTL) apply -f k8s/hello-world.yaml
	$(KUBECTL) -n kagent wait \
		--for=jsonpath='{.status.conditions[?(@.type=="Ready")].status}'=True \
		agent/hello-world --timeout=300s

## chat: one question to the agent via the kagent CLI (override with TASK=...)
chat: $(KAGENT)
	@$(KUBECTL) -n kagent port-forward svc/kagent-controller 8083:8083 >/dev/null 2>&1 & \
	pf=$$!; trap 'kill $$pf 2>/dev/null' EXIT; sleep 3; \
	$(KAGENT) invoke --agent hello-world --task "$(TASK)"

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
