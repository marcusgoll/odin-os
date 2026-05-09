GO ?= go
GOFMT ?= gofmt
GOFILES := $(shell git ls-files '*.go')

.PHONY: format fmt fmtcheck lint vet test test-alpha test-media test-skills ci build docker-smoke odin-e2e-local odin-e2e-contract run clean install-local uninstall-local

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
	bash scripts/tests/assert-odin-e2e-contract-test.sh
	bash scripts/tests/odin-e2e-workflow-test.sh
	bash scripts/tests/github-actions-permissions-test.sh
	bash scripts/tests/google-driver-security-test.sh
	bash scripts/tests/make-ci-target-test.sh
	bash scripts/tests/docker-compose-smoke-test.sh
	bash scripts/tests/verify-pr-template-test.sh
	bash scripts/tests/install-service-test.sh
	$(MAKE) test-alpha
	$(MAKE) build

build:
	mkdir -p bin
	$(GO) build -o bin/odin ./cmd/odin
	$(GO) build -o bin/odin-os ./cmd/odin-os
	$(GO) build -o bin/huginn-browser-worker ./cmd/huginn-browser-worker

docker-smoke:
	bash scripts/tests/docker-compose-smoke.sh

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
