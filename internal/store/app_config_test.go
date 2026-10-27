package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
)

func configTestEvent(id, appID, operation string, now time.Time) app.Event {
	return app.Event{ID: id, AppID: appID, Type: "config." + operation, Message: "Config " + operation + " for TOKEN", CreatedAt: now}
}

func TestConfigMutationRollsBackWhenAuditCannotBeStored(t *testing.T) {
	ctx := context.Background()
	db := newTestStore(t, ctx)
	now := time.Now().UTC()
	if err := db.CreateApp(ctx, app.App{ID: "my-app", Name: "my-app", Status: app.AppStatusCreated, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	original := AppConfigValue{AppID: "my-app", Name: "TOKEN", Ciphertext: []byte("original"), Nonce: []byte("nonce"), KeyVersion: 1, CreatedAt: now, UpdatedAt: now}
	event := configTestEvent("duplicate", "my-app", "set", now)
	if err := db.SetAppConfigValue(ctx, original, event); err != nil {
		t.Fatal(err)
	}
	replacement := original
	replacement.Ciphertext = []byte("replacement")
	for _, mutation := range []struct {
		name string
		run  func() error
	}{
		{"replace", func() error { return db.SetAppConfigValue(ctx, replacement, event) }},
		{"insert", func() error {
			replacement.Name = "OTHER"
			return db.SetAppConfigValue(ctx, replacement, event)
		}},
		{"delete", func() error {
			return db.DeleteAppConfigValue(ctx, AppConfigRef{AppID: "my-app", Name: "TOKEN"}, configTestEvent(event.ID, "my-app", "unset", now))
		}},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			if err := mutation.run(); err == nil {
				t.Fatal("mutation accepted duplicate event ID")
			}
			got, err := db.GetAppConfigValue(ctx, AppConfigRef{AppID: "my-app", Name: "TOKEN"})
			if err != nil || !reflect.DeepEqual(got, original) {
				t.Fatalf("failed mutation changed value: %#v, %v", got, err)
			}
			if _, err := db.GetAppConfigValue(ctx, AppConfigRef{AppID: "my-app", Name: "OTHER"}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("failed mutation inserted value: %v", err)
			}
			events, err := db.ListEventsByApp(ctx, "my-app")
			if err != nil || len(events) != 1 || events[0] != event {
				t.Fatalf("failed mutation changed audit: %#v, %v", events, err)
			}
		})
	}
	if _, err := db.db.ExecContext(ctx, `create trigger reject_config before insert on app_config_values begin select raise(abort, 'config write rejected'); end`); err != nil {
		t.Fatal(err)
	}
	if err := db.SetAppConfigValue(ctx, replacement, configTestEvent("must_not_exist", "my-app", "set", now)); err == nil {
		t.Fatal("mutation accepted rejected config write")
	}
	events, err := db.ListEventsByApp(ctx, "my-app")
	if err != nil || len(events) != 1 {
		t.Fatalf("failed value write created audit: %#v, %v", events, err)
	}
}

