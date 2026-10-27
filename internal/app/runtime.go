package app

import (
	"context"
	"fmt"

	"github.com/sshdock/sshdock/internal/compose"
)

type runtimeTarget struct {
	model       App
	projectDir  string
	composePath string
}

func (s *Service) currentRuntimeTarget(ctx context.Context, appID string) (runtimeTarget, error) {
	model, err := s.store.GetApp(ctx, appID)
	if err != nil {
		return runtimeTarget{}, err
	}
	projectDir, composePath, err := CurrentComposeEntry(model)
	if err != nil {
		return runtimeTarget{}, err
	}
	return runtimeTarget{model: model, projectDir: projectDir, composePath: composePath}, nil
}

func CurrentComposeEntry(model App) (string, string, error) {
	projectDir := model.WorktreePath
	if projectDir == "" {
		return "", "", fmt.Errorf("resolve current Compose entry for app %q: worktree path is empty; redeploy current remote main", model.ID)
	}
	composePath, err := compose.DetectFile(projectDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve current Compose entry for app %q: %w", model.ID, err)
	}
	return projectDir, composePath, nil
}
