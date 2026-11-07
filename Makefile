# docsync Makefile (from the otfabric/engineering CLI template, policy 1.3).
# Local binaries go in root bin/, cross-compiled packaging in root release/ (GO-013).
.DEFAULT_GOAL := help
.PHONY: docs-check help check build build-all install fmt fmt-check vet lint lint-ci vuln test coverage coverage-pkg-check e2e clean

export GOWORK := off
BIN := docsync
BIN_DIR := bin
RELEASE_DIR := release
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell git log -1 --format=%cI 2>/dev/null || echo unknown)
LDFLAGS := -s -w -X main.version=$(VERSION) 
COVER_MIN := 85
# Per-package floor (TEST-012).
COVER_PKG_MIN := 70

help: ## This help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_-]+:.*?## / {printf "\033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

check: fmt-check vet lint-ci vuln test coverage coverage-pkg-check docs-check ## Everything CI's PR gate runs

build: ## Build ./bin/docsync
	@CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BIN) .

build-all: ## Cross-compile release platforms into release/
	@for p in linux/amd64 linux/arm64 linux/arm darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do \
		CGO_ENABLED=0 GOOS=$${p%/*} GOARCH=$${p#*/} go build -trimpath -ldflags "$(LDFLAGS)" -o $(RELEASE_DIR)/$(BIN)-$${p%/*}-$${p#*/} . || exit 1; done

install: ## go install with version ldflags
	@go install -trimpath -ldflags "$(LDFLAGS)" .

fmt: ## gofmt
	@gofmt -w .
fmt-check: ## Fail if gofmt would change a file
	@out="$$(gofmt -l .)"; [ -z "$$out" ] || { echo "$$out"; exit 1; }
vet: ## go vet
	@go vet ./...
lint: lint-ci ## Alias
lint-ci: ## golangci-lint
	@golangci-lint run ./...
vuln: ## govulncheck
	@govulncheck ./...
test: ## Tests (race), including cmd/
	@go test -count=1 -race ./...
coverage-pkg-check: coverage ## Fail when a package is below COVER_PKG_MIN
	@awk -v m="$(COVER_PKG_MIN)" 'NR > 1 { n[$$1] = $$2; if ($$3 > 0) h[$$1] = 1 } \
	END { for (k in n) { p = k; sub(/\/[^\/]*$$/, "", p); s[p] += n[k]; if (k in h) c[p] += n[k] } \
	for (p in s) { v = 100 * c[p] / s[p]; if (v < m) { printf "%s: %.1f%% < %s%%\n", p, v, m; bad = 1 } } exit bad }' coverage.out

coverage: ## Coverage over ./... with COVER_MIN gate
	@go test -count=1 -race -coverpkg=./... -coverprofile=coverage.out ./... >/dev/null
	@total=$$(go tool cover -func=coverage.out | awk '/^total:/ {sub("%","",$$3); print $$3}'); \
	awk -v t="$$total" -v m="$(COVER_MIN)" 'BEGIN { if (t+0 < m+0) { printf "coverage %.1f%% < %s%%\n", t, m; exit 1 } else printf "coverage %.1f%%\n", t }'
e2e: ## Build the binary and run the e2e suite against it
	@go test -count=1 ./e2e/...
clean: ## Remove build output
	@rm -rf $(BIN_DIR) $(RELEASE_DIR) coverage.out
docs-check: ## docsync on its own documents
	@go run . check .
update-golden: ## Rewrite the golden files of the fixture tests
	@go test ./internal/cli/ -run 'Fixture|Formats' -update
