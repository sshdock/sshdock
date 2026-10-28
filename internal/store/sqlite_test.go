package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
)

func TestSQLiteStoreApps(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	model := app.App{
		ID:           "app_1",
		Name:         "my-app",
		NodeID:       "local",
		RepoPath:     "/data/apps/my-app/repo.git",
		WorktreePath: "/data/apps/my-app/worktree",
		ComposePath:  "compose.yml",
		Status:       app.AppStatusCreated,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := store.CreateApp(ctx, model); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	got, err := store.GetApp(ctx, model.ID)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if got != model {
		t.Fatalf("GetApp = %#v, want %#v", got, model)
	}

	apps, err := store.ListApps(ctx)
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(apps) != 1 || apps[0] != model {
		t.Fatalf("ListApps = %#v, want [%#v]", apps, model)
	}

	updatedAt := now.Add(time.Minute)
	if err := store.UpdateAppStatus(ctx, model.ID, app.AppStatusDeploying, updatedAt); err != nil {
		t.Fatalf("UpdateAppStatus: %v", err)
	}
	got, err = store.GetApp(ctx, model.ID)
	if err != nil {
		t.Fatalf("GetApp after status update: %v", err)
	}
	if got.Status != app.AppStatusDeploying {
		t.Fatalf("status after update = %q", got.Status)
	}
	if !got.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("updated_at after update = %s, want %s", got.UpdatedAt, updatedAt)
	}
}

func TestSQLiteStoreReleases(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	release := app.Release{
		ID:          "rel_1",
		AppID:       "app_1",
		CommitSHA:   "abc123",
		ComposePath: "compose.yml",
		Status:      app.ReleaseStatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := store.CreateRelease(ctx, release); err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}

	got, err := store.GetRelease(ctx, release.ID)
	if err != nil {
		t.Fatalf("GetRelease: %v", err)
	}
	if got != release {
		t.Fatalf("GetRelease = %#v, want %#v", got, release)
	}

	releases, err := store.ListReleasesByApp(ctx, release.AppID)
	if err != nil {
		t.Fatalf("ListReleasesByApp: %v", err)
	}
	if len(releases) != 1 || releases[0] != release {
		t.Fatalf("ListReleasesByApp = %#v, want [%#v]", releases, release)
	}

	updatedAt := now.Add(time.Minute)
	if err := store.UpdateReleaseStatus(ctx, release.ID, app.ReleaseStatusDeploying, updatedAt); err != nil {
		t.Fatalf("UpdateReleaseStatus: %v", err)
	}
	got, err = store.GetRelease(ctx, release.ID)
	if err != nil {
		t.Fatalf("GetRelease after status update: %v", err)
	}
	if got.Status != app.ReleaseStatusDeploying {
		t.Fatalf("status after update = %q", got.Status)
	}
	if !got.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("updated_at after update = %s, want %s", got.UpdatedAt, updatedAt)
	}
}

