.DEFAULT_GOAL := help

BIN := bin
IMAGE ?= gitlabanak:dev
GOPKGDIRS = $(shell go list -f '{{.Dir}}' ./...)

.PHONY: help fmtcheck vet lint tidycheck test build smoke demo image imagecheck xbuild vulncheck check ci hooks

help: ## list targets
	@grep -E '^[a-z]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

fmtcheck: ## fail on any file gofmt would change
	@dirs="$(GOPKGDIRS)"; [ -n "$$dirs" ] || { echo "go list returned no packages"; exit 1; }; \
	out=$$(gofmt -l $$dirs); [ -z "$$out" ] || { echo "gofmt needed:"; echo "$$out"; exit 1; }

vet: ## go vet
	go vet ./...

lint: export GOLANGCI_LINT_CACHE := $(CURDIR)/$(BIN)/golangci-lint-cache
lint: ## golangci-lint
	golangci-lint run ./...

tidycheck: ## fail if go.mod or go.sum are not tidy
	go mod tidy -diff

test: ## unit tests with the race detector, no cache
	go test -race -count=1 ./...

build: ## build the binary into bin/
	CGO_ENABLED=0 go build -o $(BIN)/gitlabanak ./cmd/gitlabanak

smoke: build ## run the real binary on a free port and check the basics
	tools/smoke.sh

demo: build ## run python-gitlab against the binary twice and compare (needs uv)
	tools/demo.sh

image: ## build the container image
	docker build -t $(IMAGE) .

imagecheck: image ## check image size, user and start time
	tools/imagecheck.sh $(IMAGE)

xbuild: ## cross-compile linux amd64 and arm64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/gitlabanak
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /dev/null ./cmd/gitlabanak

vulncheck: ## govulncheck (fails only on vulnerabilities our code can reach)
	go run golang.org/x/vuln/cmd/govulncheck@v1.10.0 ./...

check: fmtcheck vet lint tidycheck ## fast static gates

ci: check test xbuild ## everything CI runs

hooks: ## install the pre-push hook
	printf '#!/bin/sh\nexec make check test\n' > .git/hooks/pre-push
	chmod +x .git/hooks/pre-push
