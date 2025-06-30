# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 4.20.999-snapshot

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
CHANNELS ?= alpha

# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
DEFAULT_CHANNEL ?= alpha

# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# Konflux catalog configuration
PACKAGE_NAME_KONFLUX = numaresources-operator
CATALOG_TEMPLATE_KONFLUX = .konflux/catalog/catalog-template.in.yaml
CATALOG_KONFLUX = .konflux/catalog/$(PACKAGE_NAME_KONFLUX)/catalog.yaml

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# openshift-kni.io/numaresources-operator-bundle:$VERSION and openshift-kni.io/numaresources-operator-catalog:$VERSION.
REPO ?= quay.io/openshift-kni
IMAGE_TAG_BASE ?= $(REPO)/numaresources-operator

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:$(VERSION)

# Image URL to use all building/pushing image targets
IMG ?= $(IMAGE_TAG_BASE):$(VERSION)

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_ENGINE defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_ENGINE ?= podman

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
export PATH := $(PATH):$(PWD)/bin
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

KUSTOMIZE_DEPLOY_DIR ?= config/default

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: deps-update
deps-update:
	go mod tidy && go mod vendor

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: generate-source
generate-source: pkg/version/_buildinfo.json

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: generate-source ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet setup-envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test $$(go list ./... | grep -v /e2e) -coverprofile coverage.out

.PHONY: test-unit
test-unit: test-unit-pkgs test-controllers

.PHONY: test-unit-pkgs
test-unit-pkgs: generate-source
	go test $$(go list ./... | grep -vE 'tools|cmd|internal/controller|test/e2e|test/deviceplugin|k8simported')	

.PHONY: test-unit-pkgs-cover
test-unit-pkgs-cover: generate-source
	go test $$(go list ./... | grep -vE 'controller|test|tools|cmd') -coverprofile coverage.out

test-controllers: envtest generate-source
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test ./internal/controller/...

test-e2e: build-e2e-all
	ENABLE_SCHED_TESTS=true hack/run-test-e2e.sh

test-install-e2e: build-e2e-all
	hack/run-test-install-e2e.sh

test-uninstall-e2e: build-e2e-all
	hack/run-test-uninstall-e2e.sh

test-upgrade-e2e: build-e2e-all
	hack/run-test-upgrade-e2e.sh

test-must-gather-e2e: build-must-gather-e2e
	hack/run-test-must-gather-e2e.sh

# intentional left out:
#   api/, because autogeneration
#   cmd/, because kubebuilder scaffolding
.PHONY:
sort-imports:
	@hack/sort-imports.py -v ./test/
	@hack/sort-imports.py -v ./internal/
	@hack/sort-imports.py -v ./pkg/
	@hack/sort-imports.py -v ./rte/
	@hack/sort-imports.py -v ./tools/
	@hack/sort-imports.py -v ./nrovalidate/

.PHONY: lint
lint: update-buildinfo golangci-lint ## Run golangci-lint linter
	$(GOLANGCI_LINT) --verbose run --print-resources-usage --exclude-dirs test/internal/k8simported

.PHONY: lint-fix
lint-fix: update-buildinfo golangci-lint ## Run golangci-lint linter and perform fixes
	$(GOLANGCI_LINT) --verbose run --print-resources-usage --fix

.PHONY: lint-config
lint-config: golangci-lint ## Verify golangci-lint linter configuration
	$(GOLANGCI_LINT) --verbose config verify

.PHONY: gosec
gosec:
	gosec ./pkg/... ./rte/pkg/... ./internal/... ./nrovalidate/validator/...

.PHONY: govulncheck
govulncheck:
	govulncheck -show=verbose ./...

.PHONY: cover-view
cover-view:
	go tool cover -html=coverage.out

.PHONY: cover-summary
cover-summary:
	go tool cover -func=coverage.out

##@ Build

.PHONY: build-installer
build-installer: manifests generate kustomize ## Generate a consolidated YAML with CRDs and deployment.
	mkdir -p dist
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default > dist/install.yaml

