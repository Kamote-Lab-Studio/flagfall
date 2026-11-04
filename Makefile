# Flagfall developer tasks. Windows users: run from Git Bash or WSL,
# or call the go/pnpm commands below directly.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.0.0-dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -s -w \
	-X github.com/Kamote-Lab-Studio/flagfall/internal/version.Version=$(VERSION) \
	-X github.com/Kamote-Lab-Studio/flagfall/internal/version.Commit=$(COMMIT)

.PHONY: dev build build-go test lint web-install web-build web-lint clean

## dev: run the server locally on :8080
dev:
	go run ./cmd/flagfall serve

## build: build the theme and admin front ends, then bin/flagfall with them embedded
build: web-build build-go

## build-go: build bin/flagfall only (pages show a build reminder if the theme was never built)
build-go:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/flagfall ./cmd/flagfall

## test: run Go and frontend tests
test:
	go test -race ./...
	pnpm -r --if-present test

## lint: vet Go code, check formatting, typecheck frontend
lint:
	go vet ./...
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed on:"; gofmt -l .; exit 1)
	pnpm -r --if-present lint

web-install:
	pnpm install --frozen-lockfile

web-build:
	pnpm -r --if-present build

clean:
	rm -rf bin dist
