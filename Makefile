VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  = -ldflags="-s -w -X main.version=$(VERSION)"
IMAGE   ?= panelgen

.PHONY: build build-all docker release-check release-snapshot clean

## build: build for the current platform
build:
	go build $(LDFLAGS) -o panelgen .

## build-all: cross-compile for Linux, macOS, and Windows
build-all:
	mkdir -p dist
	GOOS=linux   GOARCH=amd64  CGO_ENABLED=0 go build $(LDFLAGS) -o dist/panelgen-linux-amd64 .
	GOOS=linux   GOARCH=arm64  CGO_ENABLED=0 go build $(LDFLAGS) -o dist/panelgen-linux-arm64 .
	GOOS=darwin  GOARCH=amd64  CGO_ENABLED=0 go build $(LDFLAGS) -o dist/panelgen-darwin-amd64 .
	GOOS=darwin  GOARCH=arm64  CGO_ENABLED=0 go build $(LDFLAGS) -o dist/panelgen-darwin-arm64 .
	GOOS=windows GOARCH=amd64  CGO_ENABLED=0 go build $(LDFLAGS) -o dist/panelgen-windows-amd64.exe .
	@echo "Built:"
	@ls -lh dist/

## docker: build a minimal container image
docker:
	docker build -t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

## release-check: validate goreleaser config
release-check:
	goreleaser check

## release-snapshot: build release artifacts locally (no publish)
release-snapshot:
	goreleaser release --snapshot --clean

## clean: remove build artifacts
clean:
	rm -f panelgen
	rm -rf dist/
