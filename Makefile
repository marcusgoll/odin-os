GO ?= go
GOFMT ?= gofmt
GOFILES := $(shell find . -type f -name '*.go' -not -path './.git/*')

.PHONY: format fmtcheck lint test test-alpha build

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