.PHONY: binary
binary: build-tools
	LDFLAGS="-s -w"; \
	LDFLAGS+=" -X github.com/openshift-kni/numaresources-operator/pkg/images.tag=$(VERSION)"; \
	go build -mod=vendor -o bin/manager -ldflags "$$LDFLAGS" -tags "$$GOTAGS" cmd/main.go

.PHONY: binary-rte
binary-rte: build-tools
	LDFLAGS="-s -w"; \
	go build -mod=vendor -o bin/exporter -ldflags "$$LDFLAGS" -tags "$$GOTAGS" rte/main.go

.PHONY: binary-nrovalidate
binary-nrovalidate: build-tools
	LDFLAGS="-s -w"; \
	go build -mod=vendor -o bin/nrovalidate -ldflags "$$LDFLAGS" -tags "$$GOTAGS" nrovalidate/main.go

.PHONY: binary-numacell
binary-numacell: build-tools
	LDFLAGS="-s -w" \
	CGO_ENABLED=0 go build -mod=vendor -o bin/numacell -ldflags "$$LDFLAGS" test/deviceplugin/cmd/numacell/main.go

.PHONY: binary-all
binary-all: goversion \
	binary \
	binary-rte \
	binary-nrovalidate \
	introspect-data

.PHONY: binary-e2e-rte-local
binary-e2e-rte-local: generate-source
	go test -c -v -o bin/e2e-nrop-rte-local.test ./test/e2e/rte/local

.PHONY: binary-e2e-rte
binary-e2e-rte: binary-e2e-rte-local generate-source
	go test -c -v -o bin/e2e-nrop-rte.test ./test/e2e/rte

.PHONY: binary-e2e-install
binary-e2e-install: generate-source
	go test -v -c -o bin/e2e-nrop-install.test ./test/e2e/install && go test -v -c -o bin/e2e-nrop-sched-install.test ./test/e2e/sched/install

.PHONY: binary-e2e-upgrade
binary-e2e-upgrade: generate-source
	go test -v -c -o bin/e2e-nrop-upgrade.test ./test/e2e/upgrade

.PHONY: binary-e2e-uninstall
binary-e2e-uninstall: generate-source
	go test -v -c -o bin/e2e-nrop-uninstall.test ./test/e2e/uninstall && go test -v -c -o bin/e2e-nrop-sched-uninstall.test ./test/e2e/sched/uninstall

.PHONY: binary-e2e-sched
binary-e2e-sched: generate-source
	go test -c -v -o bin/e2e-nrop-sched.test ./test/e2e/sched

.PHONY: binary-e2e-serial
binary-e2e-serial: generate-source
	CGO_ENABLED=0 go test -c -v -o bin/e2e-nrop-serial.test -ldflags "$$LDFLAGS" ./test/e2e/serial

.PHONY: binary-e2e-tools
binary-e2e-tools: generate-source
	go test -c -v -o bin/e2e-nrop-tools.test ./test/e2e/tools

.PHONY: binary-e2e-must-gather
binary-e2e-must-gather: generate-source
	go test -c -v -o bin/e2e-nrop-must-gather.test ./test/e2e/must-gather

# backward compatibility
binary-must-gather-e2e: binary-e2e-must-gather

.PHONY: binary-e2e-all
binary-e2e-all: goversion \
	binary-e2e-install \
	binary-e2e-upgrade \
	binary-e2e-rte \
	binary-e2e-sched \
	binary-e2e-uninstall \
	binary-e2e-serial \
	binary-e2e-tools \
	binary-e2e-must-gather \
	runner-e2e-serial \
	build-pause \
	introspect-data

.PHONY: runner-e2e-serial
runner-e2e-serial: bin/envsubst
	hack/render-e2e-runner.sh
	hack/test-e2e-runner.sh

.PHONY: introspect-data
introspect-data: build-topics build-buildinfo

.PHONY: build-topics
build-topics:
	mkdir -p bin && go run tools/lstopics/lstopics.go > bin/topics.json

.PHONY: build-buildinfo
build-buildinfo: bin/buildhelper
	bin/buildhelper inspect > bin/buildinfo.json

.PHONY: build
build: generate generate-source fmt vet binary

