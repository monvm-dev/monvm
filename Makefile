# MonVM <https://monvm.dev>
# Copyright The MonVM Authors
# SPDX-License-Identifier: Apache-2.0

.DEFAULT_GOAL := help

BINDIR := bin
BUILDVARS_PKG := github.com/monvm-dev/monvm/internal/buildvars
BUILD_VERSION ?= dev
BUILD_DATE ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
COMMIT_HASH ?= unknown
COMMIT_DATE ?= unknown
COMMIT_BRANCH ?= unknown

.PHONY: help setup fmt precommit lint lint-go lint-shell lint-infra test build website clean

help: ## Show available targets
	@echo "Usage: make <target>"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

setup: ## Verify required tools and enable git hooks
	@for tool in go shellcheck tofu; do command -v $$tool >/dev/null || { echo "$$tool is required"; exit 1; }; done
	@go tool golangci-lint version >/dev/null
	@cp scripts/git-hooks/pre-commit .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@cp scripts/git-hooks/commit-msg .git/hooks/commit-msg
	@chmod +x .git/hooks/commit-msg
	@echo "Required tools found and Git hooks installed."

fmt: ## Format source and infrastructure
	gofmt -w main.go cmd internal scripts
	tofu fmt -recursive internal/cli/infra

precommit: ## Run fast checks
	@cmp -s main.go cmd/monvm/main.go || { echo "entrypoints must be identical"; exit 1; }
	@test -z "$$(gofmt -l main.go cmd internal scripts)" || { gofmt -l main.go cmd internal scripts; exit 1; }
	@shellcheck scripts/git-hooks/*
	@find scripts/git-hooks -type f -exec bash -n {} \;
	@tofu -chdir=internal/cli/infra/aws fmt -check
	@go test ./...

lint: lint-go lint-shell lint-infra ## Run all linters

lint-go: ## Lint Go
	go tool golangci-lint run --timeout=5m

lint-shell: ## Validate rendered user data
	shellcheck scripts/git-hooks/*
	find scripts/git-hooks -type f -exec bash -n {} \;
	scripts/validate-userdata.sh

lint-infra: ## Validate the TF module with OpenTofu
	@set -eu; tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; cp internal/cli/infra/aws/* "$$tmp"/; \
		tofu -chdir="$$tmp" init -backend=false -input=false >/dev/null; tofu -chdir="$$tmp" validate

test: ## Run tests with the race detector
	go test -race ./...

build: ## Build the CLI
	mkdir -p $(BINDIR)
	CGO_ENABLED=0 go build -trimpath -o $(BINDIR)/monvm -ldflags "-s -w \
		-X $(BUILDVARS_PKG).buildVersion=$(BUILD_VERSION) -X $(BUILDVARS_PKG).buildDate=$(BUILD_DATE) \
		-X $(BUILDVARS_PKG).commitHash=$(COMMIT_HASH) -X $(BUILDVARS_PKG).commitDate=$(COMMIT_DATE) \
		-X $(BUILDVARS_PKG).commitBranch=$(COMMIT_BRANCH)" ./cmd/monvm

website: ## Render the documentation website
	go run ./scripts/sitegen -out dist/website

clean: ## Remove generated output
	rm -rf $(BINDIR) dist
