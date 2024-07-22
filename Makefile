# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 4.17.999-snapshot

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
CRD_OPTIONS ?= "crd"
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.29

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

GOOS=$(shell go env GOOS)
GOARCH=$(shell go env GOARCH)

CONTAINER_ENGINE ?= docker

BIN_DIR="bin"

OPERATOR_SDK_VERSION="v1.25.0"
OPERATOR_SDK_BIN="operator-sdk_$(GOOS)_$(GOARCH)"
OPERATOR_SDK="$(BIN_DIR)/$(OPERATOR_SDK_BIN)"

OPM_VERSION="v1.15.1"
OPM_BIN="$(GOOS)-$(GOARCH)-opm"
OPM="$(BIN_DIR)/$(OPM_BIN)"

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

KUSTOMIZE_DEPLOY_DIR ?= config/default

# golangci-lint variables
GOLANGCI_LINT_VERSION=1.54.2
GOLANGCI_LINT_NAME=golangci-lint-$(GOLANGCI_LINT_VERSION)-$(GOOS)-$(GOARCH)
GOLANGCI_LINT_ARTIFACT_FILE=$(GOLANGCI_LINT_NAME).tar.gz
GOLANGCI_LINT_EXEC_NAME=golangci-lint
GOLANGCI_LINT=$(BIN_DIR)/$(GOLANGCI_LINT_EXEC_NAME)

all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

fmt: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...

test: manifests generate fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test ./... -coverprofile cover.out

test-unit: test-unit-pkgs test-controllers

test-unit-pkgs:
	go test ./api/... ./pkg/... ./rte/pkg/... ./internal/...

test-controllers: envtest
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test ./controllers/...

test-e2e: build-e2e-all
	ENABLE_SCHED_TESTS=true hack/run-test-e2e.sh

test-install-e2e: build-e2e-all
	hack/run-test-install-e2e.sh

test-must-gather-e2e: build-must-gather-e2e
	hack/run-test-must-gather-e2e.sh

##@ Build

binary: build-tools
	LDFLAGS="-s -w"; \
	LDFLAGS+=" -X github.com/openshift-kni/numaresources-operator/pkg/version.gitcommit=$(shell bin/buildhelper commit)"; \
	LDFLAGS+=" -X github.com/openshift-kni/numaresources-operator/pkg/version.version=$(shell bin/buildhelper version)"; \
	LDFLAGS+=" -X github.com/openshift-kni/numaresources-operator/pkg/images.tag=$(VERSION)"; \
	go build -mod=vendor -o bin/manager -ldflags "$$LDFLAGS" main.go

binary-rte: build-tools
	LDFLAGS="-s -w"; \
	LDFLAGS+=" -X github.com/openshift-kni/numaresources-operator/pkg/version.gitcommit=$(shell bin/buildhelper commit)"; \
	LDFLAGS+=" -X github.com/openshift-kni/numaresources-operator/pkg/version.version=$(shell bin/buildhelper version)"; \
	LDFLAGS+=" -X github.com/openshift-kni/numaresources-operator/pkg/images.tag=$(VERSION)"; \
	go build -mod=vendor -o bin/exporter -ldflags "$$LDFLAGS" rte/main.go

binary-nrovalidate: build-tools
	LDFLAGS="-s -w"; \
	LDFLAGS+=" -X github.com/openshift-kni/numaresources-operator/pkg/version.version=$(shell bin/buildhelper version)"; \
	go build -mod=vendor -o bin/nrovalidate -ldflags "$$LDFLAGS" cmd/nrovalidate/main.go

binary-nrtcacheck: build-tools
	LDFLAGS="-s -w" \
	LDFLAGS+=" -X github.com/openshift-kni/numaresources-operator/pkg/version.version=$(shell bin/buildhelper version)"; \
	go build -mod=vendor -o bin/nrtcacheck -ldflags "$$LDFLAGS" cmd/nrtcacheck/main.go

binary-numacell: build-tools
	LDFLAGS="-s -w" \
	CGO_ENABLED=0 go build -mod=vendor -o bin/numacell -ldflags "$$LDFLAGS" test/deviceplugin/cmd/numacell/main.go

binary-all: binary binary-rte binary-nrovalidate binary-nrtcacheck

