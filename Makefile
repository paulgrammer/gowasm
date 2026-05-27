# gowasm — build, test and verify the tool itself.
#
# The examples are separate Go modules, so they are driven through the built
# binary rather than the root module's package list.

GO       ?= go
BIN      := bin/gowasm
DIST     := dist
ARCHIVES := dist/archives
EXAMPLES := urls blob worker-pool ginapi pdf \
            regex money gofmt chip8 chess sanitize expr text \
            highlight excel cue git
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

# The npm distribution: one launcher package plus one package per platform
# holding a single binary, which is how esbuild and swc ship. npm resolves the
# os/cpu fields and installs only the matching binary, so nothing has to be
# downloaded by an install script.
# Scoped, because npm refuses the bare name: it normalises punctuation when
# comparing, so "gowasm" collides with the abandoned "go-wasm" from 2018. The
# binary is still called gowasm; only the install line differs.
NPM_NAME   ?= @paulgrammer/gowasm
# Named separately from the launcher, since these published before the launcher
# name was settled and must keep working whatever it is called.
NPM_PLATFORM_PREFIX ?= gowasm
NPM_AUTHOR ?= paulgrammer <paulmugaya4@gmail.com>
NPM_DIST   := $(DIST)/npm

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

# The inverse of package: restores dist/ from the release archives, so the npm
# distribution can be built from the exact binaries that were released rather
# than from a rebuild. A rebuild would not do: the ldflags stamp a build date,
# so it would produce different bytes for the same commit.
.PHONY: unpack
unpack: ## Restore dist/ binaries from the archives in dist/archives/
	@test -d $(ARCHIVES) || { echo "no archives in $(ARCHIVES)"; exit 1; }
	@if [ -f $(ARCHIVES)/checksums.txt ]; then \
	  echo "verifying checksums"; \
	  ( cd $(ARCHIVES) && $(SHASUM) -c checksums.txt ) || exit 1; \
	fi
	@mkdir -p $(DIST)
	@for target in $(CROSS_TARGETS); do \
	  goos=$${target%/*}; goarch=$${target#*/}; \
	  ext=""; [ "$$goos" = "windows" ] && ext=".exe"; \
	  stage=$$(mktemp -d); \
	  if [ "$$goos" = "windows" ]; then \
	    archive=$$(ls $(ARCHIVES)/*-$$goos-$$goarch.zip 2>/dev/null | head -1); \
	    test -n "$$archive" || { echo "no archive for $$goos/$$goarch"; exit 1; }; \
	    unzip -q -o "$$archive" -d "$$stage"; \
	  else \
	    archive=$$(ls $(ARCHIVES)/*-$$goos-$$goarch.tar.gz 2>/dev/null | head -1); \
	    test -n "$$archive" || { echo "no archive for $$goos/$$goarch"; exit 1; }; \
	    tar -xzf "$$archive" -C "$$stage"; \
	  fi; \
	  mv "$$stage/gowasm$$ext" "$(DIST)/gowasm-$$goos-$$goarch$$ext"; \
	  chmod 0755 "$(DIST)/gowasm-$$goos-$$goarch$$ext"; \
	  rm -rf "$$stage"; \
	  echo "  restored gowasm-$$goos-$$goarch$$ext"; \
	done

.PHONY: npm-packages
npm-packages: ## Build the npm launcher and per-platform packages into dist/npm/
	@test -d $(DIST) || { echo "nothing to package; run 'make cross' first"; exit 1; }
	@rm -rf $(NPM_DIST) && mkdir -p $(NPM_DIST)
	@version=$$(echo "$(VERSION)" | sed 's/^v//'); \
	optional=""; \
	for target in $(CROSS_TARGETS); do \
	  goos=$${target%/*}; goarch=$${target#*/}; \
	  ext=""; [ "$$goos" = "windows" ] && ext=".exe"; \
	  bin="$(DIST)/gowasm-$$goos-$$goarch$$ext"; \
	  test -f "$$bin" || { echo "missing $$bin; run 'make cross' first"; exit 1; }; \
	  pkg="$(NPM_PLATFORM_PREFIX)-$$goos-$$goarch"; \
	  dir="$(NPM_DIST)/$$pkg"; \
	  mkdir -p "$$dir/bin"; \
	  cp "$$bin" "$$dir/bin/gowasm$$ext"; \
	  chmod 0755 "$$dir/bin/gowasm$$ext"; \
	  npmos=$$goos; [ "$$goos" = "windows" ] && npmos="win32"; \
	  npmcpu=$$goarch; [ "$$goarch" = "amd64" ] && npmcpu="x64"; \
	  printf '%s\n' \
	    '{' \
	    "  \"name\": \"$$pkg\"," \
	    "  \"version\": \"$$version\"," \
	    "  \"description\": \"gowasm binary for $$goos $$goarch\"," \
	    "  \"author\": \"$(NPM_AUTHOR)\"," \
	    '  "license": "MIT",' \
	    '  "repository": { "type": "git", "url": "git+https://github.com/paulgrammer/gowasm.git" },' \
	    "  \"os\": [\"$$npmos\"]," \
	    "  \"cpu\": [\"$$npmcpu\"]," \
	    '  "files": ["bin"],' \
	    '  "preferUnplugged": true' \
	    '}' > "$$dir/package.json"; \
	  optional="$$optional    \"$$pkg\": \"$$version\",\n"; \
	  echo "  built $$pkg"; \
	done; \
	mkdir -p "$(NPM_DIST)/$(NPM_NAME)/bin"; \
	cp npm/gowasm/bin/gowasm.js "$(NPM_DIST)/$(NPM_NAME)/bin/gowasm.js"; \
	cp npm/gowasm/README.md "$(NPM_DIST)/$(NPM_NAME)/README.md"; \
	chmod 0755 "$(NPM_DIST)/$(NPM_NAME)/bin/gowasm.js"; \
	printf '%s\n' \
	  '{' \
	  "  \"name\": \"$(NPM_NAME)\"," \
	  "  \"version\": \"$$version\"," \
	  '  "description": "Turn a Go package into a typed npm package built on WebAssembly",' \
	  "  \"author\": \"$(NPM_AUTHOR)\"," \
	  '  "license": "MIT",' \
	  '  "repository": { "type": "git", "url": "git+https://github.com/paulgrammer/gowasm.git" },' \
	  '  "keywords": ["go", "golang", "wasm", "webassembly", "codegen", "typescript"],' \
	  '  "bin": { "gowasm": "bin/gowasm.js" },' \
	  '  "files": ["bin", "README.md"],' \
	  '  "engines": { "node": ">=20" },' \
	  '  "optionalDependencies": {' > "$(NPM_DIST)/$(NPM_NAME)/package.json"; \
	printf "$$optional" | sed '$$ s/,$$//' >> "$(NPM_DIST)/$(NPM_NAME)/package.json"; \
	printf '%s\n' '  }' '}' >> "$(NPM_DIST)/$(NPM_NAME)/package.json"; \
	echo "  built $(NPM_NAME) (launcher)"
	@echo "npm packages in $(NPM_DIST)/"

