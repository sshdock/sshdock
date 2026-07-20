SHELL := /bin/bash

APP_NAME := sshdock
DAEMON_NAME := sshdockd
GO_PACKAGES := ./...

.PHONY: setup fmt lint test smoke e2e e2e-docker public-examples-e2e software-recipes-e2e phoenix-liveview-e2e ssh-e2e bootstrap-e2e server-push-e2e route-e2e tui-e2e tui-actions-e2e tui-screenshots-real tui-screenshots-vps recovery-e2e hardening-e2e cli-lifecycle-e2e lifecycle-volume-e2e wildcard-domain-e2e config-e2e backup-restore-e2e ci build clean check-tools

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
	go test -count=1 -tags e2e ./test/e2e -run 'TestGit(HookEndToEnd|ReceivePushToCreateEndToEnd|ReceiveInvalidAppNameEndToEnd)' -v

e2e-docker:
	SSHDOCK_E2E_DOCKER=1 go test -count=1 -tags e2e ./test/e2e -run 'Test(GitHookDockerCompose|DockerRunnerComposeHealthSemantics|PublicExamplesEffectiveRoute|FrameworkQuickstartsDocker|WordPressSoftwareRecipeDocker|ServerPushBuildServiceDocker|DockerServiceCommands)EndToEnd' -v

public-examples-e2e:
	SSHDOCK_E2E_DOCKER=1 go test -count=1 -tags e2e ./test/e2e -run 'Test(PublicExamplesEffectiveRoute|FrameworkQuickstartsDocker|WordPressSoftwareRecipeDocker)EndToEnd' -v

software-recipes-e2e:
	SSHDOCK_E2E_DOCKER=1 go test -count=1 -tags e2e ./test/e2e -run TestWordPressSoftwareRecipeDockerEndToEnd -v

phoenix-liveview-e2e:
	@test -n "$(SSHDOCK_E2E_PHOENIX_URL)" || (echo "SSHDOCK_E2E_PHOENIX_URL is required" && exit 1)
	@test -n "$(SSHDOCK_E2E_PHOENIX_SSH_TARGET)" || (echo "SSHDOCK_E2E_PHOENIX_SSH_TARGET is required" && exit 1)
	@test -n "$(SSHDOCK_E2E_PHOENIX_SSH_KEY)" || (echo "SSHDOCK_E2E_PHOENIX_SSH_KEY is required" && exit 1)
	@command -v agent-browser >/dev/null 2>&1 || (echo "agent-browser is required" && exit 1)
	go test -count=1 -tags e2e ./test/e2e -run TestPhoenixLiveViewBrowserEndToEnd -v

ssh-e2e:
	go test -count=1 -tags e2e ./test/e2e -run 'Test(CLI|OpenSSH)' -v

bootstrap-e2e:
	go test -count=1 -tags e2e ./test/e2e -run TestBootstrap -v

server-push-e2e:
	go test -count=1 -tags e2e ./test/e2e -run TestServerPush -v

route-e2e:
	go test -count=1 -tags e2e ./test/e2e -run TestRouteThroughCaddy -v

tui-e2e:
	go test -count=1 -tags e2e ./test/e2e -run TestDashboardSSH -v

tui-actions-e2e:
	go test -count=1 -tags e2e ./test/e2e -run TestTUIActions -v

tui-screenshots-real:
	go test -count=1 -tags e2e ./test/e2e -run TestRealDashboardSSHScreenCapture -v

tui-screenshots-vps:
	go test -count=1 -tags e2e ./test/e2e -run TestExternalDashboardSSHScreenCapture -v

recovery-e2e:
	go test -count=1 -tags e2e ./test/e2e -run TestRecovery -v

hardening-e2e:
	go test -count=1 -tags e2e ./test/e2e -run TestHardening -v

cli-lifecycle-e2e:
	go test -count=1 -tags e2e ./test/e2e -run TestCLILifecycle -v

lifecycle-volume-e2e:
	SSHDOCK_E2E_DOCKER=1 go test -count=1 -tags e2e ./test/e2e -run TestDockerLifecyclePreservesExistingConfigAndNamedVolume -v

wildcard-domain-e2e:
	go test -count=1 -tags e2e ./test/e2e -run TestWildcardDomain -v

config-e2e:
	go test -count=1 ./test/harness -run TestConfigImportAndGitPushDeployUsesProcessEnvironment -v
	go test -count=1 ./cmd/sshdockd -run TestRunDashboardConfigCommandFeedsGitPushDeploy -v

backup-restore-e2e:
	go test -count=1 ./test/harness -run TestBackupRestoreKeepsEncryptedConfigUsable -v

ci: check-tools fmt lint test smoke build

build:
	mkdir -p bin
	go build -o bin/$(APP_NAME) ./cmd/sshdock
	go build -o bin/$(DAEMON_NAME) ./cmd/sshdockd

clean:
	rm -rf bin
	go clean -testcache

check-tools:
	@command -v go >/dev/null 2>&1 || (echo "go is required" && exit 1)

dev:
	go run ./cmd/sshdock version

devd:
	go run ./cmd/sshdockd version