.PHONY: build-rte
build-rte: generate-source fmt vet binary-rte

.PHONY: build-numacell
build-numacell: fmt vet binary-numacell

.PHONY: build-nrovalidate
build-nrovalidate: generate-source fmt vet binary-nrovalidate

.PHONY: build-all
build-all: generate generate-source fmt vet binary binary-rte binary-numacell binary-nrovalidate

.PHONY: build-e2e-rte
build-e2e-rte: generate-source fmt vet binary-e2e-rte

.PHONY: build-e2e-install
build-e2e-install: fmt vet binary-e2e-install

.PHONY: build-e2e-upgrade
build-e2e-upgrade: fmt vet binary-e2e-upgrade

.PHONY: build-e2e-uninstall
build-e2e-uninstall: fmt vet binary-e2e-uninstall

.PHONY: build-e2e-all
build-e2e-all: generate-source fmt vet binary-e2e-all

.PHONY: build-e2e-must-gather
build-e2e-must-gather: fmt vet binary-e2e-must-gather

# backward compatibility
build-must-gather-e2e: build-e2e-must-gather

.PHONY: build-pause
build-pause: bin-dir
	install -m 755 hack/pause bin/

.PHONY: build-pause-gcc
build-pause-gcc: bin-dir
	gcc -Wall -g -Os -static -o bin/pause hack/pause.c

.PHONY: bin-dir
bin-dir:
	@mkdir -p bin || :

run: manifests generate generate-source fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

container-build: #test ## Build container image with the manager.
	$(CONTAINER_ENGINE) build -t ${IMG} .

container-push: ## Push container image with the manager.
	$(CONTAINER_ENGINE) push ${IMG}

container-build-pause:
	$(CONTAINER_ENGINE) build -f Dockerfile.pause.gcc -t ${REPO}/pause:test-ci . 

container-push-pause:
	$(CONTAINER_ENGINE) push ${REPO}/pause:test-ci . 

##@ Manifest Bundle

.PHONY: bundle
bundle: operator-sdk manifests kustomize ## Generate bundle manifests and metadata, then validate generated files.
	$(OPERATOR_SDK) generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)
	$(OPERATOR_SDK) bundle validate ./bundle

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	$(CONTAINER_ENGINE) build -f Dockerfile.bundle -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) container-push IMG=$(BUNDLE_IMG)

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
catalog-build: opm ## Build a catalog image.
	$(OPM) index add --container-tool $(CONTAINER_ENGINE) --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) container-push IMG=$(CATALOG_IMG)

##@ Build tools:

.PHONY: goversion
goversion:
	@go version

.PHONY: build-tools
build-tools: goversion bin/buildhelper bin/envsubst bin/lsplatform update-buildinfo

.PHONY: build-tools-all
build-tools-all: goversion bin/buildhelper bin/envsubst bin/lsplatform bin/catkubeletconfmap bin/watchnrtattr bin/mkginkgolabelfilter bin/nrtcacheck update-buildinfo

pkg/version/_buildinfo.json: bin/buildhelper
	@bin/buildhelper inspect > pkg/version/_buildinfo.json

.PHONY: update-buildinfo
update-buildinfo: pkg/version/_buildinfo.json

bin/buildhelper: tools/buildhelper/buildhelper.go
	@go build -o $@ $<

bin/envsubst: tools/envsubst/envsubst.go
	@go build -o $@ $<

bin/lsplatform: tools/lsplatform/lsplatform.go
	@go build -o $@ $<

bin/catkubeletconfmap: tools/catkubeletconfmap/catkubeletconfmap.go
	LDFLAGS="-static"
	CGO_ENABLED=0 go build -o $@ -ldflags "$$LDFLAGS" $<

bin/watchnrtattr: tools/watchnrtattr/watchnrtattr.go
	@go build -o $@ $<

bin/mkginkgolabelfilter: tools/mkginkgolabelfilter/mkginkgolabelfilter.go
	LDFLAGS="-static"
	@go build -o $@ -ldflags "$$LDFLAGS" $<

