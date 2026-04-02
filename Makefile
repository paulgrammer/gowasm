# gowasm — build, test and verify the tool itself.
#
# The examples are separate Go modules, so they are driven through the built
# binary rather than the root module's package list.

GO       ?= go
BIN      := bin/gowasm
DIST     := dist
ARCHIVES := dist/archives
EXAMPLES := urls blob worker-pool ginapi
GOROOT_  := $(shell $(GO) env GOROOT)
JS_EXEC  := $(GOROOT_)/lib/wasm/go_js_wasm_exec

# Build stamping. VERSION comes from the git tag when there is one, so a release
# built from a tagged commit reports that tag and nothing has to be edited.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

PKG     := github.com/paulgrammer/gowasm/internal/cli
LDFLAGS := -s -w \
  -X $(PKG).Version=$(VERSION) \
  -X $(PKG).Commit=$(COMMIT) \
  -X $(PKG).Date=$(DATE)

# The platform matrix released binaries are cross-compiled to.
#
# CGO is off for every target, which makes the binaries fully static and
# portable. gowasm has no cgo dependencies of its own, so this costs nothing:
# it only shells out to the go and npm commands already on the user's machine.
CROSS_TARGETS := \
  darwin/arm64 \
  darwin/amd64 \
  linux/arm64 \
  linux/amd64 \
  windows/arm64 \
  windows/amd64

# sha256sum on Linux, shasum on macOS.
SHASUM := $(shell command -v sha256sum >/dev/null 2>&1 && echo "sha256sum" || echo "shasum -a 256")

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
	  | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the gowasm binary into bin/
	$(GO) build -ldflags="$(LDFLAGS)" -o $(BIN) ./cmd/gowasm

# Binary names carry no version, so steps between building and packaging --
# a signing step, say -- do not have to know it. The version appears on the
# archive instead, which is what people download.
.PHONY: cross
cross: ## Cross-compile the binary for every released platform
	@rm -rf $(DIST) && mkdir -p $(DIST)
	@for target in $(CROSS_TARGETS); do \
	  goos=$${target%/*}; goarch=$${target#*/}; \
	  ext=""; [ "$$goos" = "windows" ] && ext=".exe"; \
	  echo "  $$goos/$$goarch"; \
	  CGO_ENABLED=0 GOOS=$$goos GOARCH=$$goarch \
	    $(GO) build -trimpath -ldflags="$(LDFLAGS)" \
	      -o "$(DIST)/gowasm-$$goos-$$goarch$$ext" ./cmd/gowasm || exit 1; \
	done
	@echo "binaries in $(DIST)/"

# Packaging is separate from building, and never rebuilds: anything done to the
# binaries in between -- signing, notarizing, stripping -- survives.
.PHONY: package
package: ## Archive the binaries already in dist/, and checksum them
	@test -d $(DIST) || { echo "nothing to package; run 'make cross' first"; exit 1; }
	@mkdir -p $(ARCHIVES)
	@for target in $(CROSS_TARGETS); do \
	  goos=$${target%/*}; goarch=$${target#*/}; \
	  ext=""; [ "$$goos" = "windows" ] && ext=".exe"; \
	  bin="$(DIST)/gowasm-$$goos-$$goarch$$ext"; \
	  test -f "$$bin" || { echo "missing $$bin; run 'make cross' first"; exit 1; }; \
	  base="gowasm-$(VERSION)-$$goos-$$goarch"; \
	  stage=$$(mktemp -d); \
	  cp "$$bin" "$$stage/gowasm$$ext"; \
	  if [ "$$goos" = "windows" ]; then \
	    ( cd "$$stage" && zip -q "$$base.zip" "gowasm$$ext" ) && \
	      mv "$$stage/$$base.zip" $(ARCHIVES)/; \
	  else \
	    tar -czf "$(ARCHIVES)/$$base.tar.gz" -C "$$stage" "gowasm$$ext"; \
	  fi; \
	  rm -rf "$$stage"; \
	  echo "  packaged $$base"; \
	done
	@cd $(ARCHIVES) && $(SHASUM) *.tar.gz *.zip > checksums.txt
	@echo "archives and checksums.txt in $(ARCHIVES)/"
	@ls -1 $(ARCHIVES)

.PHONY: dist
dist: cross package ## Cross-compile, archive per platform, and checksum

.PHONY: test
test: ## Run the tool's unit tests
	$(GO) test ./...

.PHONY: test-runtime
test-runtime: ## Test the Go runtime bridge under the real wasm_exec.js
	@# The explicit -exec is deliberate: cmd/go does not add lib/wasm to PATH,
	@# so a stale go_js_wasm_exec from an older Go would be found first and
	@# resolve into the misc/wasm directory that no longer exists.
	GOOS=js GOARCH=wasm $(GO) test -exec="$(JS_EXEC)" ./internal/wasmbridge/...

.PHONY: examples
examples: build ## Build and test every example end to end
	@for ex in $(EXAMPLES); do \
	  echo "=== examples/$$ex ==="; \
	  ( cd examples/$$ex && $(GO) test ./... >/dev/null && ../../$(BIN) test ) || exit 1; \
	done

.PHONY: check-generated
check-generated: build ## Fail if regenerating changes anything (CI drift guard)
	@for ex in $(EXAMPLES); do \
	  ( cd examples/$$ex && ../../$(BIN) generate >/dev/null ) || exit 1; \
	done
	@git diff --exit-code -- examples || \
	  { echo "generated output is stale; run 'make examples' and commit"; exit 1; }

.PHONY: lint
lint: ## Vet and check formatting
	$(GO) vet ./...
	@unformatted=$$(gofmt -l . | grep -v '^examples/' || true); \
	  if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi

.PHONY: verify
verify: lint test test-runtime examples ## Everything CI should run

.PHONY: clean
clean: ## Remove build output and generated packages
	rm -rf bin $(DIST)
	@for ex in $(EXAMPLES); do rm -rf examples/$$ex/node; done

.PHONY: install
install: ## Install gowasm onto your PATH
	$(GO) install -ldflags="$(LDFLAGS)" ./cmd/gowasm

.PHONY: version
version: ## Print the version that would be stamped into a build
	@echo "$(VERSION) ($(COMMIT), $(DATE))"
