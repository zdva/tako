.DEFAULT_GOAL := help
.PHONY: help screenshots package-linux build run test race cover lint vuln fmt tidy integration e2e fuzz build-all check clean

PLATFORMS ?= windows/amd64,windows/arm64,darwin/universal,linux/amd64,linux/arm64
COVER_PKGS := ./internal/model/... ./internal/hostlib/... ./internal/session/... ./internal/layout/... \
	./internal/inputbus/... ./internal/workspace/... ./internal/transport/... ./internal/store/... \
	./internal/secrets/... ./internal/importer/... ./internal/settings/...

help: ## List the targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-12s %s\n", $$1, $$2}'

build: ## Build ./tako for this machine
	go build -o tako ./cmd/tako

run: ## Run the app from source (needs a display)
	go run ./cmd/tako

test: ## Unit and UI tests
	go test -count=1 ./...

race: ## Tests with the race detector
	go test -race -count=1 ./...

cover: ## Coverage of the core packages (gate: 80%)
	go test -count=1 -coverprofile=coverage.out $(COVER_PKGS)
	@go tool cover -func=coverage.out | tail -1

lint: ## golangci-lint
	golangci-lint run ./...

vuln: ## govulncheck
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

fmt: ## Format with gofumpt and goimports
	golangci-lint fmt ./...

tidy: ## go mod tidy
	go mod tidy

integration: ## Tests against a real OpenSSH (downloads a portable sshd once)
	sh scripts/fetch-sshd.sh
	go test -race -count=1 -tags integration ./test/integration/...

package-linux: ## Packaged linux/amd64 app in build/linux-amd64 (ships libghostty-vt next to the binary)
	go tool mygo build -platform linux/amd64 -skip-installer

screenshots: package-linux ## Real-window screenshots into docs/screenshots/m1 (needs DISPLAY, e.g. under xvfb-run)
	sh scripts/fetch-sshd.sh
	TAKO_SHOTS=$(CURDIR)/docs/screenshots/m1 TAKO_BIN=$(CURDIR)/build/linux-amd64/tako go test -count=1 -tags e2e -run Screenshots -v ./test/e2e/...
	TAKO_SHOTS=$(CURDIR)/docs/screenshots/m1/tester go test -count=1 ./internal/ui

e2e: package-linux ## Live-window checks under Xvfb: idle CPU/RSS, cold start, kill -9 recovery
	sh scripts/fetch-sshd.sh
	TAKO_BIN=$(CURDIR)/build/linux-amd64/tako xvfb-run -a go test -count=1 -tags e2e -v ./test/e2e/...

fuzz: ## Short fuzz run of the parsers
	go test -run '^$$' -fuzz FuzzReadPPK -fuzztime 20s ./internal/transport/ssh
	go test -run '^$$' -fuzz FuzzParse -fuzztime 20s ./internal/importer/sshconfig

build-all: ## Cross-compile every platform (no installers, no signing)
	go tool mygo build -platform $(PLATFORMS) -skip-installer

check: fmt lint race ## What CI runs, minus the slow targets

clean:
	rm -rf build dist tako tako.exe coverage.out