binary-e2e-rte-local:
	go test -c -v -o bin/e2e-nrop-rte-local.test ./test/e2e/rte/local

binary-e2e-rte: binary-e2e-rte-local
	go test -c -v -o bin/e2e-nrop-rte.test ./test/e2e/rte

binary-e2e-install:
	go test -v -c -o bin/e2e-nrop-install.test ./test/e2e/install && go test -v -c -o bin/e2e-nrop-sched-install.test ./test/e2e/sched/install

binary-e2e-uninstall:
	go test -v -c -o bin/e2e-nrop-uninstall.test ./test/e2e/uninstall && go test -v -c -o bin/e2e-nrop-sched-uninstall.test ./test/e2e/sched/uninstall

binary-e2e-sched:
	go test -c -v -o bin/e2e-nrop-sched.test ./test/e2e/sched

binary-e2e-serial:
	LDFLAGS+="-X github.com/openshift-kni/numaresources-operator/pkg/version.version=$(shell bin/buildhelper version) "; \
	LDFLAGS+="-X github.com/openshift-kni/numaresources-operator/pkg/version.gitcommit=$(shell bin/buildhelper commit)"; \
	CGO_ENABLED=0 go test -c -v -o bin/e2e-nrop-serial.test -ldflags "$$LDFLAGS" ./test/e2e/serial

binary-e2e-tools:
	go test -c -v -o bin/e2e-nrop-tools.test ./test/e2e/tools

binary-e2e-must-gather:
	go test -c -v -o bin/e2e-nrop-must-gather.test ./test/e2e/must-gather

# backward compatibility
binary-must-gather-e2e: binary-e2e-must-gather

binary-e2e-all: goversion \
	binary-e2e-install \
	binary-e2e-rte \
	binary-e2e-sched \
	binary-e2e-uninstall \
	binary-e2e-serial \
	binary-e2e-tools \
	binary-e2e-must-gather \
	runner-e2e-serial \
	build-pause

runner-e2e-serial: bin/envsubst
	hack/render-e2e-runner.sh
	hack/test-e2e-runner.sh

build: generate fmt vet binary

build-rte: fmt vet binary-rte

build-numacell: fmt vet binary-numacell

build-nrovalidate: fmt vet binary-nrovalidate

build-all: generate fmt vet binary binary-rte binary-numacell binary-nrovalidate

build-e2e-rte: fmt vet binary-e2e-rte

build-e2e-install: fmt vet binary-e2e-install

build-e2e-uninstall: fmt vet binary-e2e-uninstall

build-e2e-all: fmt vet binary-e2e-all

build-e2e-must-gather: fmt vet binary-e2e-must-gather

# backward compatibility
build-must-gather-e2e: build-e2e-must-gather

build-pause: bin-dir
	install -m 755 hack/pause bin/

build-pause-gcc: bin-dir
	gcc -Wall -g -Os -static -o bin/pause hack/pause.c

bin-dir:
	@mkdir -p bin || :

run: manifests generate fmt vet ## Run a controller from your host.
	go run ./main.go

# backward compatibility
docker-build: container-build

docker-push: container-push

container-build: #test ## Build container image with the manager.
	$(CONTAINER_ENGINE) build -t ${IMG} .

container-push: ## Push container image with the manager.
	$(CONTAINER_ENGINE) push ${IMG}

container-build-pause:
	$(CONTAINER_ENGINE) build -f Dockerfile.pause.gcc -t ${REPO}/pause:test-ci . 

container-push-pause:
	$(CONTAINER_ENGINE) push ${REPO}/pause:test-ci . 

##@ Deployment

install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

deploy-mco-crds: build-tools
	@echo "Verifying that the MCO CRDs are present in the cluster"
	hack/deploy-mco-crds.sh

deploy: manifests kustomize deploy-mco-crds ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build $(KUSTOMIZE_DEPLOY_DIR) | kubectl apply -f -

undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build $(KUSTOMIZE_DEPLOY_DIR) | kubectl delete -f -
	# do not remove machine config pool CRD here, because the deployment can run on top of the OpenShift environment
	# do not remove kubeletconfig CRD here, because the deployment can run on top of the OpenShift environment

CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.14.0)

KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v4@v4.5.4)

