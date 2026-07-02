SHELL := /bin/bash

APP_NAME := rhumbase
DAEMON_NAME := rhumbased
GO_PACKAGES := ./...

.PHONY: setup fmt lint test smoke e2e e2e-docker ssh-e2e bootstrap-e2e server-push-e2e route-e2e ci build clean check-tools

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

e2e:
	go test -count=1 -tags e2e ./test/e2e -run 'TestGit(HookEndToEnd|ReceivePushToCreateEndToEnd)' -v

e2e-docker:
	RHUMBASE_E2E_DOCKER=1 go test -count=1 -tags e2e ./test/e2e -run TestGitHookDockerComposeEndToEnd -v

ssh-e2e:
	go test -count=1 -tags e2e ./test/e2e -run 'Test(CLI|OpenSSH)' -v

bootstrap-e2e:
	go test -count=1 -tags e2e ./test/e2e -run TestBootstrap -v

server-push-e2e:
	go test -count=1 -tags e2e ./test/e2e -run TestServerPush -v

route-e2e:
	go test -count=1 -tags e2e ./test/e2e -run TestRouteThroughCaddy -v

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