func TestSQLiteStoreReleaseIdentityIsStablePerAppCommit(t *testing.T) {
	// Given
	ctx := context.Background()
	store := newTestStore(t, ctx)
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	commit := "1234567890abcdef1234567890abcdef12345678"
	first := app.Release{
		ID:          app.ReleaseID("first-app", commit),
		AppID:       "first-app",
		CommitSHA:   commit,
		ComposePath: "compose.yml",
		Status:      app.ReleaseStatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	second := first
	second.ID = app.ReleaseID("second-app", commit)
	second.AppID = "second-app"

	// When
	if err := store.CreateRelease(ctx, first); err != nil {
		t.Fatalf("CreateRelease first: %v", err)
	}
	if err := store.CreateRelease(ctx, second); err != nil {
		t.Fatalf("CreateRelease second: %v", err)
	}
	got, err := store.GetReleaseByAppCommit(ctx, first.AppID, commit)

	// Then
	if err != nil {
		t.Fatalf("GetReleaseByAppCommit: %v", err)
	}
	if got != first {
		t.Fatalf("release = %#v, want %#v", got, first)
	}
	duplicate := first
	duplicate.ID = "rel_duplicate"
	if err := store.CreateRelease(ctx, duplicate); err == nil {
		t.Fatal("CreateRelease duplicate app/commit error = nil")
	}
}

func TestSQLiteStoreDeploymentStatus(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)
	startedAt := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(time.Minute)
	deployment := app.Deployment{
		ID:        "dep_1",
		AppID:     "app_1",
		ReleaseID: "rel_1",
		Status:    app.DeploymentStatusDeploying,
		StartedAt: startedAt,
	}

	if err := store.CreateDeployment(ctx, deployment); err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	failureDetail := "stage=build services; detail=compose failed; fix=fix the Dockerfile"
	deployment.FinishedAt = finishedAt
	deployment.FailureStage = "build services"
	deployment.FailureDetail = failureDetail
	deployment.RetryGuidance = "push the same commit again"
	if err := store.UpdateDeploymentFailure(ctx, deployment); err != nil {
		t.Fatalf("UpdateDeploymentFailure: %v", err)
	}

	var status string
	var finishedAtText string
	var failureStage string
	var storedFailureDetail string
	var retryGuidance string
	var errorMessage string
	err := store.db.QueryRowContext(ctx, `select status, finished_at, failure_stage, failure_detail, retry_guidance, error_message from deployments where id = ?`, deployment.ID).
		Scan(&status, &finishedAtText, &failureStage, &storedFailureDetail, &retryGuidance, &errorMessage)
	if err != nil {
		t.Fatalf("query deployment: %v", err)
	}
	if status != string(app.DeploymentStatusFailed) {
		t.Fatalf("status = %q", status)
	}
	if finishedAtText != formatTime(finishedAt) {
		t.Fatalf("finished_at = %q", finishedAtText)
	}
	if failureStage != "build services" || storedFailureDetail != failureDetail || retryGuidance != "push the same commit again" {
		t.Fatalf("failure metadata = %q, %q, %q", failureStage, storedFailureDetail, retryGuidance)
	}
	if errorMessage != storedFailureDetail {
		t.Fatalf("error_message = %q", errorMessage)
	}

	second := app.Deployment{
		ID:        "dep_2",
		AppID:     "app_1",
		ReleaseID: "rel_2",
		Status:    app.DeploymentStatusSucceeded,
		StartedAt: startedAt.Add(2 * time.Minute),
	}
	if err := store.CreateDeployment(ctx, second); err != nil {
		t.Fatalf("CreateDeployment second: %v", err)
	}
	otherApp := app.Deployment{
		ID:        "dep_other",
		AppID:     "app_2",
		ReleaseID: "rel_other",
		Status:    app.DeploymentStatusSucceeded,
		StartedAt: startedAt.Add(3 * time.Minute),
	}
	if err := store.CreateDeployment(ctx, otherApp); err != nil {
		t.Fatalf("CreateDeployment other app: %v", err)
	}

	deployments, err := store.ListDeploymentsByApp(ctx, "app_1")
	if err != nil {
		t.Fatalf("ListDeploymentsByApp: %v", err)
	}
	if len(deployments) != 2 {
		t.Fatalf("deployments = %#v, want two app_1 rows", deployments)
	}
	if deployments[0].ID != "dep_1" || deployments[0].Status != app.DeploymentStatusFailed || deployments[0].FailureStage != "build services" || deployments[0].FailureDetail != storedFailureDetail || deployments[0].RetryGuidance != retryGuidance {
		t.Fatalf("first deployment = %#v", deployments[0])
	}
	if deployments[1] != second {
		t.Fatalf("second deployment = %#v, want %#v", deployments[1], second)
	}
}

func TestSQLiteStoreFailedAttemptDoesNotDemoteSucceededRelease(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	good := app.Release{ID: "rel_good", AppID: "app_1", CommitSHA: "good", Status: app.ReleaseStatusSucceeded, CreatedAt: now, UpdatedAt: now}
	deploying := app.Release{ID: "rel_deploying", AppID: "app_1", CommitSHA: "deploying", Status: app.ReleaseStatusDeploying, CreatedAt: now, UpdatedAt: now}
	failed := app.Release{ID: "rel_failed", AppID: "app_1", CommitSHA: "failed", Status: app.ReleaseStatusFailed, CreatedAt: now, UpdatedAt: now}
	for _, release := range []app.Release{good, deploying, failed} {
		if err := store.CreateRelease(ctx, release); err != nil {
			t.Fatalf("CreateRelease %s: %v", release.ID, err)
		}
	}

	if err := store.MarkReleaseFailedUnlessSucceeded(ctx, good.ID, now.Add(time.Minute)); err != nil {
		t.Fatalf("MarkReleaseFailedUnlessSucceeded good: %v", err)
	}
	if err := store.MarkReleaseFailedUnlessSucceeded(ctx, deploying.ID, now.Add(time.Minute)); err != nil {
		t.Fatalf("MarkReleaseFailedUnlessSucceeded deploying: %v", err)
	}
	if err := store.MarkReleaseDeployingUnlessSucceeded(ctx, good.ID, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("MarkReleaseDeployingUnlessSucceeded good: %v", err)
	}
	if err := store.MarkReleaseDeployingUnlessSucceeded(ctx, failed.ID, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("MarkReleaseDeployingUnlessSucceeded failed: %v", err)
	}

	gotGood, err := store.GetRelease(ctx, good.ID)
	if err != nil {
		t.Fatalf("GetRelease good: %v", err)
	}
	gotDeploying, err := store.GetRelease(ctx, deploying.ID)
	if err != nil {
		t.Fatalf("GetRelease deploying: %v", err)
	}
	gotFailed, err := store.GetRelease(ctx, failed.ID)
	if err != nil {
		t.Fatalf("GetRelease failed: %v", err)
	}
	if gotGood.Status != app.ReleaseStatusSucceeded {
		t.Fatalf("good release status = %q", gotGood.Status)
	}
	if gotDeploying.Status != app.ReleaseStatusFailed {
		t.Fatalf("deploying release status = %q", gotDeploying.Status)
	}
	if gotFailed.Status != app.ReleaseStatusDeploying {
		t.Fatalf("failed release status = %q", gotFailed.Status)
	}
}

