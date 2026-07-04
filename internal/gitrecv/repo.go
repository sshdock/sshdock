package gitrecv

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type GitCommand struct {
	Name string
	Args []string
	Dir  string
}

type GitCommandExecutor interface {
	Run(ctx context.Context, command GitCommand) error
}

type RepoManagerConfig struct {
	AppsDir  string
	GitHost  string
	Executor GitCommandExecutor
}

type RepoManager struct {
	appsDir  string
	gitHost  string
	executor GitCommandExecutor
}

type BareRepo struct {
	Path      string
	RemoteURL string
}

func NewRepoManager(config RepoManagerConfig) *RepoManager {
	gitHost := config.GitHost
	if gitHost == "" {
		gitHost = "server"
	}

	return &RepoManager{
		appsDir:  config.AppsDir,
		gitHost:  gitHost,
		executor: config.Executor,
	}
}

func (m *RepoManager) SetupBareRepo(ctx context.Context, appName string) (BareRepo, error) {
	repoPath := BareRepoPath(m.appsDir, appName)
	if err := os.MkdirAll(filepath.Dir(repoPath), 0o755); err != nil {
		return BareRepo{}, err
	}

	if m.executor != nil {
		if err := m.executor.Run(ctx, GitCommand{Name: "git", Args: []string{"init", "--bare", repoPath}}); err != nil {
			return BareRepo{}, err
		}
	}

	if err := m.renderPostReceiveHook(appName, repoPath); err != nil {
		return BareRepo{}, err
	}

	return BareRepo{
		Path:      repoPath,
		RemoteURL: m.RemoteURL(appName),
	}, nil
}

func (m *RepoManager) RemoteURL(appName string) string {
	return fmt.Sprintf("git@%s:%s.git", m.gitHost, appName)
}

func (m *RepoManager) renderPostReceiveHook(appName string, repoPath string) error {
	hookDir := filepath.Join(repoPath, "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		return err
	}

	hook := fmt.Sprintf(`#!/bin/sh
set -eu
sshdockd git-hook --app %q --repo %q
`, appName, repoPath)

	return os.WriteFile(filepath.Join(hookDir, "post-receive"), []byte(hook), 0o755)
}
