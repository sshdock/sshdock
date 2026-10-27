package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	appmodel "github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/store"
)

type configManager interface {
	Set(ctx context.Context, request appconfig.SetRequest) error
	Import(ctx context.Context, request appconfig.ImportRequest) (int, error)
	List(ctx context.Context, appID string) ([]appconfig.Entry, error)
	Reveal(ctx context.Context, ref appconfig.ConfigRef) (string, error)
	Unset(ctx context.Context, ref appconfig.ConfigRef) error
	ResolveEnv(ctx context.Context, appID string) (map[string]string, error)
	RedactionValues(ctx context.Context, appID string) (map[string]string, error)
}

func (b *StoreBackend) SetConfig(appName string, name string, value []byte) error {
	if b.configManager == nil {
		return fmt.Errorf("config manager is not configured")
	}
	return b.configManager.Set(context.Background(), appconfig.SetRequest{AppID: appName, Name: name, Value: value, MutatedBy: "sshdock"})
}

func (b *StoreBackend) ImportConfig(appName string, input io.Reader) (int, error) {
	if b.configManager == nil {
		return 0, fmt.Errorf("config manager is not configured")
	}
	return b.configManager.Import(context.Background(), appconfig.ImportRequest{AppID: appName, Input: input, MutatedBy: "sshdock"})
}

func (b *StoreBackend) ListConfig(appName string) ([]ConfigEntry, error) {
	if b.configManager == nil {
		return nil, fmt.Errorf("config manager is not configured")
	}
	entries, err := b.configManager.List(context.Background(), appName)
	if err != nil {
		return nil, err
	}
	result := make([]ConfigEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, ConfigEntry{Name: entry.Name, Status: entry.Status, RedactedValue: entry.RedactedValue, UpdatedAt: entry.UpdatedAt, MutatedBy: entry.MutatedBy})
	}
	return result, nil
}

func (b *StoreBackend) GetConfig(appName string, name string) (string, error) {
	if b.configManager == nil {
		return "", fmt.Errorf("config manager is not configured")
	}
	return b.configManager.Reveal(context.Background(), appconfig.ConfigRef{AppID: appName, Name: name})
}

func (b *StoreBackend) UnsetConfig(appName string, name string) error {
	if b.configManager == nil {
		return fmt.Errorf("config manager is not configured")
	}
	return b.configManager.Unset(context.Background(), appconfig.ConfigRef{AppID: appName, Name: name})
}

func (b *StoreBackend) configRedactionValues(ctx context.Context, appName string) (map[string]string, error) {
	if b.configManager == nil {
		return nil, nil
	}
	values, err := b.configManager.RedactionValues(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("load config values for redaction: %w", err)
	}
	return values, nil
}

func (b *StoreBackend) Logs(request LogRequest, stdout io.Writer, stderr io.Writer) error {
	ctx := context.Background()
	if b.recoveryRunner == nil {
		return fmt.Errorf("compose runner is not configured")
	}
	model, err := b.store.GetApp(ctx, request.AppName)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("app %q not found", request.AppName)
	}
	if err != nil {
		return fmt.Errorf("get app %q: %w", request.AppName, err)
	}
	projectDir, composePath, err := appmodel.CurrentComposeEntry(model)
	if err != nil {
		return fmt.Errorf("resolve current Compose entry for app %q: %w", request.AppName, err)
	}
	env, err := appmodel.ResolveRuntimeEnv(ctx, model, b.configManager)
	if err != nil {
		return err
	}
	redactionValues, err := b.configRedactionValues(ctx, request.AppName)
	if err != nil {
		return err
	}
	logsRequest := compose.LogsRequest{AppName: request.AppName, ProjectDir: projectDir, ComposePath: composePath, ServiceName: request.ServiceName, Lines: request.Lines, Follow: request.Follow, Env: env}
	if request.Follow {
		if streamer, ok := b.recoveryRunner.(logStreamer); ok {
			stdoutRedactor := compose.NewRedactingWriter(stdout, redactionValues)
			stderrRedactor := compose.NewRedactingWriter(stderr, redactionValues)
			streamErr := streamer.StreamLogs(ctx, logsRequest, stdoutRedactor, stderrRedactor)
			flushErr := errors.Join(stdoutRedactor.Flush(), stderrRedactor.Flush())
			if err := errors.Join(streamErr, flushErr); err != nil {
				return compose.RedactError(fmt.Errorf("stream logs for app %q: %w", request.AppName, err), redactionValues)
			}
			return nil
		}
	}
	output, err := b.recoveryRunner.Logs(ctx, logsRequest)
	if err != nil {
		return compose.RedactError(fmt.Errorf("load logs for app %q: %w", request.AppName, err), redactionValues)
	}
	_, err = fmt.Fprint(stdout, compose.RedactValues(output, redactionValues))
	return err
}