func TestConfigMutationRequiresMatchingExistingApp(t *testing.T) {
	ctx := context.Background()
	db := newTestStore(t, ctx)
	now := time.Now().UTC()
	value := AppConfigValue{AppID: "missing", Name: "TOKEN", Ciphertext: []byte("value"), Nonce: []byte("nonce"), KeyVersion: 1, CreatedAt: now, UpdatedAt: now}
	if err := db.SetAppConfigValue(ctx, value, configTestEvent("set", value.AppID, "set", now)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("set missing app = %v", err)
	}
	if err := db.DeleteAppConfigValue(ctx, AppConfigRef{AppID: value.AppID, Name: value.Name}, configTestEvent("unset", value.AppID, "unset", now)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unset missing app = %v", err)
	}
	if err := db.CreateApp(ctx, app.App{ID: value.AppID, Name: value.AppID, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetAppConfigValue(ctx, value, configTestEvent("wrong-app", "other", "set", now)); err == nil {
		t.Fatal("accepted event for another app")
	}
	values, _ := db.ListAppConfigValues(ctx, value.AppID)
	events, _ := db.ListEventsByApp(ctx, value.AppID)
	if len(values) != 0 || len(events) != 0 {
		t.Fatalf("rejected mutation persisted value/audit: %#v, %#v", values, events)
	}
}

func TestConfigMutationAfterConcurrentAppDeletionCannotCreateOrphan(t *testing.T) {
	for _, operation := range []string{"set", "unset"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			path := filepath.Join(t.TempDir(), "sshdock.db")
			deleter, err := OpenSQLite(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer deleter.Close()
			mutator, err := OpenSQLite(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer mutator.Close()
			now := time.Now().UTC()
			if err := deleter.CreateApp(ctx, app.App{ID: "my-app", Name: "my-app", CreatedAt: now, UpdatedAt: now}); err != nil {
				t.Fatal(err)
			}
			value := AppConfigValue{AppID: "my-app", Name: "TOKEN", Ciphertext: []byte("value"), Nonce: []byte("nonce"), KeyVersion: 1, CreatedAt: now, UpdatedAt: now}
			if operation == "unset" {
				if err := mutator.SetAppConfigValue(ctx, value, configTestEvent("before", "my-app", "set", now)); err != nil {
					t.Fatal(err)
				}
			}
			tx, err := deleter.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			for _, statement := range []string{`delete from app_config_values where app_id = 'my-app'`, `delete from apps where id = 'my-app'`} {
				if _, err := tx.ExecContext(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			done := make(chan error, 1)
			go func() {
				event := configTestEvent("after", "my-app", operation, now)
				if operation == "set" {
					done <- mutator.SetAppConfigValue(ctx, value, event)
				} else {
					done <- mutator.DeleteAppConfigValue(ctx, AppConfigRef{AppID: value.AppID, Name: value.Name}, event)
				}
			}()
			select {
			case err := <-done:
				t.Fatalf("mutation bypassed uncommitted removal: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			if err := <-done; !errors.Is(err, ErrNotFound) {
				t.Fatalf("mutation after app removal = %v", err)
			}
			values, err := mutator.ListAppConfigValues(ctx, "my-app")
			if err != nil || len(values) != 0 {
				t.Fatalf("orphan config values = %#v, %v", values, err)
			}
			var count int
			if err := mutator.db.QueryRowContext(ctx, `select count(*) from events where id = 'after'`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("removed-app mutation audit count = %d, %v", count, err)
			}
		})
	}
}

func TestCurrentSchemaHistorySurvivesReopenUnchanged(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sshdock.db")
	db, err := OpenSQLite(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if db != nil {
			_ = db.Close()
		}
	}()
	now := time.Date(2026, 10, 27, 1, 0, 0, 0, time.UTC)
	model := app.App{ID: "my-app", Name: "my-app", NodeID: "local", RepoPath: "/apps/my-app/repo.git", WorktreePath: "/apps/my-app/worktree", Status: app.AppStatusHealthy, CreatedAt: now, UpdatedAt: now}
	release := app.Release{ID: "release", AppID: model.ID, CommitSHA: "fixed", ComposePath: "/apps/my-app/worktree/compose.yml", Status: app.ReleaseStatusSucceeded, CreatedAt: now, UpdatedAt: now}
	if err := db.CreateApp(ctx, model); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateRelease(ctx, release); err != nil {
		t.Fatal(err)
	}
	attempts := []app.Deployment{
		{ID: "push", AppID: model.ID, ReleaseID: release.ID, CommitSHA: release.CommitSHA, Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusSucceeded, StartedAt: now, FinishedAt: now},
		{ID: "redeploy", AppID: model.ID, ReleaseID: release.ID, CommitSHA: release.CommitSHA, Trigger: app.DeploymentTriggerRedeploy, Status: app.DeploymentStatusFailed, StartedAt: now.Add(time.Minute), FinishedAt: now.Add(time.Minute), FailureStage: "wait services", FailureDetail: "unhealthy", RetryGuidance: "redeploy current main", ErrorMessage: "unhealthy"},
	}
	for _, attempt := range attempts {
		if err := db.CreateDeployment(ctx, attempt); err != nil {
			t.Fatal(err)
		}
	}
	value := AppConfigValue{AppID: model.ID, Name: "TOKEN", Ciphertext: []byte("encrypted"), Nonce: []byte("nonce"), KeyVersion: 1, CreatedAt: now, UpdatedAt: now, MutatedBy: "sshdock"}
	event := configTestEvent("config", model.ID, "set", now)
	if err := db.SetAppConfigValue(ctx, value, event); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = OpenSQLite(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	gotApp, appErr := db.GetApp(ctx, model.ID)
	gotRelease, releaseErr := db.GetRelease(ctx, release.ID)
	gotAttempts, attemptsErr := db.ListDeploymentsByApp(ctx, model.ID)
	gotValue, valueErr := db.GetAppConfigValue(ctx, AppConfigRef{AppID: model.ID, Name: value.Name})
	gotEvents, eventsErr := db.ListEventsByApp(ctx, model.ID)
	if err := errors.Join(appErr, releaseErr, attemptsErr, valueErr, eventsErr); err != nil {
		t.Fatal(err)
	}
	if gotApp != model || gotRelease != release || !reflect.DeepEqual(gotAttempts, attempts) || !reflect.DeepEqual(gotValue, value) || !reflect.DeepEqual(gotEvents, []app.Event{event}) {
		t.Fatalf("history changed on reopen: app=%#v release=%#v attempts=%#v config=%#v events=%#v", gotApp, gotRelease, gotAttempts, gotValue, gotEvents)
	}
	var configCount, deploymentCount int
	if err := db.db.QueryRowContext(ctx, `select (select count(*) from app_config_values), (select count(*) from deployments)`).Scan(&configCount, &deploymentCount); err != nil || configCount != 1 || deploymentCount != 2 {
		t.Fatalf("row counts after reopen = %d, %d, %v", configCount, deploymentCount, err)
	}
}
