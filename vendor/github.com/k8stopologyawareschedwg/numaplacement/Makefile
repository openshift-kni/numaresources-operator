##@ general

default: test-unit

help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-36s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ tools

tools: leb89dump  ## Build all tools

leb89dump: tools/leb89dump/main.go  ## Build the LEB89 alphabet dump tool
	go build -o _out/$@ ./tools/leb89dump

PKGS = $(shell go list ./... | grep -v /tools/)
PKGS_UNIT = $(shell go list ./... | grep -vE 'tools/|test/codec$$|examples/')

##@ testing

test-unit:  ## Run unit tests
	go test $(PKGS_UNIT)

test-integration:  ## Run integration tests
	go test ./test/codec/...

test-unit-cover:  ## Run unit tests with coverage report
	go test -coverprofile=coverage.out $(PKGS)

cover-view:  ## View the console coverage report
	go tool cover -func=coverage.out

cover-view-html:  ## View the HTML coverage report
	go tool cover -html=coverage.out

##@ quality

fmt-check: ## Check gofmt formatting
	@out="$$(gofmt -l . 2>&1)"; status="$$?"; \
	if [ "$$status" -ne 0 ]; then \
		echo "$$out"; \
		exit "$$status"; \
	fi; \
	unformatted="$$out"; \
	if [ -n "$$unformatted" ]; then \
		echo "The following files are not gofmt-formatted:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

lint: ## Run static checks
	go vet $(PKGS)

vuln-check: ## Run govulncheck against all packages
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...
