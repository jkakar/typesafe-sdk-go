# The stable developer interface for this repository. Run `make check` before
# opening a pull request.

GO ?= go
COVERAGE_FILE ?= cover.out

# The packages coverage is measured against. Programs under examples/ are
# documentation: the compiler and the linter keep them honest, and a test that
# ran them would prove nothing a reader cares about.
COVER_PKGS = $(shell $(GO) list ./... | grep -v '/examples/' | paste -sd, -)

.DEFAULT_GOAL := check

.PHONY: help
help: ## Show the available targets
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'

.PHONY: check
check: fmt-check tidy-check lint-tests vet lint test ## Run every check CI runs

.PHONY: fix
fix: ## Format the source and tidy the module
	$(GO) fmt ./...
	$(GO) mod tidy

.PHONY: fmt-check
fmt-check: ## Fail when a file is not gofmt clean
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt reports these files; run 'make fix':"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

.PHONY: tidy-check
tidy-check: ## Fail when go.mod or go.sum is stale
	@cp go.mod go.mod.check && cp go.sum go.sum.check
	@$(GO) mod tidy
	@if ! cmp -s go.mod go.mod.check || ! cmp -s go.sum go.sum.check; then \
		mv go.mod.check go.mod; mv go.sum.check go.sum; \
		echo "go.mod or go.sum is stale; run 'make fix'"; \
		exit 1; \
	fi
	@rm -f go.mod.check go.sum.check

.PHONY: lint-tests
lint-tests: ## Check the mechanical rules in docs/testing.md
	$(GO) run ./cmd/lint-tests .

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run

.PHONY: test
test: ## Run the tests with the race detector, leak detection and coverage
	$(GO) test -race -shuffle=on -coverpkg=$(COVER_PKGS) -coverprofile=$(COVERAGE_FILE) ./...
	@$(GO) tool cover -func=$(COVERAGE_FILE) | tail -1

.PHONY: cover
cover: test ## Report the statements no test reaches
	@$(GO) tool cover -func=$(COVERAGE_FILE) | awk '$$3 != "100.0%"'
	@echo "Write the report to cover.html with: go tool cover -html=$(COVERAGE_FILE) -o cover.html"

.PHONY: doc
doc: ## Serve the package documentation at http://localhost:6060
	$(GO) run golang.org/x/tools/cmd/godoc@latest -http=:6060
