package gitrecv

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/compose"
)

func TestPostReceiveHandlerRedactsStoredValuesFromDeployFailures(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newHookTestStore(t, ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	secret := "stored-secret"
	configService := appconfig.NewService(sqlite, filepath.Join(t.TempDir(), "config.key"))
	if err := configService.Set(ctx, appconfig.SetRequest{AppID: "my-app", Name: "TOKEN", Value: []byte(secret)}); err != nil {
		t.Fatalf("Set config: %v", err)
	}
	runner := &compose.FakeRunner{DeployErr: errors.New("deploy output contained " + secret)}
	handler := NewPostReceiveHandler(PostReceiveHandlerConfig{
		Store:          sqlite,
		Runner:         runner,
		ConfigResolver: configService,
		Checkout: WorktreeCheckoutFunc(func(_ context.Context, _ string, worktreePath string, _ string) error {
			writeHookComposeFixture(t, worktreePath)
			return nil
		}),
		NewDeploymentID: func() (string, error) { return "dep_scoped_redaction", nil },
	})

	// When
	err := handler.Handle(ctx, "my-app", "/apps/my-app/repo.git", filepath.Join(t.TempDir(), "worktree"), strings.NewReader("old abc123 refs/heads/main\n"))

	// Then
	if err == nil || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "<redacted>") {
		t.Fatalf("Handle error = %v", err)
	}
	deployments, listErr := sqlite.ListDeploymentsByApp(ctx, "my-app")
	if listErr != nil {
		t.Fatalf("ListDeploymentsByApp: %v", listErr)
	}
	if strings.Contains(deployments[0].FailureDetail, secret) || !strings.Contains(deployments[0].FailureDetail, "<redacted>") {
		t.Fatalf("failure detail = %q", deployments[0].FailureDetail)
	}
}
