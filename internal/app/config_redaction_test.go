package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestServiceRedeployFailureRedactsAllKnownConfigValues(t *testing.T) {
	// Given
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	worktreePath := testComposeWorktree(t)
	secret := "legacy-scoped-secret"
	failure := errors.New("compose output contained " + secret)
	runner := &fakeRecoveryRunner{deployErr: failure}
	resolver := allValueConfigResolver{env: map[string]string{"DATABASE_URL": "flat"}, redactionValues: map[string]string{"app_1/worker/TOKEN": secret}}
	service := NewService(store, WithClock(func() time.Time { return now }), WithDeployRunner(runner), WithConfigResolver(resolver), withCurrentMain("new"))
	store.apps["app_1"] = App{ID: "app_1", Name: "app_1", WorktreePath: worktreePath, Status: AppStatusHealthy}
	store.releases["rel_good"] = Release{ID: "rel_good", AppID: "app_1", CommitSHA: "new", ComposePath: "/apps/app_1/worktree/compose.yml", Status: ReleaseStatusSucceeded, CreatedAt: now}

	// When
	_, err := service.RedeployCurrentMain(ctx, "app_1", "dep_config_redaction")

	// Then
	if !errors.Is(err, failure) || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "<redacted>") {
		t.Fatalf("redeploy error = %v", err)
	}
	stored := store.deployments["dep_config_redaction"]
	if strings.Contains(stored.FailureDetail, secret) || !strings.Contains(stored.FailureDetail, "<redacted>") {
		t.Fatalf("stored deployment = %#v", stored)
	}
	events := store.events["app_1"]
	if strings.Contains(events[len(events)-1].Message, secret) || !strings.Contains(events[len(events)-1].Message, "<redacted>") {
		t.Fatalf("failure event = %#v", events[len(events)-1])
	}
}

type allValueConfigResolver struct {
	env             map[string]string
	redactionValues map[string]string
}

func (r allValueConfigResolver) ResolveAppConfig(context.Context, string) (map[string]string, error) {
	return r.env, nil
}

func (r allValueConfigResolver) RedactionValues(context.Context, string) (map[string]string, error) {
	return r.redactionValues, nil
}
