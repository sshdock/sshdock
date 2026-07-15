package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	appmodel "github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/appconfig"
	"github.com/sshdock/sshdock/internal/compose"
	"github.com/sshdock/sshdock/internal/store"
)

type configManager interface {
	Set(ctx context.Context, request appconfig.SetRequest) error
	List(ctx context.Context, appID string) ([]appconfig.Entry, error)
	Reveal(ctx context.Context, ref appconfig.ConfigRef) (string, error)
	Unset(ctx context.Context, ref appconfig.ConfigRef) error
	ResolveAppConfig(ctx context.Context, appID string) (map[string]string, error)
	RedactionValues(ctx context.Context, appID string) (map[string]string, error)
}

type configMutationEventRequest struct {
	appName   string
	name      string
	eventType string
	message   string
}

func (b *StoreBackend) SetConfig(appName string, name string, value []byte) error {
	if b.configManager == nil {
		return fmt.Errorf("config manager is not configured")
	}
	ctx := context.Background()
	event, err := b.configMutationEvent(configMutationEventRequest{
		appName: appName, name: name,
		eventType: "config.set", message: "Config value set",
	})
	if err != nil {
		return err
	}
	if err := b.configManager.Set(ctx, appconfig.SetRequest{AppID: appName, Name: name, Value: value, MutatedBy: "dashboard"}); err != nil {
		return err
	}
	return b.recordConfigMutation(ctx, event)
}

func (b *StoreBackend) ImportConfig(appName string, input io.Reader) (int, error) {
	if b.configManager == nil {
		return 0, fmt.Errorf("config manager is not configured")
	}
	if input == nil {
		input = strings.NewReader("")
	}
	scanner := bufio.NewScanner(input)
	count := 0
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			return count, fmt.Errorf("config import line %d must be KEY=VALUE", lineNumber)
		}
		if err := b.SetConfig(appName, strings.TrimSpace(name), []byte(value)); err != nil {
			return count, err
		}
		count++
	}
	return count, scanner.Err()
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
	ctx := context.Background()
	event, err := b.configMutationEvent(configMutationEventRequest{
		appName: appName, name: name,
		eventType: "config.unset", message: "Config value unset",
	})
	if err != nil {
		return err
	}
	if err := b.configManager.Unset(ctx, appconfig.ConfigRef{AppID: appName, Name: name}); err != nil {
		return err
	}
	return b.recordConfigMutation(ctx, event)
}

func (b *StoreBackend) configMutationEvent(request configMutationEventRequest) (appmodel.Event, error) {
	operationID, err := b.newDeploymentID()
	if err != nil {
		return appmodel.Event{}, fmt.Errorf("create config mutation event ID: %w", err)
	}
	return appmodel.Event{
		ID: eventID(operationID, request.eventType), AppID: request.appName,
		Type: request.eventType, Message: request.message + " for " + request.name, CreatedAt: b.now(),
	}, nil
}

func (b *StoreBackend) recordConfigMutation(ctx context.Context, event appmodel.Event) error {
	if err := b.store.CreateEvent(ctx, event); err != nil {
		return fmt.Errorf("record %s event for app %q: %w", event.Type, event.AppID, err)
	}
	return nil
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

func (b *StoreBackend) configEnv(ctx context.Context, appName string, projectDir string) (map[string]string, error) {
	if b.configManager == nil {
		return nil, nil
	}
	env, err := b.configManager.ResolveAppConfig(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("resolve config for app %q: %w", appName, err)
	}
	return env, nil
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
	release, ok, err := b.latestRuntimeRelease(ctx, request.AppName)
	if err != nil {
		return fmt.Errorf("list releases for logs: %w", err)
	}
	if !ok || release.ComposePath == "" {
		return fmt.Errorf("no deployed release for app %q", request.AppName)
	}
	projectDir := projectDirFromModel(model, release)
	env, err := b.configEnv(ctx, request.AppName, projectDir)
	if err != nil {
		return err
	}
	redactionValues, err := b.configRedactionValues(ctx, request.AppName)
	if err != nil {
		return err
	}
	logsRequest := compose.LogsRequest{AppName: request.AppName, ProjectDir: projectDir, ComposePath: release.ComposePath, ServiceName: request.ServiceName, Lines: request.Lines, Follow: request.Follow, Env: env}
	if request.Follow {
		if streamer, ok := b.recoveryRunner.(logStreamer); ok {
			stdoutRedactor := newRedactingWriter(stdout, redactionValues)
			stderrRedactor := newRedactingWriter(stderr, redactionValues)
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

type redactingWriter struct {
	target  io.Writer
	values  map[string]string
	pending string
}

func newRedactingWriter(target io.Writer, values map[string]string) *redactingWriter {
	return &redactingWriter{target: target, values: values}
}

func (w *redactingWriter) Write(p []byte) (int, error) {
	combined := w.pending + string(p)
	pendingLength := pendingSecretPrefixLength(combined, w.values)
	emitLength := len(combined) - pendingLength
	w.pending = combined[emitLength:]
	if w.target == nil {
		return len(p), nil
	}
	_, err := io.WriteString(w.target, compose.RedactValues(combined[:emitLength], w.values))
	return len(p), err
}

func (w *redactingWriter) Flush() error {
	if w.target == nil || w.pending == "" {
		w.pending = ""
		return nil
	}
	_, err := io.WriteString(w.target, compose.RedactValues(w.pending, w.values))
	w.pending = ""
	return err
}

func pendingSecretPrefixLength(text string, values map[string]string) int {
	longest := 0
	for _, value := range values {
		limit := len(value) - 1
		if limit > len(text) {
			limit = len(text)
		}
		for length := limit; length > longest; length-- {
			if strings.HasSuffix(text, value[:length]) {
				longest = length
				break
			}
		}
	}
	return longest
}