bin/nrtcacheck: build-tools tools/nrtcacheck/nrtcacheck.go
	LDFLAGS="-s -w" \
	go build -mod=vendor -o $@ -ldflags "$$LDFLAGS" -tags "$$GOTAGS" $<

verify-generated: bundle generate
	@echo "Verifying that all code is committed after updating deps and formatting and generating code"
	hack/verify-generated.sh

install-git-hooks:
	git config core.hooksPath .githooks	

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy-mco-crds
deploy-mco-crds: build-tools
	@echo "Verifying that the MCO CRDs are present in the cluster"
	hack/deploy-mco-crds.sh

.PHONY: deploy
deploy: manifests kustomize deploy-mco-crds ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | $(KUBECTL) apply -f -

.PHONY: undeploy
undeploy: kustomize ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries - download binary

GOOS=$(shell go env GOOS)
GOARCH=$(shell go env GOARCH)

OPERATOR_SDK_VERSION ?= 1.36.1
OPERATOR_SDK_BIN = "operator-sdk_$(GOOS)_$(GOARCH)"
OPERATOR_SDK = "$(LOCALBIN)/$(OPERATOR_SDK_BIN)"

OPM_VERSION ?= 1.52.0
OPM_BIN = "$(GOOS)-$(GOARCH)-opm"
OPM = "$(LOCALBIN)/$(OPM_BIN)"

GOLANGCI_LINT_VERSION ?= 1.64.6
GOLANGCI_LINT_NAME = golangci-lint-$(GOLANGCI_LINT_VERSION)-$(GOOS)-$(GOARCH)
GOLANGCI_LINT_ARTIFACT_FILE = $(GOLANGCI_LINT_NAME).tar.gz
GOLANGCI_LINT_EXEC_NAME = golangci-lint
GOLANGCI_LINT = $(LOCALBIN)/$(GOLANGCI_LINT_EXEC_NAME)

.PHONY: operator-sdk
operator-sdk:
	@if [ ! -x "$(OPERATOR_SDK)" ]; then\
		echo "Downloading operator-sdk $(OPERATOR_SDK_VERSION)";\
		mkdir -p $(LOCALBIN);\
		curl -JL https://github.com/operator-framework/operator-sdk/releases/download/v$(OPERATOR_SDK_VERSION)/$(OPERATOR_SDK_BIN) -o $(OPERATOR_SDK);\
		chmod +x $(OPERATOR_SDK);\
	else\
		echo "Using operator-sdk cached at $(OPERATOR_SDK)";\
	fi

.PHONY: opm
opm: ## Download opm locally if necessary.
	@if [ ! -x "$(OPM)" ]; then\
		echo "Downloading opm $(OPM_VERSION)";\
		mkdir -p $(LOCALBIN);\
		curl -JL https://github.com/operator-framework/operator-registry/releases/download/v$(OPM_VERSION)/$(OPM_BIN) -o $(OPM);\
		chmod +x $(OPM);\
	else\
		echo "Using OPM cached at $(OPM_BIN)";\
	fi

.PHONY: golangci-lint
golangci-lint: ## Download golangci-lint locally if necessary.
	@if [ ! -x "$(GOLANGCI_LINT)" ]; then\
		echo "Downloading golangci-lint from https://github.com/golangci/golangci-lint/releases/download/v$(GOLANGCI_LINT_VERSION)/$(GOLANGCI_LINT_ARTIFACT_FILE)";\
		mkdir -p $(LOCALBIN);\
		curl -JL https://github.com/golangci/golangci-lint/releases/download/v$(GOLANGCI_LINT_VERSION)/$(GOLANGCI_LINT_ARTIFACT_FILE) -o $(LOCALBIN)/$(GOLANGCI_LINT_ARTIFACT_FILE);\
		pushd $(LOCALBIN);\
		tar --no-same-owner -xzf $(GOLANGCI_LINT_ARTIFACT_FILE)  --strip-components=1 $(GOLANGCI_LINT_NAME)/$(GOLANGCI_LINT_EXEC_NAME);\
		chmod +x $(GOLANGCI_LINT_EXEC_NAME);\
		rm -f $(GOLANGCI_LINT_ARTIFACT_FILE);\
		popd;\
	else\
		echo "Using golangci-lint cached at $(GOLANGCI_LINT), current version $(GOLANGCI_LINT_LOCAL_VERSION) expected version: $(GOLANGCI_LINT_VERSION)";\
	fi

