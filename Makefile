.PHONY: help all test coverage coverage-pkg-check cover compile lint lint-ci fmt fmt-check vet build build-nocheck build-all release-all install clean vuln e2e fuzz chaos interop docs-check check

help: ## This help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_-]+:.*?## / {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.DEFAULT_GOAL := help

# Parent otfabric/go.work does not list this module; isolate toolchain commands.
export GOWORK := off

# CI formats and lints with the Go version from go.mod, not with the toolchain installed here.
# gofmt's output changes between Go releases (comment alignment, for one), so formatting is
# done with that version, and `fmt-check` also requires the local gofmt to agree.
GO_MOD_VERSION := $(shell sed -nE 's/^go ([0-9]+\.[0-9]+).*/\1/p' go.mod | head -n1)
GOFMT_CI = $(shell GOTOOLCHAIN=go$(GO_MOD_VERSION).0 go env GOROOT)/bin/gofmt

# The four tools of this repository: cmd/<tool> is the main package of each.
TOOLS     := iec101ctl iec102ctl iec103ctl iec104ctl
BIN_DIR   := bin
ARCHS     := linux/amd64 linux/arm64 linux/arm/v7 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64
RELEASE_DIR := release

# Version of the tools: the four share one, the tag vX.Y.Z on HEAD; any other build reports
# "dev". VERSION has no leading v, as the release workflow passes it. See docs/RELEASING.md.
tool_tag = $(shell git tag --points-at HEAD 2>/dev/null | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$$' | sort -V | tail -n1)
tool_version = $(or $(patsubst v%,%,$(notdir $(call tool_tag,$(1)))),dev)
# -dirty when the working tree has changes, as Go itself marks vcs.modified.
DIRTY := $(shell [ -z "$$(git status --porcelain 2>/dev/null)" ] || echo -dirty)
COMMIT     ?= $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")$(DIRTY)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
ldflags = -ldflags "-s -w -X 'main.version=$(call tool_version,$(1))' -X 'main.tag=$(call tool_tag,$(1))' -X 'main.commit=$(COMMIT)' -X 'main.buildDate=$(BUILD_DATE)'"

FUZZTIME ?= 10s
CHAOS_SEEDS ?= 20
# The interop suites to run; one tool: INTEROP_PKG=./interop/iec101ctl/...
INTEROP_PKG ?= ./interop/...

# docsync release of docs-check (DOC-008).
DOCSYNC_VERSION := v0.3.1

# Minimum total statement coverage (percent) enforced by `make coverage`.
COVERAGE_MIN ?= 90
# Per-package floor (TEST-012).
COVER_PKG_MIN := 90

all: test build build-all ## Test and build the tools

check: fmt fmt-check lint lint-ci vet vuln test coverage-pkg-check docs-check ## Run all checks (format, lint, vet, vulnerability scan, test, coverage, docs)

test: ## Run unit tests with race detector
	@echo "Running unit tests (race detector)"
	@go test -count=1 -race ./...

coverage: ## Run tests with whole-module coverage (writes coverage.out); fails below COVERAGE_MIN percent
	@echo "Running tests with coverage"
	@go test -count=1 -race -coverprofile=coverage.out -covermode=atomic -coverpkg=./... ./...
	@total=$$(go tool cover -func=coverage.out | awk '/^total:/ { sub("%", "", $$3); print $$3 }'); \
	echo "Total coverage: $$total% (minimum $(COVERAGE_MIN)%)"; \
	awk -v t="$$total" -v m="$(COVERAGE_MIN)" 'BEGIN { exit (t + 0 < m + 0) ? 1 : 0 }' || \
		{ echo "Coverage $$total% is below the required $(COVERAGE_MIN)%"; exit 1; }

coverage-pkg-check: coverage ## Fail when a package is below COVER_PKG_MIN
	@awk -v m="$(COVER_PKG_MIN)" 'NR > 1 { n[$$1] = $$2; if ($$3 > 0) h[$$1] = 1 } \
	END { for (k in n) { p = k; sub(/\/[^\/]*$$/, "", p); s[p] += n[k]; if (k in h) c[p] += n[k] } \
	for (p in s) { v = 100 * c[p] / s[p]; if (v < m) { printf "%s: %.1f%% < %s%%\n", p, v, m; bad = 1 } } exit bad }' coverage.out

fuzz: ## Run every Fuzz target for FUZZTIME (targets discovered, never listed)
	@for pkg in $$(go list ./...); do \
		for t in $$(go test -list '^Fuzz' $$pkg | grep '^Fuzz'); do \
			echo "fuzz $$pkg $$t"; \
			go test -run '^$$' -fuzz "^$$t$$" -fuzztime $(FUZZTIME) $$pkg || exit 1; \
		done; \
	done

chaos: ## Run the chaos and hostile-peer tests with CHAOS_SEEDS seeds each (race detector)
	@CHAOS_SEEDS=$(CHAOS_SEEDS) go test -count=1 -race -run 'Chaos|Hostile' ./...

cover: coverage ## Open coverage report in browser (run coverage first)
	@go tool cover -html=coverage.out

compile: ## Typecheck entire module, the e2e and interop suites included
	@echo "Typechecking all packages (go build ./... and go vet -tags=e2e,interop)"
	@go build ./...
	@go vet -tags=e2e,interop ./e2e/... ./interop/...

lint: compile ## Run staticcheck after full-module compile
	@echo "Running staticcheck"
	@staticcheck -tags=e2e,interop ./...

lint-ci: compile ## go vet + golangci-lint (typecheck + configured linters)
	@echo "Running go vet"
	@go vet ./...
	@echo "Running golangci-lint"
	@golangci-lint run ./...

fmt: ## Format Go code with the gofmt of the Go version in go.mod (what CI uses)
	@echo "Running gofmt (go$(GO_MOD_VERSION))"
	@$(GOFMT_CI) -w .

fmt-check: ## Fail if the go.mod-version gofmt or the local gofmt would change any file
	@echo "Checking formatting (gofmt go$(GO_MOD_VERSION) and local $$(go env GOVERSION))"
	@ci="$$($(GOFMT_CI) -l .)"; loc="$$(gofmt -l .)"; \
	if [ -n "$$ci$$loc" ]; then \
		[ -z "$$ci" ] || { echo "Not formatted for gofmt go$(GO_MOD_VERSION) (CI):"; echo "$$ci"; }; \
		[ -z "$$loc" ] || { echo "Not formatted for the local gofmt:"; echo "$$loc"; }; \
		echo "Files must be stable under both. Run 'make fmt'."; \
		exit 1; \
	fi

vet: ## Run go vet on project packages
	@go vet ./...

build: check build-nocheck ## Run all checks, then build the four tools into bin/

build-nocheck: ## Build the four tools into bin/ without running the checks
	@mkdir -p $(BIN_DIR)
	@$(foreach t,$(TOOLS),echo "Building $(BIN_DIR)/$(t) ($(call tool_version,$(t)))" && go build $(call ldflags,$(t)) -o $(BIN_DIR)/$(t) ./cmd/$(t) &&) true

build-all: ## Build every tool for all architectures into release/
	@mkdir -p $(RELEASE_DIR)
	@$(foreach t,$(TOOLS),./scripts/cross-build.sh $(t) "$(call tool_version,$(t))" '$(call ldflags,$(t))' "$(ARCHS)" $(RELEASE_DIR) &&) true

release-all: build-all ## Package the cross builds into tar.gz (Unix) or zip (Windows)
	@./scripts/package.sh $(RELEASE_DIR)

# The documents against the code (DOC-008): docsync, as docs.yml runs it.
docs-check: ## Check that the documents agree with the code (docsync)
	@go run github.com/otfabric/docsync@$(DOCSYNC_VERSION) check .

vuln: ## Scan the module and its dependencies for known vulnerabilities (govulncheck)
	@govulncheck ./...

e2e: build-nocheck ## Build the four tools and run the e2e suite against them and go-iec60870-5's servers
	@E2E_BIN_DIR=$(CURDIR)/$(BIN_DIR) go test -count=1 -tags=e2e ./e2e/...

interop: build-nocheck ## Run the tools against the iec60870-5-interop v0.4.0 images (Docker; pinned by digest in interop/<tool>/; V=1 for -v)
	@INTEROP_BIN_DIR=$(CURDIR)/$(BIN_DIR) go test -count=1 -tags=interop -timeout=40m $(if $(V),-v,) $(INTEROP_PKG)

install: build-nocheck ## Install the four tools to /usr/local/bin
	@$(foreach t,$(TOOLS),sudo install -m 0755 $(BIN_DIR)/$(t) /usr/local/bin/$(t) &&) true

clean: ## Clean the build artifacts
	@rm -rf $(BIN_DIR) coverage.out $(RELEASE_DIR)