func TestSQLiteStoreDeploymentAttemptMetadata(t *testing.T) {
	// Given
	ctx := context.Background()
	store := newTestStore(t, ctx)
	startedAt := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(time.Minute)
	deployment := app.Deployment{
		ID:            "dep_0123456789abcdef0123456789abcdef",
		AppID:         "my-app",
		ReleaseID:     "rel_my-app_abcdef",
		CommitSHA:     "abcdef",
		Trigger:       app.DeploymentTriggerPush,
		Status:        app.DeploymentStatusFailed,
		StartedAt:     startedAt,
		FinishedAt:    finishedAt,
		FailureStage:  "build services",
		FailureDetail: "Dockerfile line 7 failed",
		RetryGuidance: "push the same commit after fixing the Dockerfile",
	}

	// When
	if err := store.CreateDeployment(ctx, deployment); err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	deployments, err := store.ListDeploymentsByApp(ctx, deployment.AppID)

	// Then
	if err != nil {
		t.Fatalf("ListDeploymentsByApp: %v", err)
	}
	if len(deployments) != 1 || deployments[0] != deployment {
		t.Fatalf("deployments = %#v, want [%#v]", deployments, deployment)
	}
}

func TestSQLiteStoreQueueDeploymentRejectsSecondActiveAttempt(t *testing.T) {
	// Given
	ctx := context.Background()
	store := newTestStore(t, ctx)
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	if err := store.CreateApp(ctx, app.App{ID: "app_1", Name: "app_1", NodeID: "local", Status: app.AppStatusHealthy, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	first := app.Deployment{ID: "dep_first", AppID: "app_1", ReleaseID: "rel_first", CommitSHA: "first", Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusPending, StartedAt: now}
	second := app.Deployment{ID: "dep_second", AppID: "app_1", ReleaseID: "rel_second", CommitSHA: "second", Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusPending, StartedAt: now.Add(time.Minute)}

	// When
	if err := store.QueueDeployment(ctx, first, "initial"); err != nil {
		t.Fatalf("QueueDeployment first: %v", err)
	}
	err := store.QueueDeployment(ctx, second, first.CommitSHA)

	// Then
	if !errors.Is(err, ErrActiveDeployment) {
		t.Fatalf("QueueDeployment second error = %v, want ErrActiveDeployment", err)
	}
	deployments, err := store.ListDeploymentsByApp(ctx, "app_1")
	if err != nil {
		t.Fatalf("ListDeploymentsByApp: %v", err)
	}
	if len(deployments) != 1 || deployments[0] != first {
		t.Fatalf("deployments = %#v, want [%#v]", deployments, first)
	}
}

func TestSQLiteStoreQueueDeploymentReplacesUnacceptedReservation(t *testing.T) {
	// Given
	ctx := context.Background()
	store := newTestStore(t, ctx)
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	if err := store.CreateApp(ctx, app.App{ID: "app_1", Name: "app_1", NodeID: "local", Status: app.AppStatusCreated, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	stale := app.Deployment{ID: "dep_stale", AppID: "app_1", ReleaseID: "rel_stale", CommitSHA: "rejected", Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusPending, StartedAt: now}
	if err := store.QueueDeployment(ctx, stale, "initial"); err != nil {
		t.Fatalf("QueueDeployment stale: %v", err)
	}
	fresh := app.Deployment{ID: "dep_fresh", AppID: "app_1", ReleaseID: "rel_fresh", CommitSHA: "accepted", Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusPending, StartedAt: now.Add(time.Minute)}

	// When: the next pre-receive observes that remote main never moved to the stale SHA.
	err := store.QueueDeployment(ctx, fresh, "initial")

	// Then
	if err != nil {
		t.Fatalf("QueueDeployment fresh: %v", err)
	}
	deployments, err := store.ListDeploymentsByApp(ctx, fresh.AppID)
	if err != nil {
		t.Fatalf("ListDeploymentsByApp: %v", err)
	}
	if len(deployments) != 1 || deployments[0] != fresh {
		t.Fatalf("deployments = %#v, want [%#v]", deployments, fresh)
	}
}

func TestSQLiteStoreClaimNextPendingDeploymentMarksOnlyOneAttemptDeploying(t *testing.T) {
	// Given
	ctx := context.Background()
	store := newTestStore(t, ctx)
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	for _, appID := range []string{"app_1", "app_2"} {
		if err := store.CreateApp(ctx, app.App{ID: appID, Name: appID, NodeID: "local", Status: app.AppStatusCreated, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("CreateApp(%s): %v", appID, err)
		}
	}
	first := app.Deployment{ID: "dep_first", AppID: "app_1", ReleaseID: "rel_first", CommitSHA: "first", Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusPending, StartedAt: now}
	second := app.Deployment{ID: "dep_second", AppID: "app_2", ReleaseID: "rel_second", CommitSHA: "second", Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusPending, StartedAt: now.Add(time.Nanosecond)}
	for _, deployment := range []app.Deployment{first, second} {
		if err := store.QueueDeployment(ctx, deployment, ""); err != nil {
			t.Fatalf("QueueDeployment(%s): %v", deployment.ID, err)
		}
	}

	// When: pre-receive has reserved the attempt, but Git has not accepted it yet.
	_, found, err := store.ClaimNextPendingDeployment(ctx)
	if err != nil {
		t.Fatalf("ClaimNextPendingDeployment before acceptance: %v", err)
	}
	if found {
		t.Fatal("ClaimNextPendingDeployment found a pre-receive reservation before Git acceptance")
	}
	if err := store.CreateEvent(ctx, app.Event{ID: "evt_dep_first_git_ref_accepted", AppID: first.AppID, Type: "git.ref_accepted", Message: "Remote main accepted", CreatedAt: now}); err != nil {
		t.Fatalf("CreateEvent accepted: %v", err)
	}
	_, found, err = store.ClaimNextPendingDeployment(ctx)
	if err != nil {
		t.Fatalf("ClaimNextPendingDeployment after acceptance: %v", err)
	}
	if found {
		t.Fatal("ClaimNextPendingDeployment found an accepted deployment before its queue transition")
	}
	if err := store.CreateEvent(ctx, app.Event{ID: "evt_dep_first_queued", AppID: first.AppID, Type: "deploy.queued", Message: "Deploy queued", CreatedAt: now}); err != nil {
		t.Fatalf("CreateEvent queued: %v", err)
	}
	if err := store.CreateEvent(ctx, app.Event{ID: "evt_dep_second_queued", AppID: second.AppID, Type: "deploy.queued", CreatedAt: second.StartedAt}); err != nil {
		t.Fatal(err)
	}
	claimed, found, err := store.ClaimNextPendingDeployment(ctx)

	// Then: post-receive acceptance makes exactly one attempt runnable.
	if err != nil {
		t.Fatalf("ClaimNextPendingDeployment: %v", err)
	}
	if !found {
		t.Fatal("ClaimNextPendingDeployment found = false, want true")
	}
	if claimed.ID != first.ID || claimed.Status != app.DeploymentStatusDeploying {
		t.Fatalf("claimed deployment = %#v, want first deployment marked deploying", claimed)
	}
	firstDeployments, err := store.ListDeploymentsByApp(ctx, first.AppID)
	if err != nil {
		t.Fatalf("ListDeploymentsByApp(%s): %v", first.AppID, err)
	}
	if len(firstDeployments) != 1 || firstDeployments[0].Status != app.DeploymentStatusDeploying || !firstDeployments[0].FinishedAt.IsZero() {
		t.Fatalf("first app deployments = %#v, want one deploying attempt without a finish time", firstDeployments)
	}
	deployments, err := store.ListDeploymentsByApp(ctx, second.AppID)
	if err != nil {
		t.Fatalf("ListDeploymentsByApp: %v", err)
	}
	if len(deployments) != 1 || deployments[0] != second {
		t.Fatalf("second app deployments = %#v, want [%#v]", deployments, second)
	}
}

func TestOpenSQLiteConfiguresBusyTimeoutForConcurrentProcesses(t *testing.T) {
	// Given a store opened through the production SQLite boundary.
	ctx := context.Background()
	store, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	// When the connection's lock-wait policy is inspected.
	var timeoutMilliseconds int
	if err := store.db.QueryRowContext(ctx, "pragma busy_timeout").Scan(&timeoutMilliseconds); err != nil {
		t.Fatalf("query busy_timeout: %v", err)
	}

	// Then concurrent CLI processes get a bounded wait instead of immediate SQLITE_BUSY.
	if timeoutMilliseconds != 5000 {
		t.Fatalf("busy_timeout = %dms, want 5000ms", timeoutMilliseconds)
	}
}

func TestSQLiteStoreDomains(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	if err := store.CreateApp(ctx, app.App{ID: "app_1", Name: "app_1", Status: app.AppStatusCreated, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	domain := app.Domain{
		ID:          "dom_1",
		AppID:       "app_1",
		ServiceName: "web",
		DomainName:  "example.com",
		Port:        3000,
		HTTPS:       true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := store.AttachDomain(ctx, domain); err != nil {
		t.Fatalf("AttachDomain: %v", err)
	}

	domains, err := store.ListDomainsByApp(ctx, domain.AppID)
	if err != nil {
		t.Fatalf("ListDomainsByApp: %v", err)
	}
	if len(domains) != 1 || domains[0] != domain {
		t.Fatalf("ListDomainsByApp = %#v, want [%#v]", domains, domain)
	}

	if err := store.CreateApp(ctx, app.App{ID: "app_2", Name: "app_2", Status: app.AppStatusCreated, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	secondDomain := app.Domain{
		ID:          "dom_2",
		AppID:       "app_2",
		ServiceName: "api",
		DomainName:  "api.example.com",
		Port:        4000,
		HTTPS:       true,
		CreatedAt:   now.Add(time.Minute),
		UpdatedAt:   now.Add(time.Minute),
	}
	if err := store.AttachDomain(ctx, secondDomain); err != nil {
		t.Fatalf("AttachDomain second: %v", err)
	}

	allDomains, err := store.ListDomains(ctx)
	if err != nil {
		t.Fatalf("ListDomains: %v", err)
	}
	wantDomains := []app.Domain{domain, secondDomain}
	if len(allDomains) != len(wantDomains) {
		t.Fatalf("ListDomains len = %d, want %d: %#v", len(allDomains), len(wantDomains), allDomains)
	}
	for i := range wantDomains {
		if allDomains[i] != wantDomains[i] {
			t.Fatalf("ListDomains[%d] = %#v, want %#v", i, allDomains[i], wantDomains[i])
		}
	}

	deleted, err := store.DeleteDomainByAppAndName(ctx, domain.AppID, domain.DomainName)
	if err != nil {
		t.Fatalf("DeleteDomainByAppAndName: %v", err)
	}
	if deleted != domain {
		t.Fatalf("deleted domain = %#v, want %#v", deleted, domain)
	}
	domains, err = store.ListDomainsByApp(ctx, domain.AppID)
	if err != nil {
		t.Fatalf("ListDomainsByApp after delete: %v", err)
	}
	if len(domains) != 0 {
		t.Fatalf("domains after delete = %#v, want none", domains)
	}
	if _, err := store.DeleteDomainByAppAndName(ctx, domain.AppID, domain.DomainName); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteDomainByAppAndName missing error = %v, want ErrNotFound", err)
	}
}

func TestSQLiteStoreAttachDomainUpsertsByDomainID(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)
	createdAt := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	if err := store.CreateApp(ctx, app.App{ID: "my-app", Name: "my-app", Status: app.AppStatusCreated, CreatedAt: createdAt, UpdatedAt: createdAt}); err != nil {
		t.Fatal(err)
	}

	domain := app.Domain{
		ID:          "dom_my_app_my_app_example_com",
		AppID:       "my-app",
		ServiceName: "web",
		DomainName:  "my-app.example.com",
		Port:        3000,
		HTTPS:       true,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}
	if err := store.AttachDomain(ctx, domain); err != nil {
		t.Fatalf("AttachDomain initial: %v", err)
	}

	domain.ServiceName = "api"
	domain.Port = 4000
	domain.CreatedAt = updatedAt
	domain.UpdatedAt = updatedAt
	if err := store.AttachDomain(ctx, domain); err != nil {
		t.Fatalf("AttachDomain update: %v", err)
	}

	domains, err := store.ListDomainsByApp(ctx, domain.AppID)
	if err != nil {
		t.Fatalf("ListDomainsByApp: %v", err)
	}
	if len(domains) != 1 {
		t.Fatalf("domains len = %d, want 1: %#v", len(domains), domains)
	}
	got := domains[0]
	if got.ServiceName != "api" || got.Port != 4000 {
		t.Fatalf("updated domain = %#v, want api:4000", got)
	}
	if !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt = %s, want original %s", got.CreatedAt, createdAt)
	}
	if !got.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("UpdatedAt = %s, want %s", got.UpdatedAt, updatedAt)
	}
}

func TestSQLiteStoreEvents(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	event := app.Event{
		ID:        "evt_1",
		AppID:     "app_1",
		Type:      "app.created",
		Message:   "App created",
		CreatedAt: now,
	}

	if err := store.CreateEvent(ctx, event); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}

	events, err := store.ListEventsByApp(ctx, event.AppID)
	if err != nil {
		t.Fatalf("ListEventsByApp: %v", err)
	}
	if len(events) != 1 || events[0] != event {
		t.Fatalf("ListEventsByApp = %#v, want [%#v]", events, event)
	}
}

func TestSQLiteStoreServerConfig(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)
	updatedAt := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)

	if _, err := store.GetServerConfig(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetServerConfig error = %v, want ErrNotFound", err)
	}
	if err := store.SetServerConfig(ctx, ServerConfig{BaseDomain: "example.com", GitHost: "sshdock.example.com", UpdatedAt: updatedAt}); err != nil {
		t.Fatalf("SetServerConfig: %v", err)
	}

	got, err := store.GetServerConfig(ctx)
	if err != nil {
		t.Fatalf("GetServerConfig: %v", err)
	}
	want := ServerConfig{BaseDomain: "example.com", GitHost: "sshdock.example.com", UpdatedAt: updatedAt}
	if got != want {
		t.Fatalf("server config = %#v, want %#v", got, want)
	}
}

func TestSQLiteStoreServerConfigKeepsLegacyGitHost(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)
	updatedAt := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)

	if err := store.SetServerConfig(ctx, ServerConfig{GitHost: "sshdock.example.com", UpdatedAt: updatedAt}); err != nil {
		t.Fatalf("SetServerConfig: %v", err)
	}

	got, err := store.GetServerConfig(ctx)
	if err != nil {
		t.Fatalf("GetServerConfig: %v", err)
	}
	want := ServerConfig{GitHost: "sshdock.example.com", UpdatedAt: updatedAt}
	if got != want {
		t.Fatalf("server config = %#v, want %#v", got, want)
	}
}

func TestSQLiteStoreSSHKeys(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)
	createdAt := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	key := SSHKey{
		Name:      "admin",
		PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey admin@example.com",
		CreatedAt: createdAt,
	}

	if err := store.UpsertSSHKey(ctx, key); err != nil {
		t.Fatalf("UpsertSSHKey: %v", err)
	}
	keys, err := store.ListSSHKeys(ctx)
	if err != nil {
		t.Fatalf("ListSSHKeys: %v", err)
	}
	if len(keys) != 1 || keys[0] != key {
		t.Fatalf("keys = %#v, want [%#v]", keys, key)
	}

	replacement := SSHKey{
		Name:      "admin",
		PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIReplacement admin@example.com",
		CreatedAt: createdAt.Add(time.Minute),
	}
	if err := store.UpsertSSHKey(ctx, replacement); err != nil {
		t.Fatalf("UpsertSSHKey replacement: %v", err)
	}
	keys, err = store.ListSSHKeys(ctx)
	if err != nil {
		t.Fatalf("ListSSHKeys replacement: %v", err)
	}
	if len(keys) != 1 || keys[0] != replacement {
		t.Fatalf("keys after replacement = %#v, want [%#v]", keys, replacement)
	}

	if err := store.DeleteSSHKey(ctx, "admin"); err != nil {
		t.Fatalf("DeleteSSHKey: %v", err)
	}
	keys, err = store.ListSSHKeys(ctx)
	if err != nil {
		t.Fatalf("ListSSHKeys after delete: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("keys after delete = %#v, want none", keys)
	}
	if err := store.DeleteSSHKey(ctx, "admin"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteSSHKey missing error = %v, want ErrNotFound", err)
	}
}

func TestSQLiteStoreAppConfigValues(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)
	createdAt := time.Date(2026, 7, 6, 10, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)

	if err := store.CreateApp(ctx, app.App{ID: "my-app", Name: "my-app", Status: app.AppStatusCreated, CreatedAt: createdAt, UpdatedAt: createdAt}); err != nil {
		t.Fatal(err)
	}

	first := AppConfigValue{
		AppID:      "my-app",
		Name:       "DATABASE_URL",
		Ciphertext: []byte("ciphertext-1"),
		Nonce:      []byte("nonce-1234567"),
		KeyVersion: 1,
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt,
		MutatedBy:  "dashboard",
	}
	if err := store.SetAppConfigValue(ctx, first, configTestEvent("evt_first", "my-app", "set", createdAt)); err != nil {
		t.Fatalf("SetAppConfigValue: %v", err)
	}

	second := AppConfigValue{
		AppID:      "my-app",
		Name:       "API_TOKEN",
		Ciphertext: []byte("ciphertext-2"),
		Nonce:      []byte("nonce-7654321"),
		KeyVersion: 1,
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt,
		MutatedBy:  "dashboard",
	}
	if err := store.SetAppConfigValue(ctx, second, configTestEvent("evt_second", "my-app", "set", createdAt)); err != nil {
		t.Fatalf("SetAppConfigValue second: %v", err)
	}

	replacement := first
	replacement.Ciphertext = []byte("ciphertext-new")
	replacement.Nonce = []byte("nonce-new1234")
	replacement.CreatedAt = updatedAt
	replacement.UpdatedAt = updatedAt
	if err := store.SetAppConfigValue(ctx, replacement, configTestEvent("evt_replacement", "my-app", "set", updatedAt)); err != nil {
		t.Fatalf("SetAppConfigValue replacement: %v", err)
	}

	got, err := store.GetAppConfigValue(ctx, AppConfigRef{AppID: "my-app", Name: "DATABASE_URL"})
	if err != nil {
		t.Fatalf("GetAppConfigValue: %v", err)
	}
	if string(got.Ciphertext) != "ciphertext-new" || !got.CreatedAt.Equal(createdAt) || !got.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("config value = %#v", got)
	}

	values, err := store.ListAppConfigValues(ctx, "my-app")
	if err != nil {
		t.Fatalf("ListAppConfigValues: %v", err)
	}
	if len(values) != 2 {
		t.Fatalf("values = %#v, want two", values)
	}
	if values[0].Name != "API_TOKEN" || values[1].Name != "DATABASE_URL" {
		t.Fatalf("values ordering/content = %#v", values)
	}

	if err := store.DeleteAppConfigValue(ctx, AppConfigRef{AppID: "my-app", Name: "API_TOKEN"}, configTestEvent("evt_delete", "my-app", "unset", updatedAt)); err != nil {
		t.Fatalf("DeleteAppConfigValue: %v", err)
	}
	if _, err := store.GetAppConfigValue(ctx, AppConfigRef{AppID: "my-app", Name: "API_TOKEN"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetAppConfigValue deleted error = %v, want ErrNotFound", err)
	}
	if err := store.DeleteAppConfigValue(ctx, AppConfigRef{AppID: "my-app", Name: "API_TOKEN"}, configTestEvent("evt_delete", "my-app", "unset", updatedAt)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteAppConfigValue missing error = %v, want ErrNotFound", err)
	}
}

