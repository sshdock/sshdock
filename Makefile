SHELL := /bin/bash

APP_NAME := rhumbase
DAEMON_NAME := rhumbased
GO_PACKAGES := ./...

.PHONY: setup fmt lint test smoke ci build clean check-tools

setup:
	go mod download
	go mod tidy

fmt:
	gofmt -w ./cmd ./internal ./test

lint:
	go vet $(GO_PACKAGES)

test:
	go test $(GO_PACKAGES)

smoke:
	go test ./test/harness -run TestSmoke -v

ci: check-tools fmt lint test smoke build

build:
	mkdir -p bin
	go build -o bin/$(APP_NAME) ./cmd/rhumbase
	go build -o bin/$(DAEMON_NAME) ./cmd/rhumbased

clean:
	rm -rf bin
	go clean -testcache

check-tools:
	@command -v go >/dev/null 2>&1 || (echo "go is required" && exit 1)

dev:
	go run ./cmd/rhumbase version

devd:
	go run ./cmd/rhumbased version
