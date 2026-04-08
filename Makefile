GO ?= go
GOFMT ?= gofmt
GOFILES := $(shell find . -type f -name '*.go' -not -path './.git/*')

.PHONY: format fmtcheck lint test build

format:
	$(GOFMT) -w $(GOFILES)

fmtcheck:
	@test -z "$$($(GOFMT) -l $(GOFILES))"

lint:
	$(GO) vet ./...

test:
	$(GO) test ./...

build:
	mkdir -p bin
	$(GO) build -o bin/odin ./cmd/odin
