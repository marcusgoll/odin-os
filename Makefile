GO ?= go
GOFMT ?= gofmt
GOFILES := $(shell find . -type f -name '*.go' -not -path './.git/*')

.PHONY: format fmtcheck lint test test-alpha build install-local uninstall-local

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

build:
	mkdir -p bin
	$(GO) build -o bin/odin ./cmd/odin

install-local: build
	./scripts/dev/install-local.sh

uninstall-local:
	./scripts/dev/uninstall-local.sh
