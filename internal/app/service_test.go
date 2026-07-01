package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceCreatesAndListsApps(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	service := NewService(store, WithClock(func() time.Time { return now }))

	created, err := service.CreateApp(ctx, App{
		ID:     "app_1",
		Name:   "my-app",
		NodeID: "local",
	})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	if created.Status != AppStatusCreated {
		t.Fatalf("Status = %q, want %q", created.Status, AppStatusCreated)
	}
	if !created.CreatedAt.Equal(now) {
		t.Fatalf("CreatedAt = %s, want %s", created.CreatedAt, now)
	}
	if !created.UpdatedAt.Equal(now) {
		t.Fatalf("UpdatedAt = %s, want %s", created.UpdatedAt, now)
	}

	apps, err := service.ListApps(ctx)
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(apps) != 1 || apps[0] != created {
		t.Fatalf("ListApps = %#v, want [%#v]", apps, created)
	}
}

func TestServiceCreatesRelease(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	service := NewService(store, WithClock(func() time.Time { return now }))

	release, err := service.CreateRelease(ctx, Release{
		ID:          "rel_1",
		AppID:       "app_1",
		CommitSHA:   "abc123",
		ComposePath: "compose.yml",
	})
	if err != nil {
		t.Fatalf("CreateRelease: %v", err)
	}

	if release.Status != ReleaseStatusPending {
		t.Fatalf("Status = %q, want %q", release.Status, ReleaseStatusPending)
	}
	if !release.CreatedAt.Equal(now) {
		t.Fatalf("CreatedAt = %s, want %s", release.CreatedAt, now)
	}
	if !release.UpdatedAt.Equal(now) {
		t.Fatalf("UpdatedAt = %s, want %s", release.UpdatedAt, now)
	}
}

func TestServiceDeploymentLifecycle(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	startedAt := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(time.Minute)
	service := NewService(store, WithClock(func() time.Time { return startedAt }))

	deployment, err := service.StartDeployment(ctx, Deployment{
		ID:        "dep_1",
		AppID:     "app_1",
		ReleaseID: "rel_1",
	})
	if err != nil {
		t.Fatalf("StartDeployment: %v", err)
	}

	if deployment.Status != DeploymentStatusDeploying {
		t.Fatalf("Status = %q, want %q", deployment.Status, DeploymentStatusDeploying)
	}
	if !deployment.StartedAt.Equal(startedAt) {
		t.Fatalf("StartedAt = %s, want %s", deployment.StartedAt, startedAt)
	}

	service = NewService(store, WithClock(func() time.Time { return finishedAt }))
	if err := service.MarkDeploymentSucceeded(ctx, deployment.ID); err != nil {
		t.Fatalf("MarkDeploymentSucceeded: %v", err)
	}
	if store.deploymentStatuses[deployment.ID] != DeploymentStatusSucceeded {
		t.Fatalf("deployment status = %q", store.deploymentStatuses[deployment.ID])
	}
	if !store.deploymentFinishedAt[deployment.ID].Equal(finishedAt) {
		t.Fatalf("finished at = %s", store.deploymentFinishedAt[deployment.ID])
	}

	if err := service.MarkDeploymentFailed(ctx, deployment.ID, "compose failed"); err != nil {
		t.Fatalf("MarkDeploymentFailed: %v", err)
	}
	if store.deploymentStatuses[deployment.ID] != DeploymentStatusFailed {
		t.Fatalf("deployment status = %q", store.deploymentStatuses[deployment.ID])
	}
	if store.deploymentErrors[deployment.ID] != "compose failed" {
		t.Fatalf("deployment error = %q", store.deploymentErrors[deployment.ID])
	}
}

func TestServiceAttachesDomain(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	service := NewService(store, WithClock(func() time.Time { return now }))

	domain, err := service.AttachDomain(ctx, Domain{
		ID:          "dom_1",
		AppID:       "app_1",
		ServiceName: "web",
		DomainName:  "example.com",
		Port:        3000,
		HTTPS:       true,
	})
	if err != nil {
		t.Fatalf("AttachDomain: %v", err)
	}

	if !domain.CreatedAt.Equal(now) {
		t.Fatalf("CreatedAt = %s, want %s", domain.CreatedAt, now)
	}
	if !domain.UpdatedAt.Equal(now) {
		t.Fatalf("UpdatedAt = %s, want %s", domain.UpdatedAt, now)
	}
	if len(store.domains[domain.AppID]) != 1 || store.domains[domain.AppID][0] != domain {
		t.Fatalf("stored domains = %#v", store.domains[domain.AppID])
	}
}

