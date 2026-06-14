BINARY := ckad-trainer
PKG := ./cmd/ckad-trainer
PREFIX ?= /usr/local
VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: all build install test smoke fmt vet headers check clean dist

all: check build

build: ## Build the binary (embeds the scenario catalog)
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

dist: ## Cross-compile release binaries into dist/ (set VERSION=vX.Y.Z)
	@rm -rf dist && mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; out=dist/$(BINARY)-$$os-$$arch; \
		[ $$os = windows ] && out=$$out.exe; \
		echo "  $$out"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $$out $(PKG) || exit 1; \
	done

install: build ## Install the binary to $(PREFIX)/bin
	install -m 0755 $(BINARY) $(PREFIX)/bin/$(BINARY)

test: ## Run the cluster-free tests (unit + catalog render across seeds)
	go test ./...

smoke: ## Run the cluster smoke test (mutates the configured cluster)
	go test -tags=cluster -run TestSmokeSolutions . -timeout 590s

fmt: ## Format the code
	gofmt -w .

vet: ## Vet the code
	go vet ./...

headers: ## Add the GPL SPDX header to any Go file missing it
	./scripts/license-headers.sh

check: fmt vet test ## Format, vet, test and verify license headers (pre-commit gate)
	./scripts/license-headers.sh --check
	@gofmt -l . | grep . && { echo "unformatted files"; exit 1; } || true

clean: ## Remove the built binary and local state
	rm -f $(BINARY)
	rm -rf state

help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  %-10s %s\n", $$1, $$2}'
