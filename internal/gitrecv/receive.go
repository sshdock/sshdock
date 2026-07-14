package gitrecv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	domaincfg "github.com/sshdock/sshdock/internal/domain"
	"github.com/sshdock/sshdock/internal/store"
)

type receivePackStore interface {
	CreateApp(ctx context.Context, model app.App) error
	GetApp(ctx context.Context, id string) (app.App, error)
	ListApps(ctx context.Context) ([]app.App, error)
}

type ReceivePackRunner interface {
	RunReceivePack(ctx context.Context, repoPath string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error
}

type ReceivePackRequest struct {
	OriginalCommand string
	Stdin           io.Reader
	Stdout          io.Writer
	Stderr          io.Writer
}

type ReceivePackServiceConfig struct {
	Store             receivePackStore
	AppsDir           string
	NodeID            string
	RepoManager       *RepoManager
	ReceivePackRunner ReceivePackRunner
	Now               func() time.Time
}

type ReceivePackService struct {
	store             receivePackStore
	appsDir           string
	nodeID            string
	repoManager       *RepoManager
	receivePackRunner ReceivePackRunner
	now               func() time.Time
}

func NewReceivePackService(config ReceivePackServiceConfig) *ReceivePackService {
	nodeID := config.NodeID
	if nodeID == "" {
		nodeID = "local"
	}
	now := config.Now
	if now == nil {
		now = func() time.Time {
			return time.Now().UTC()
		}
	}
	repoManager := config.RepoManager
	if repoManager == nil {
		repoManager = NewRepoManager(RepoManagerConfig{AppsDir: config.AppsDir})
	}
	receivePackRunner := config.ReceivePackRunner
	if receivePackRunner == nil {
		receivePackRunner = LocalReceivePackRunner{}
	}

	return &ReceivePackService{
		store:             config.Store,
		appsDir:           config.AppsDir,
		nodeID:            nodeID,
		repoManager:       repoManager,
		receivePackRunner: receivePackRunner,
		now:               now,
	}
}

func ParseReceivePackCommand(originalCommand string) (string, error) {
	const command = "git-receive-pack"
	trimmed := strings.TrimSpace(originalCommand)
	if !strings.HasPrefix(trimmed, command) || len(trimmed) == len(command) || !strings.ContainsRune(" \t\r\n", rune(trimmed[len(command)])) {
		return "", fmt.Errorf("unsupported SSH command %q: expected git-receive-pack '<app>.git'", originalCommand)
	}

	repoPath := strings.TrimSpace(trimmed[len(command):])
	if len(repoPath) >= 2 {
		first := repoPath[0]
		last := repoPath[len(repoPath)-1]
		if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
			repoPath = repoPath[1 : len(repoPath)-1]
		}
	}
	if repoPath == "" || strings.ContainsAny(repoPath, `"'`) {
		return "", fmt.Errorf("unsupported SSH command %q: expected git-receive-pack '<app>.git'", originalCommand)
	}
	if !strings.HasSuffix(repoPath, ".git") {
		return "", fmt.Errorf("unsupported git path %q: expected <app>.git", repoPath)
	}

	appName := strings.TrimSuffix(repoPath, ".git")
	if err := validateFlatAppName(appName); err != nil {
		return "", err
	}

	return appName, nil
}

func (s *ReceivePackService) Receive(ctx context.Context, request ReceivePackRequest) error {
	if s.store == nil {
		return fmt.Errorf("receive-pack store is not configured")
	}
	if s.repoManager == nil {
		return fmt.Errorf("receive-pack repo manager is not configured")
	}
	if s.receivePackRunner == nil {
		return fmt.Errorf("receive-pack runner is not configured")
	}

	appName, err := ParseReceivePackCommand(request.OriginalCommand)
	if err != nil {
		return s.withAppNameGuidance(err)
	}

	model, err := s.store.GetApp(ctx, appName)
	if errors.Is(err, store.ErrNotFound) {
		if nameErr := domaincfg.ValidateAppName(appName); nameErr != nil {
			return s.withAppNameGuidance(nameErr)
		}
		existingApps, listErr := s.store.ListApps(ctx)
		if listErr != nil {
			return fmt.Errorf("list apps before creating %q: %w", appName, listErr)
		}
		existingNames := make([]string, 0, len(existingApps))
		for _, existingApp := range existingApps {
			existingNames = append(existingNames, existingApp.Name)
		}
		if isolationErr := domaincfg.ValidateAppIsolation(appName, existingNames); isolationErr != nil {
			return isolationErr
		}
		model, err = s.createApp(ctx, appName)
	}
	if err != nil {
		return err
	}
	if err := s.repoManager.InstallHooks(model.Name, model.RepoPath); err != nil {
		return fmt.Errorf("install receive hooks for %q: %w", model.Name, err)
	}

	return s.receivePackRunner.RunReceivePack(ctx, model.RepoPath, request.Stdin, request.Stdout, request.Stderr)
}

func (s *ReceivePackService) withAppNameGuidance(err error) error {
	var invalidName *domaincfg.InvalidAppNameError
	if !errors.As(err, &invalidName) {
		return err
	}
	return fmt.Errorf("%w\nrun: git remote set-url sshdock %s", err, s.repoManager.RemoteURL(invalidName.Suggestion))
}

func (s *ReceivePackService) createApp(ctx context.Context, appName string) (app.App, error) {
	repo, err := s.repoManager.SetupBareRepo(ctx, appName)
	if err != nil {
		return app.App{}, err
	}

	now := s.now()
	model := app.App{
		ID:           appName,
		Name:         appName,
		NodeID:       s.nodeID,
		RepoPath:     repo.Path,
		WorktreePath: filepath.Join(s.appsDir, appName, "worktree"),
		Status:       app.AppStatusCreated,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.store.CreateApp(ctx, model); err != nil {
		return app.App{}, err
	}

	return model, nil
}

func validateFlatAppName(appName string) error {
	if appName == "" || appName == "." || appName == ".." || strings.ContainsAny(appName, `/\`) {
		return domaincfg.ValidateAppName(appName)
	}

	return nil
}

type LocalReceivePackRunner struct{}

func (LocalReceivePackRunner) RunReceivePack(ctx context.Context, repoPath string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "git-receive-pack", repoPath)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git-receive-pack %s failed: %w", repoPath, err)
	}

	return nil
}
