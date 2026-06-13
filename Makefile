.PHONY: build test vet fmt check release-snapshot

BINARY ?= proofswe
VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/proofswe

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w ./cmd ./internal

check: fmt vet test
	go build ./...

release-snapshot:
	goreleaser release --snapshot --clean
