# Kubernetes version of the envtest control plane used by the conformance
# suite: the minor of the k8s.io/api module in go.mod (v0.36.2 -> 1.36.x).
ENVTEST_K8S_VERSION ?= $(shell go list -m -f '{{.Version}}' k8s.io/api | sed -E 's/^v0\.([0-9]+)\..*/1.\1.x/')
# renovate: datasource=go depName=sigs.k8s.io/controller-runtime/tools/setup-envtest
SETUP_ENVTEST_VERSION ?= v0.25.2
# renovate: datasource=go depName=github.com/golangci/golangci-lint/v2
GOLANGCI_LINT_VERSION ?= v2.15.0

# renovate: datasource=docker depName=helmunittest/helm-unittest
HELM_UNITTEST_VERSION ?= 4.3.0-1.2.1
HELM_UNITTEST_IMAGE := helmunittest/helm-unittest:$(HELM_UNITTEST_VERSION)

IMAGE ?= ghcr.io/alessandrozanatta/cert-manager-porkbun-webhook:dev
CHART := deploy/cert-manager-porkbun-webhook
BIN := $(CURDIR)/_bin

ENVTEST_ASSETS = $(shell go run sigs.k8s.io/controller-runtime/tools/setup-envtest@$(SETUP_ENVTEST_VERSION) use -p path --bin-dir $(BIN)/envtest $(ENVTEST_K8S_VERSION))

.PHONY: all
all: lint test chart-test

.PHONY: test
test: ## Unit tests and the offline cert-manager conformance suite.
	@assets="$(ENVTEST_ASSETS)"; \
	TEST_ASSET_ETCD="$$assets/etcd" \
	TEST_ASSET_KUBE_APISERVER="$$assets/kube-apiserver" \
	TEST_ASSET_KUBECTL="$$assets/kubectl" \
	go test -race -count=1 -tags conformance ./...

.PHONY: unit
unit: ## Unit tests only; needs no envtest binaries.
	go test -race -count=1 ./...

.PHONY: lint
lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

.PHONY: chart-test
chart-test: ## Lint, unit-test and schema-validate the chart (needs helm, kubeconform and docker).
	@for values in $(CHART)/ci/*.yaml; do \
		helm lint --strict -f $$values $(CHART) || exit 1; \
		helm template porkbun $(CHART) -n cert-manager -f $$values | \
			kubeconform -strict -summary \
				-schema-location default \
				-schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
			|| exit 1; \
	done
	docker run --rm -u $$(id -u):$$(id -g) -v $(CURDIR):/apps -w /apps $(HELM_UNITTEST_IMAGE) $(CHART)

.PHONY: image
image:
	docker build -t $(IMAGE) .

.PHONY: e2e
e2e: ## Deploy into a throwaway kind cluster and smoke-test (needs docker, kind, helm, kubectl).
	hack/e2e.sh

.PHONY: clean
clean:
	chmod -R u+w $(BIN) 2>/dev/null || true
	rm -rf $(BIN) _out
