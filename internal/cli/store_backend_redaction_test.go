package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/compose"
)

func TestStoreBackendHistoryAndLogErrorsRedactStoredConfigValues(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	seedRecoveryApp(t, ctx, sqlite, appsDir, now)
	secret := "postgres://secret"
	configService := appconfig.NewService(sqlite, filepath.Join(t.TempDir(), "config.key"), appconfig.WithClock(func() time.Time { return now }))
	if err := configService.Set(ctx, appconfig.SetRequest{AppID: "my-app", Name: "DATABASE_URL", Value: []byte(secret)}); err != nil {
		t.Fatalf("Set config: %v", err)
	}
	if err := sqlite.CreateDeployment(ctx, app.Deployment{ID: "dep_secret", AppID: "my-app", ReleaseID: "rel_new", Status: app.DeploymentStatusFailed, StartedAt: now.Add(time.Minute), FinishedAt: now.Add(2 * time.Minute), ErrorMessage: "release failed for " + secret}); err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	if err := sqlite.CreateEvent(ctx, app.Event{ID: "evt_secret", AppID: "my-app", Type: "deploy.failed", Message: "event failed for " + secret, CreatedAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	runner := &compose.FakeRunner{LogsErr: errors.New("logs failed for " + secret)}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{AppsDir: appsDir, RecoveryRunner: runner, ConfigManager: configService})
	cliRunner := NewRunner(backend, "dev")

	for _, args := range [][]string{{"releases", "list", "my-app"}, {"events", "list", "my-app"}, {"logs", "my-app", "web"}} {
		// When
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		code := cliRunner.Run(args, &stdout, &stderr)

		// Then
		output := stdout.String() + stderr.String()
		if strings.Contains(output, secret) || !strings.Contains(output, "<redacted>") {
			t.Fatalf("%v code=%d output=%q", args, code, output)
		}
	}
}

func TestStoreBackendFollowLogsRedactsSecretSplitAcrossWrites(t *testing.T) {
	// Given
	ctx := context.Background()
	sqlite := newStoreBackendTestStore(t, ctx)
	appsDir := filepath.Join(t.TempDir(), "apps")
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	seedRecoveryApp(t, ctx, sqlite, appsDir, now)
	configService := appconfig.NewService(sqlite, filepath.Join(t.TempDir(), "config.key"), appconfig.WithClock(func() time.Time { return now }))
	if err := configService.Set(ctx, appconfig.SetRequest{AppID: "my-app", Name: "TOKEN", Value: []byte("split-secret")}); err != nil {
		t.Fatalf("Set config: %v", err)
	}
	runner := &chunkedLogRunner{FakeRunner: &compose.FakeRunner{}, chunks: []string{"before split-", "secret after\n"}}
	backend := NewStoreBackend(sqlite, StoreBackendConfig{AppsDir: appsDir, RecoveryRunner: runner, ConfigManager: configService})
	var stdout bytes.Buffer

	// When
	err := backend.Logs(LogRequest{AppName: "my-app", ServiceName: "web", Follow: true}, &stdout, io.Discard)

	// Then
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if strings.Contains(stdout.String(), "split-secret") || stdout.String() != "before <redacted> after\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

type chunkedLogRunner struct {
	*compose.FakeRunner
	chunks []string
	err    error
}

func (r *chunkedLogRunner) StreamLogs(_ context.Context, _ compose.LogsRequest, stdout io.Writer, _ io.Writer) error {
	for _, chunk := range r.chunks {
		if _, err := io.WriteString(stdout, chunk); err != nil {
			return err
		}
	}
	return r.err
}