func TestSQLiteStoreDeleteAppRemovesRelatedRows(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	model := app.App{
		ID:           "app_1",
		Name:         "my-app",
		NodeID:       "local",
		RepoPath:     "/data/apps/my-app/repo.git",
		WorktreePath: "/data/apps/my-app/worktree",
		Status:       app.AppStatusHealthy,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := store.CreateApp(ctx, model); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if err := store.CreateRelease(ctx, app.Release{ID: "rel_1", AppID: model.ID, CommitSHA: "abc123", ComposePath: "compose.yml", Status: app.ReleaseStatusSucceeded, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}
	if err := store.CreateDeployment(ctx, app.Deployment{ID: "dep_1", AppID: model.ID, ReleaseID: "rel_1", Status: app.DeploymentStatusSucceeded, StartedAt: now, FinishedAt: now}); err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}
	queued := app.Deployment{ID: "dep_log", AppID: model.ID, ReleaseID: "rel_1", CommitSHA: "abc124", Trigger: app.DeploymentTriggerPush, Status: app.DeploymentStatusPending, StartedAt: now.Add(time.Minute)}
	if err := store.QueueDeployment(ctx, queued, "abc123"); err != nil {
		t.Fatalf("QueueDeployment: %v", err)
	}
	if err := store.UpdateDeploymentStatus(ctx, queued.ID, app.DeploymentStatusSucceeded, queued.StartedAt, ""); err != nil {
		t.Fatalf("UpdateDeploymentStatus: %v", err)
	}
	if err := store.AttachDomain(ctx, app.Domain{ID: "dom_1", AppID: model.ID, ServiceName: "web", DomainName: "example.com", Port: 3000, HTTPS: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("AttachDomain: %v", err)
	}
	if err := store.CreateEvent(ctx, app.Event{ID: "evt_1", AppID: model.ID, Type: "deploy.succeeded", Message: "Deploy succeeded", CreatedAt: now}); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	if err := store.SetAppConfigValue(ctx, AppConfigValue{AppID: model.ID, Name: "SECRET", Ciphertext: []byte("ciphertext"), Nonce: []byte("nonce-1234567"), KeyVersion: 1, CreatedAt: now, UpdatedAt: now}, configTestEvent("evt_config", model.ID, "set", now)); err != nil {
		t.Fatalf("SetAppConfigValue: %v", err)
	}

	if err := store.DeleteApp(ctx, model.ID); err != nil {
		t.Fatalf("DeleteApp: %v", err)
	}

	if _, err := store.GetApp(ctx, model.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetApp after DeleteApp error = %v, want ErrNotFound", err)
	}
	if releases, err := store.ListReleasesByApp(ctx, model.ID); err != nil || len(releases) != 0 {
		t.Fatalf("releases after DeleteApp = %#v, err = %v", releases, err)
	}
	if deployments, err := store.ListDeploymentsByApp(ctx, model.ID); err != nil || len(deployments) != 0 {
		t.Fatalf("deployments after DeleteApp = %#v, err = %v", deployments, err)
	}
	if _, err := store.DeploymentLog(ctx, model.ID, "dep_log"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deployment log after DeleteApp error = %v, want ErrNotFound", err)
	}
	if domains, err := store.ListDomainsByApp(ctx, model.ID); err != nil || len(domains) != 0 {
		t.Fatalf("domains after DeleteApp = %#v, err = %v", domains, err)
	}
	if events, err := store.ListEventsByApp(ctx, model.ID); err != nil || len(events) != 2 || events[1].Type != "config.set" {
		t.Fatalf("audit events after DeleteApp = %#v, err = %v", events, err)
	}
	if values, err := store.ListAppConfigValues(ctx, model.ID); err != nil || len(values) != 0 {
		t.Fatalf("config values after DeleteApp = %#v, err = %v", values, err)
	}
	if err := store.DeleteApp(ctx, model.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteApp missing error = %v, want ErrNotFound", err)
	}
}

func TestSQLiteStoreQueueDeploymentRetainsOnlyNewestDeploymentLogs(t *testing.T) {
	// Given
	ctx := context.Background()
	store := newTestStore(t, ctx)
	now := time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)
	model := app.App{ID: "my-app", Name: "my-app", NodeID: "local", Status: app.AppStatusCreated, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateApp(ctx, model); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	// When
	for index := range 21 {
		deployment := app.Deployment{
			ID:        fmt.Sprintf("dep_%02d", index),
			AppID:     model.ID,
			ReleaseID: fmt.Sprintf("rel_%02d", index),
			CommitSHA: fmt.Sprintf("commit_%02d", index),
			Trigger:   app.DeploymentTriggerPush,
			Status:    app.DeploymentStatusPending,
			StartedAt: now.Add(time.Duration(index) * time.Nanosecond),
		}
		if err := store.QueueDeployment(ctx, deployment, fmt.Sprintf("previous_%02d", index)); err != nil {
			t.Fatalf("QueueDeployment %d: %v", index, err)
		}
		if err := store.UpdateDeploymentStatus(ctx, deployment.ID, app.DeploymentStatusSucceeded, deployment.StartedAt.Add(time.Second), ""); err != nil {
			t.Fatalf("UpdateDeploymentStatus %d: %v", index, err)
		}
	}

	// Then
	if _, err := store.DeploymentLog(ctx, model.ID, "dep_00"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeploymentLog oldest error = %v, want ErrNotFound", err)
	}
	latest, err := store.LatestDeploymentLog(ctx, model.ID)
	if err != nil {
		t.Fatalf("LatestDeploymentLog: %v", err)
	}
	if latest.DeploymentID != "dep_20" {
		t.Fatalf("latest deployment log = %#v, want dep_20", latest)
	}
	if err := store.AppendDeploymentLog(ctx, model.ID, latest.DeploymentID, "deploy output\n", now.Add(time.Minute)); err != nil {
		t.Fatalf("AppendDeploymentLog: %v", err)
	}
	latest, err = store.DeploymentLog(ctx, model.ID, latest.DeploymentID)
	if err != nil {
		t.Fatalf("DeploymentLog latest: %v", err)
	}
	if latest.Content != "deploy output\n" || latest.Truncated {
		t.Fatalf("latest deployment log = %#v", latest)
	}
}

func TestAppendDeploymentLogContentCapsOutputWithMarker(t *testing.T) {
	// Given
	existing := strings.Repeat("x", deploymentLogLimitBytes)

	// When
	content, truncated := appendDeploymentLogContent(existing, "more output")

	// Then
	if !truncated {
		t.Fatal("truncated = false, want true")
	}
	if len(content) != deploymentLogLimitBytes {
		t.Fatalf("log length = %d, want %d", len(content), deploymentLogLimitBytes)
	}
	if !strings.HasSuffix(content, deploymentLogTruncationMarker) {
		t.Fatalf("log missing truncation marker: %q", content[len(content)-len(deploymentLogTruncationMarker):])
	}
}

func TestSQLiteStoreMissingRowsReturnNotFound(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, ctx)

	_, err := store.GetApp(ctx, "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetApp error = %v, want ErrNotFound", err)
	}

	_, err = store.GetRelease(ctx, "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetRelease error = %v, want ErrNotFound", err)
	}
}

func newTestStore(t *testing.T, ctx context.Context) *SQLiteStore {
	t.Helper()

	store, err := OpenSQLite(ctx, filepath.Join(t.TempDir(), "sshdock.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	return store
}