.PHONY: npm-publish
npm-publish: ## Publish the npm packages (platform packages first, then the launcher)
	@test -d $(NPM_DIST) || { echo "run 'make npm-packages' first"; exit 1; }
	@# The launcher lists the platform packages as dependencies, so they have to
	@# exist on the registry before it does, or the first install of it fails.
	@for target in $(CROSS_TARGETS); do \
	  goos=$${target%/*}; goarch=$${target#*/}; \
	  ( cd "$(NPM_DIST)/$(NPM_PLATFORM_PREFIX)-$$goos-$$goarch" && npm publish --access public $(NPM_FLAGS) ) || exit 1; \
	done
	@cd "$(NPM_DIST)/$(NPM_NAME)" && npm publish --access public $(NPM_FLAGS)
	@echo "published $(NPM_NAME)@$(VERSION)"

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
	@find npm -name '*.tgz' -delete 2>/dev/null || true
	@for ex in $(EXAMPLES); do rm -rf examples/$$ex/node; done

.PHONY: install
install: ## Install gowasm onto your PATH
	$(GO) install -ldflags="$(LDFLAGS)" ./cmd/gowasm

.PHONY: version
version: ## Print the version that would be stamped into a build
	@echo "$(VERSION) ($(COMMIT), $(DATE))"
