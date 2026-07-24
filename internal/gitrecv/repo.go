package gitrecv

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
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
	AppsDir   string
	GitHost   string
	Executor  GitCommandExecutor
	OwnerUser string
}

type RepoManager struct {
	appsDir     string
	gitHost     string
	executor    GitCommandExecutor
	ownerUser   string
	isRoot      func() bool
	lookupOwner func(string) (repoOwner, error)
	chown       func(string, repoOwner) error
}

type BareRepo struct {
	Path      string
	RemoteURL string
}

type repoOwner struct {
	uid int
	gid int
}

func NewRepoManager(config RepoManagerConfig) *RepoManager {
	gitHost := config.GitHost
	if gitHost == "" {
		gitHost = "server"
	}

	return &RepoManager{
		appsDir:     config.AppsDir,
		gitHost:     gitHost,
		executor:    config.Executor,
		ownerUser:   config.OwnerUser,
		isRoot:      func() bool { return os.Geteuid() == 0 },
		lookupOwner: lookupRepoOwner,
		chown:       chownRepoPath,
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

	if err := m.InstallHooks(appName, repoPath); err != nil {
		return BareRepo{}, err
	}
	if err := m.ensureRepoOwnership(repoPath); err != nil {
		return BareRepo{}, err
	}

	return BareRepo{
		Path:      repoPath,
		RemoteURL: m.RemoteURL(appName),
	}, nil
}

func (m *RepoManager) ensureRepoOwnership(repoPath string) error {
	if m.ownerUser == "" || !m.isRoot() {
		return nil
	}
	owner, err := m.lookupOwner(m.ownerUser)
	if err != nil {
		return fmt.Errorf("resolve Git receiver owner %q: %w", m.ownerUser, err)
	}
	appDir := filepath.Dir(repoPath)
	if err := filepath.WalkDir(appDir, func(path string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk repository path %q: %w", path, walkErr)
		}
		if err := m.chown(path, owner); err != nil {
			return fmt.Errorf("assign Git receiver ownership to %q for %q: %w", m.ownerUser, path, err)
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func lookupRepoOwner(name string) (repoOwner, error) {
	account, err := user.Lookup(name)
	if err != nil {
		return repoOwner{}, fmt.Errorf("look up user: %w", err)
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return repoOwner{}, fmt.Errorf("parse UID %q: %w", account.Uid, err)
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return repoOwner{}, fmt.Errorf("parse GID %q: %w", account.Gid, err)
	}
	return repoOwner{uid: uid, gid: gid}, nil
}

func chownRepoPath(path string, owner repoOwner) error {
	if err := os.Lchown(path, owner.uid, owner.gid); err != nil {
		return err
	}
	return nil
}

func (m *RepoManager) InstallHooks(appName string, repoPath string) error {
	if err := m.renderPreReceiveHook(repoPath); err != nil {
		return err
	}
	return m.renderPostReceiveHook(appName, repoPath)
}

func (m *RepoManager) renderPreReceiveHook(repoPath string) error {
	hookDir := filepath.Join(repoPath, "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		return err
	}

	const hook = `#!/bin/sh
set -eu
sshdockd git-pre-receive
`

	return writeExecutableHook(filepath.Join(hookDir, "pre-receive"), []byte(hook))
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

	return writeExecutableHook(filepath.Join(hookDir, "post-receive"), []byte(hook))
}

func writeExecutableHook(path string, contents []byte) (returnErr error) {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".sshdock-hook-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		if returnErr != nil {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(0o755); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return nil
}
