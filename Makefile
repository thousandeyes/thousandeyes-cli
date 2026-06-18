GO ?= go
GOFILES := $(shell find . -name '*.go' -not -path './vendor/*')

.PHONY: deps fmt fmt-check validate test build ci

deps:
	$(GO) mod download

fmt:
	gofmt -w $(GOFILES)

fmt-check:
	@test -z "$$(gofmt -l $(GOFILES))" || (gofmt -l $(GOFILES); exit 1)

validate: fmt-check
	$(GO) vet ./...
	$(GO) run ./cmd/tools/check-cli-command-coverage

test:
	$(GO) tool gotestsum --junitfile=test-report.xml --format standard-verbose -- \
		-coverprofile=coverage.out -covermode=atomic ./...

build:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -o bin/thousandeyes .

ci: deps validate test build
