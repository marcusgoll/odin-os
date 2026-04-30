GO ?= go
GOFMT ?= gofmt
GOFILES := $(shell find . -type f -name '*.go' -not -path './.git/*' -not -path './node_modules/*' -not -path './.worktrees/*')

.PHONY: format fmt fmtcheck lint vet test test-alpha ci build odin-e2e-local odin-e2e-contract run clean install-local uninstall-local

format:
	$(GOFMT) -w $(GOFILES)

fmt: format

fmtcheck:
	@test -z "$$($(GOFMT) -l $(GOFILES))"

lint:
	$(GO) list ./... | grep -v '/node_modules/' | grep -v '/.worktrees/' | xargs $(GO) vet

vet:
	$(GO) list ./... | grep -v '/node_modules/' | grep -v '/.worktrees/' | xargs $(GO) vet

test:
	$(GO) list ./... | grep -v '/node_modules/' | grep -v '/.worktrees/' | xargs $(GO) test

test-alpha:
	$(GO) test ./tests/integration -run TestAlphaAcceptance -count=1 -v

ci: fmtcheck lint test
	bash scripts/tests/make-ci-target-test.sh
	bash scripts/tests/verify-pr-template-test.sh
	bash scripts/tests/codex-odin-task-test.sh
	bash scripts/tests/assert-odin-e2e-contract-test.sh
	bash scripts/tests/odin-e2e-workflow-test.sh
	$(MAKE) test-alpha
	$(MAKE) build

build:
	mkdir -p bin
	$(GO) build -o bin/odin ./cmd/odin
	$(GO) build -o bin/odin-os ./cmd/odin-os

odin-e2e-local:
	./scripts/odin-e2e-local.sh

odin-e2e-contract:
	./scripts/assert-odin-e2e-contract.sh

run:
	$(GO) run ./cmd/odin-os

clean:
	rm -rf bin/odin bin/odin-os

install-local: build
	./scripts/dev/install-local.sh

uninstall-local:
	./scripts/dev/uninstall-local.sh
