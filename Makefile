.PHONY: all build test vet fmt clean build-linux snapshot release-check

GO      ?= go
BIN     := bin/imgcdc
PKG     := ./cmd/imgcdc

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
  -X main.version=$(VERSION) \
  -X main.commit=$(COMMIT) \
  -X main.date=$(DATE)

all: vet test build

build:
	$(GO) build -ldflags='$(LDFLAGS)' -o $(BIN) $(PKG)

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build \
	  -ldflags='$(LDFLAGS)' -o $(BIN) $(PKG)

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w .

clean:
	rm -rf bin/ dist/

snapshot:
	goreleaser release --snapshot --clean

release-check:
	goreleaser check
