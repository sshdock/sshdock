package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	appmodel "github.com/sshdock/sshdock/internal/app"
	"github.com/sshdock/sshdock/internal/compose"
)

type removalAudit struct {
	backend     *StoreBackend
	ctx         context.Context
	appName     string
	operationID string
	startedAt   time.Time
}

func (b *StoreBackend) beginRemovalAudit(ctx context.Context, appName string) (removalAudit, error) {
	now := b.now()
	audit := removalAudit{
		backend:     b,
		ctx:         ctx,
		appName:     appName,
		operationID: appName + "_" + strconv.FormatInt(now.UnixNano(), 10),
		startedAt:   now,
	}
	eventType := "remove.started"
	if err := b.store.CreateEvent(ctx, appmodel.Event{
		ID:        eventID(audit.operationID, eventType),
		AppID:     appName,
		Type:      eventType,
		Message:   "Remove started for app " + appName + "; Docker volumes will be preserved",
		CreatedAt: now,
	}); err != nil {
		return removalAudit{}, fmt.Errorf("record removal start for app %q: %w", appName, err)
	}
	return audit, nil
}

func (a removalAudit) fail(stage string, cause error) error {
	wrapped := fmt.Errorf("remove app %q at %s: %w", a.appName, stage, cause)
	eventType := "remove.failed"
	message := wrapped.Error() + "; fix the problem and retry with sudo sshdock apps remove " + a.appName + " --force"
	eventErr := a.backend.store.CreateEvent(a.ctx, appmodel.Event{
		ID:        eventID(a.operationID, eventType),
		AppID:     a.appName,
		Type:      eventType,
		Message:   message,
		CreatedAt: a.finishedAt(),
	})
	if eventErr != nil {
		return errors.Join(wrapped, fmt.Errorf("record removal failure for app %q: %w", a.appName, eventErr))
	}
	return wrapped
}

func (a removalAudit) succeed() error {
	eventType := "remove.succeeded"
	if err := a.backend.store.CreateEvent(a.ctx, appmodel.Event{
		ID:        eventID(a.operationID, eventType),
		AppID:     a.appName,
		Type:      eventType,
		Message:   "Remove succeeded for app " + a.appName + "; Docker volumes were preserved",
		CreatedAt: a.finishedAt(),
	}); err != nil {
		return fmt.Errorf("record removal success for app %q: %w", a.appName, err)
	}
	return nil
}

func (a removalAudit) finishedAt() time.Time {
	finishedAt := a.backend.now()
	if !finishedAt.After(a.startedAt) {
		return a.startedAt.Add(time.Second)
	}
	return finishedAt
}

func (b *StoreBackend) scrubRetainedEventMessages(ctx context.Context, appName string) error {
	redactionValues, err := b.configRedactionValues(ctx, appName)
	if err != nil {
		return err
	}
	if len(redactionValues) == 0 {
		return nil
	}
	events, err := b.store.ListEventsByApp(ctx, appName)
	if err != nil {
		return fmt.Errorf("list events for redaction: %w", err)
	}
	for _, event := range events {
		redacted := compose.RedactValues(event.Message, redactionValues)
		if redacted == event.Message {
			continue
		}
		if err := b.store.UpdateEventMessage(ctx, event.ID, redacted); err != nil {
			return fmt.Errorf("redact event %q: %w", event.ID, err)
		}
	}
	return nil
}