## Tool Binaries - go-install binary

KUBECTL ?= kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest

## Tool Versions
KUSTOMIZE_VERSION ?= v5.5.0
CONTROLLER_TOOLS_VERSION ?= v0.17.3
#ENVTEST_VERSION is the version of controller-runtime release branch to fetch the envtest setup script (i.e. release-0.20)
ENVTEST_VERSION ?= $(shell go list -m -f "{{ .Version }}" sigs.k8s.io/controller-runtime | awk -F'[v.]' '{printf "release-%d.%d", $$2, $$3}')
#ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries (i.e. 1.31)
ENVTEST_K8S_VERSION ?= $(shell go list -m -f "{{ .Version }}" k8s.io/api | awk -F'[v.]' '{printf "1.%d", $$3}')

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: setup-envtest
setup-envtest: envtest ## Download the binaries required for ENVTEST in the local bin directory.
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@$(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef

##@ Konflux

.PHONY: yq
YQ ?= ./bin/yq
yq: ## download yq if not in the path
ifeq (,$(wildcard $(YQ)))
ifeq (,$(shell which yq 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(YQ)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(YQ) https://github.com/mikefarah/yq/releases/download/v4.45.4/yq_$${OS}_$${ARCH} ;\
	chmod +x $(YQ) ;\
	}
else
YQ = $(shell which yq)
endif
endif

.PHONY: konflux-update-task-refs ## update task images
konflux-update-task-refs: yq
	hack/konflux-update-task-refs.sh .tekton/build-pipeline.yaml
	hack/konflux-update-task-refs.sh .tekton/fbc-pipeline.yaml

.PHONY: konflux-validate-catalog-template-bundle ## validate the last bundle entry on the catalog template file
konflux-validate-catalog-template-bundle: yq operator-sdk
	@{ \
	set -e ;\
	bundle=$(shell $(YQ) ".entries[-1].image" $(CATALOG_TEMPLATE_KONFLUX)) ;\
	echo "validating the last bundle entry: $${bundle} on catalog template: $(CATALOG_TEMPLATE_KONFLUX)" ;\
	$(OPERATOR_SDK) bundle validate $${bundle} ;\
	}

.PHONY: konflux-validate-catalog
konflux-validate-catalog: opm ## validate the current catalog file
	@echo "validating catalog: .konflux/catalog/$(PACKAGE_NAME_KONFLUX)"
	$(OPM) validate .konflux/catalog/$(PACKAGE_NAME_KONFLUX)/

.PHONY: konflux-generate-catalog ## generate a quay.io catalog
konflux-generate-catalog: yq opm
	hack/konflux-update-catalog-template.sh --set-catalog-template-file $(CATALOG_TEMPLATE_KONFLUX) --set-bundle-builds-file .konflux/catalog/bundle.builds.in.yaml	
	$(OPM) alpha render-template basic --output yaml --migrate-level bundle-object-to-csv-metadata $(CATALOG_TEMPLATE_KONFLUX) > $(CATALOG_KONFLUX)
	$(OPM) validate .konflux/catalog/$(PACKAGE_NAME_KONFLUX)/

.PHONY: konflux-generate-catalog-production ## generate a registry.redhat.io catalog
konflux-generate-catalog-production: konflux-generate-catalog
        # overlay the bundle image for production
	sed -i 's|quay.io/redhat-user-workloads/telco-5g-tenant/$(PACKAGE_NAME_KONFLUX)-bundle-4-20|registry.redhat.io/openshift4/$(PACKAGE_NAME_KONFLUX)-bundle|g' $(CATALOG_KONFLUX)
        # From now on, all the related images must reference production (registry.redhat.io) exclusively
	./hack/konflux-validate-related-images-production.sh --set-catalog-file $(CATALOG_KONFLUX)
	$(OPM) validate .konflux/catalog/$(PACKAGE_NAME_KONFLUX)/
