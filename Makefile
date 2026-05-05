.PHONY: all build test vet fmt clean build-linux

GO  ?= go
BIN := bin/imgcdc

all: vet test build

build:
	$(GO) build -o $(BIN) ./cmd/imgcdc

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build \
		-ldflags="-s -w" -o $(BIN) ./cmd/imgcdc

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	gofmt -w .

clean:
	rm -rf bin/