func TestServiceRollbackReleaseStartsDeploymentForKnownRelease(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	service := NewService(store, WithClock(func() time.Time { return now }))
	store.releases["rel_1"] = Release{
		ID:          "rel_1",
		AppID:       "app_1",
		CommitSHA:   "abc123",
		ComposePath: "compose.yml",
		Status:      ReleaseStatusSucceeded,
	}

	deployment, err := service.RollbackRelease(ctx, "app_1", "rel_1", "dep_rollback_1")
	if err != nil {
		t.Fatalf("RollbackRelease: %v", err)
	}

	if deployment.AppID != "app_1" {
		t.Fatalf("AppID = %q", deployment.AppID)
	}
	if deployment.ReleaseID != "rel_1" {
		t.Fatalf("ReleaseID = %q", deployment.ReleaseID)
	}
	if deployment.Status != DeploymentStatusDeploying {
		t.Fatalf("Status = %q", deployment.Status)
	}
	if !deployment.StartedAt.Equal(now) {
		t.Fatalf("StartedAt = %s", deployment.StartedAt)
	}
}

func TestServiceRollbackRejectsReleaseFromDifferentApp(t *testing.T) {
	ctx := context.Background()
	store := newFakeServiceStore()
	service := NewService(store)
	store.releases["rel_1"] = Release{ID: "rel_1", AppID: "other-app"}

	_, err := service.RollbackRelease(ctx, "app_1", "rel_1", "dep_rollback_1")
	if err == nil {
		t.Fatal("RollbackRelease error = nil, want error")
	}
}

type fakeServiceStore struct {
	apps                 map[string]App
	releases             map[string]Release
	deployments          map[string]Deployment
	deploymentStatuses   map[string]DeploymentStatus
	deploymentFinishedAt map[string]time.Time
	deploymentErrors     map[string]string
	domains              map[string][]Domain
	events               map[string][]Event
}

func newFakeServiceStore() *fakeServiceStore {
	return &fakeServiceStore{
		apps:                 map[string]App{},
		releases:             map[string]Release{},
		deployments:          map[string]Deployment{},
		deploymentStatuses:   map[string]DeploymentStatus{},
		deploymentFinishedAt: map[string]time.Time{},
		deploymentErrors:     map[string]string{},
		domains:              map[string][]Domain{},
		events:               map[string][]Event{},
	}
}

func (f *fakeServiceStore) CreateApp(_ context.Context, model App) error {
	f.apps[model.ID] = model
	return nil
}

func (f *fakeServiceStore) GetApp(_ context.Context, id string) (App, error) {
	model, ok := f.apps[id]
	if !ok {
		return App{}, errors.New("not found")
	}
	return model, nil
}

func (f *fakeServiceStore) ListApps(_ context.Context) ([]App, error) {
	apps := make([]App, 0, len(f.apps))
	for _, model := range f.apps {
		apps = append(apps, model)
	}
	return apps, nil
}

func (f *fakeServiceStore) CreateRelease(_ context.Context, model Release) error {
	f.releases[model.ID] = model
	return nil
}

func (f *fakeServiceStore) GetRelease(_ context.Context, id string) (Release, error) {
	model, ok := f.releases[id]
	if !ok {
		return Release{}, errors.New("not found")
	}
	return model, nil
}

func (f *fakeServiceStore) ListReleasesByApp(_ context.Context, appID string) ([]Release, error) {
	var releases []Release
	for _, model := range f.releases {
		if model.AppID == appID {
			releases = append(releases, model)
		}
	}
	return releases, nil
}

func (f *fakeServiceStore) CreateDeployment(_ context.Context, model Deployment) error {
	f.deployments[model.ID] = model
	return nil
}

func (f *fakeServiceStore) UpdateDeploymentStatus(_ context.Context, id string, status DeploymentStatus, finishedAt time.Time, errorMessage string) error {
	f.deploymentStatuses[id] = status
	f.deploymentFinishedAt[id] = finishedAt
	f.deploymentErrors[id] = errorMessage
	return nil
}

func (f *fakeServiceStore) AttachDomain(_ context.Context, model Domain) error {
	f.domains[model.AppID] = append(f.domains[model.AppID], model)
	return nil
}

func (f *fakeServiceStore) ListDomainsByApp(_ context.Context, appID string) ([]Domain, error) {
	return f.domains[appID], nil
}

func (f *fakeServiceStore) CreateEvent(_ context.Context, model Event) error {
	f.events[model.AppID] = append(f.events[model.AppID], model)
	return nil
}

func (f *fakeServiceStore) ListEventsByApp(_ context.Context, appID string) ([]Event, error) {
	return f.events[appID], nil
}
