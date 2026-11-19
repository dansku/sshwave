BIN       := sshwave
PKG       := ./cmd/sshwave
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -s -w -X main.version=$(VERSION)
PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64

.PHONY: help build install run test cover golden fmt fmt-check vet lint verify vuln check dist screenshot demo clean

help: ## Show this help
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

build: ## Build the binary with the version from git
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) $(PKG)

install: ## Install the binary to GOBIN
	go install -trimpath -ldflags "$(LDFLAGS)" $(PKG)

run: ## Run the app on ~/.ssh/config (use CONFIG=path for another file)
	go run $(PKG) $(if $(CONFIG),-config $(CONFIG))

test: ## Run all tests
	go test ./...

cover: ## Run all tests and show the coverage of each function
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

golden: ## Refresh golden views (review the diff)
	go test ./internal/ui -update

fmt: ## Format the code
	gofmt -w .

fmt-check: ## Fail if gofmt would change a file
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt would change:"; echo "$$out"; exit 1; fi

vet: ## Run go vet
	go vet ./...

lint: fmt-check vet ## Check the format and run go vet

verify: ## Verify module checksums
	go mod verify

vuln: ## Scan for known vulnerabilities
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

check: fmt vet test verify ## Format, vet, test, verify

dist: ## Cross-compile release archives and SHA256SUMS into dist/
	@rm -rf dist
	@set -e; for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; name=$(BIN)_$${os}_$${arch}; \
		echo "build $$name"; \
		mkdir -p dist/$$name; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o dist/$$name/$(BIN) $(PKG); \
		cp LICENSE README.md dist/$$name/; \
		tar -C dist -czf dist/$$name.tar.gz $$name; \
	done
	@cd dist && if command -v sha256sum >/dev/null 2>&1; then sha256sum *.tar.gz > SHA256SUMS; else shasum -a 256 *.tar.gz > SHA256SUMS; fi
	@cat dist/SHA256SUMS

screenshot: ## Redraw docs/assets/screenshot.svg from the fixture
	go test ./internal/ui -run TestScreenshot -screenshot "$(CURDIR)/docs/assets/screenshot.svg"

demo: ## Record docs/assets/demo.gif with VHS
	@command -v vhs >/dev/null 2>&1 || { echo "vhs is not installed. See https://github.com/charmbracelet/vhs, then run 'make demo' again."; exit 1; }
	go build -o $(BIN) $(PKG)
	vhs docs/assets/demo.tape

clean: ## Remove build output
	rm -f $(BIN) coverage.out
	rm -rf dist
