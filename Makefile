GO ?= go
GOFMT ?= gofmt
GOFILES := $(shell find . -type f -name '*.go' -not -path './.git/*')

.PHONY: format fmt fmtcheck lint vet test test-alpha test-media test-skills ci build run clean install-local uninstall-local

format:
	$(GOFMT) -w $(GOFILES)

fmt: format

fmtcheck:
	@test -z "$$($(GOFMT) -l $(GOFILES))"

lint:
	$(GO) vet ./...

vet: lint

test:
	$(GO) test ./...

test-alpha:
	$(GO) test ./tests/integration -run TestAlphaAcceptance -count=1 -v

test-media:
	$(GO) test ./tests/integration -run TestMediaStackAcceptance -count=1 -v

test-skills:
	$(GO) test ./tests/integration -run TestSkillLifecycleCrudAndInvocation -count=1 -v

ci: fmtcheck lint test
	bash scripts/tests/make-ci-target-test.sh
	bash scripts/tests/verify-pr-template-test.sh
	bash scripts/tests/install-service-test.sh
	$(MAKE) test-alpha
	$(MAKE) build

build:
	mkdir -p bin
	$(GO) build -o bin/odin ./cmd/odin
	$(GO) build -o bin/odin-os ./cmd/odin-os

run:
	$(GO) run ./cmd/odin-os

clean:
	rm -rf bin/odin bin/odin-os

install-local: build
	./scripts/dev/install-local.sh

uninstall-local:
	./scripts/dev/uninstall-local.sh