# use the last version before the bump golang 1.20 -> 1.22. We want to avoid the go.mod version format changes - for now.
ENVTEST = $(shell pwd)/bin/setup-envtest
envtest: ## Download envtest-setup locally if necessary.
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest@v0.0.0-20230927023946-553bd00cfec5)

# go-install-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-install-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go install $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef

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

.PHONY: operator-sdk
operator-sdk:
	@if [ ! -x "$(OPERATOR_SDK)" ]; then\
		echo "Downloading operator-sdk $(OPERATOR_SDK_VERSION)";\
		mkdir -p $(BIN_DIR);\
		curl -JL https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/$(OPERATOR_SDK_BIN) -o $(OPERATOR_SDK);\
		chmod +x $(OPERATOR_SDK);\
	else\
		echo "Using operator-sdk cached at $(OPERATOR_SDK)";\
	fi

.PHONY: opm
opm: ## Download opm locally if necessary.
	@if [ ! -x "$(OPM)" ]; then\
		echo "Downloading opm $(OPM_VERSION)";\
		curl -JL https://github.com/operator-framework/operator-registry/releases/download/$(OPM_VERSION)/$(OPM_BIN) -o $(OPM);\
		chmod +x $(OPM);\
	else\
		echo "Using OPM cached at $(OPM_BIN)";\
	fi

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

.PHONY: deps-update
deps-update:
	go mod tidy && go mod vendor


# Build tools:
#
.PHONY: goversion
goversion:
	@go version

.PHONY: build-tools
build-tools: goversion bin/buildhelper bin/envsubst bin/lsplatform

.PHONY: build-tools-all
build-tools-all: goversion bin/buildhelper bin/envsubst bin/lsplatform bin/catkubeletconfmap bin/watchnrtattr bin/mkginkgolabelfilter

bin/buildhelper:
	@go build -o bin/buildhelper tools/buildhelper/buildhelper.go

bin/envsubst:
	@go build -o bin/envsubst tools/envsubst/envsubst.go

bin/lsplatform:
	@go build -o bin/lsplatform tools/lsplatform/lsplatform.go

bin/catkubeletconfmap:
	@go build -o bin/catkubeletconfmap tools/catkubeletconfmap/catkubeletconfmap.go

bin/watchnrtattr:
	@go build -o bin/watchnrtattr tools/watchnrtattr/watchnrtattr.go

bin/mkginkgolabelfilter:
	@go build -o bin/mkginkgolabelfilter tools/mkginkgolabelfilter/mkginkgolabelfilter.go

verify-generated: bundle generate
	@echo "Verifying that all code is committed after updating deps and formatting and generating code"
	hack/verify-generated.sh

install-git-hooks:
	git config core.hooksPath .githooks	


GOLANGCI_LINT_LOCAL_VERSION := $(shell command ${GOLANGCI_LINT} --version 2> /dev/null | awk '{print $$4}')
.PHONY: golangci-lint
golangci-lint:
	@if [ ! -x "$(GOLANGCI_LINT)" ]; then\
		echo "Downloading golangci-lint from https://github.com/golangci/golangci-lint/releases/download/v$(GOLANGCI_LINT_VERSION)/$(GOLANGCI_LINT_ARTIFACT_FILE)";\
		mkdir -p $(BIN_DIR);\
		curl -JL https://github.com/golangci/golangci-lint/releases/download/v$(GOLANGCI_LINT_VERSION)/$(GOLANGCI_LINT_ARTIFACT_FILE) -o $(BIN_DIR)/$(GOLANGCI_LINT_ARTIFACT_FILE);\
		pushd $(BIN_DIR);\
		tar --no-same-owner -xzf $(GOLANGCI_LINT_ARTIFACT_FILE)  --strip-components=1 $(GOLANGCI_LINT_NAME)/$(GOLANGCI_LINT_EXEC_NAME);\
		chmod +x $(GOLANGCI_LINT_EXEC_NAME);\
		rm -f $(GOLANGCI_LINT_ARTIFACT_FILE);\
		popd;\
	else\
		echo "Using golangci-lint cached at $(GOLANGCI_LINT), current version $(GOLANGCI_LINT_LOCAL_VERSION) expected version: $(GOLANGCI_LINT_VERSION)";\
	fi
	$(GOLANGCI_LINT) run --verbose --print-resources-usage -c .golangci.yaml
