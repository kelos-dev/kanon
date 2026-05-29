SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: test
test: ## Run unit tests.
	go test ./...

.PHONY: update
update: ## Run formatters and update module metadata.
	go fmt ./...
	go mod tidy

.PHONY: verify
verify: ## Verify formatting, modules, vet, and tests.
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './bin/*'))"
	go mod tidy
	git diff --exit-code -- go.mod go.sum
	go vet ./...
	go test ./...

##@ Build

.PHONY: build
build: ## Build the kanon binary.
	@mkdir -p bin
	CGO_ENABLED=0 go build -o bin/kanon .
