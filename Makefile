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

.PHONY: up cluster ollama model kagent agent chat down status

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
