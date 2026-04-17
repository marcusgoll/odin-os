GO ?= go
GOFMT ?= gofmt
GOFILES := $(shell find . -type f -name '*.go' -not -path './.git/*')

.PHONY: format fmtcheck lint test test-alpha ci build install-local uninstall-local

format:
	$(GOFMT) -w $(GOFILES)

fmtcheck:
	@test -z "$$($(GOFMT) -l $(GOFILES))"

lint:
	$(GO) vet ./...

test:
	$(GO) test ./...

test-alpha:
	$(GO) test ./tests/integration -run TestAlphaAcceptance -count=1 -v

ci: fmtcheck lint test
	bash scripts/tests/make-ci-target-test.sh
	bash scripts/tests/verify-pr-template-test.sh
	$(MAKE) test-alpha
	$(MAKE) build

build:
	mkdir -p bin
	$(GO) build -o bin/odin ./cmd/odin

install-local: build
	./scripts/dev/install-local.sh

uninstall-local:
	./scripts/dev/uninstall-local.sh
