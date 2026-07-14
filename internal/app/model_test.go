package app

import (
	"strings"
	"testing"
	"time"
)

func TestAppModelFields(t *testing.T) {
	createdAt := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)

	model := App{
		ID:        "app_123",
		Name:      "my-app",
		NodeID:    "local",
		Status:    AppStatusCreated,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}

	if model.ID != "app_123" {
		t.Fatalf("ID = %q", model.ID)
	}
	if model.Name != "my-app" {
		t.Fatalf("Name = %q", model.Name)
	}
	if model.NodeID != "local" {
		t.Fatalf("NodeID = %q", model.NodeID)
	}
	if model.Status != AppStatusCreated {
		t.Fatalf("Status = %q", model.Status)
	}
	if !model.CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt = %s", model.CreatedAt)
	}
	if !model.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("UpdatedAt = %s", model.UpdatedAt)
	}
}

func TestReleaseIDScopesFullCommitIdentityToApp(t *testing.T) {
	// Given
	commit := "1234567890abcdef1234567890abcdef12345678"

	// When
	first := ReleaseID("first-app", commit)
	second := ReleaseID("second-app", commit)

	// Then
	if first == second {
		t.Fatalf("release IDs = %q, want app-scoped identities", first)
	}
	if !strings.Contains(first, commit) {
		t.Fatalf("release ID = %q, want full commit %q", first, commit)
	}
}

func TestNewDeploymentIDCreatesUniqueAttemptIdentity(t *testing.T) {
	// Given
	const attempts = 2

	// When
	ids := make([]string, 0, attempts)
	for range attempts {
		id, err := NewDeploymentID()
		if err != nil {
			t.Fatalf("NewDeploymentID: %v", err)
		}
		ids = append(ids, id)
	}

	// Then
	if ids[0] == ids[1] {
		t.Fatalf("deployment IDs = %#v, want unique attempt identities", ids)
	}
	for _, id := range ids {
		if !strings.HasPrefix(id, "dep_") || len(id) != len("dep_")+32 {
			t.Fatalf("deployment ID = %q, want dep_ plus 128-bit hex", id)
		}
	}
}

func TestReleaseModelFields(t *testing.T) {
	createdAt := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)

	model := Release{
		ID:          "rel_123",
		AppID:       "app_123",
		CommitSHA:   "abc123",
		ComposePath: "compose.yml",
		Status:      ReleaseStatusPending,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}

	if model.ID != "rel_123" {
		t.Fatalf("ID = %q", model.ID)
	}
	if model.AppID != "app_123" {
		t.Fatalf("AppID = %q", model.AppID)
	}
	if model.CommitSHA != "abc123" {
		t.Fatalf("CommitSHA = %q", model.CommitSHA)
	}
	if model.ComposePath != "compose.yml" {
		t.Fatalf("ComposePath = %q", model.ComposePath)
	}
	if model.Status != ReleaseStatusPending {
		t.Fatalf("Status = %q", model.Status)
	}
	if !model.CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt = %s", model.CreatedAt)
	}
	if !model.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("UpdatedAt = %s", model.UpdatedAt)
	}
}

func TestRuntimeModelsExist(t *testing.T) {
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)

	node := Node{
		ID:        "node_123",
		Name:      "local",
		Address:   "127.0.0.1",
		Status:    "ready",
		CreatedAt: now,
		UpdatedAt: now,
	}
	deployment := Deployment{
		ID:           "dep_123",
		AppID:        "app_123",
		ReleaseID:    "rel_123",
		Status:       DeploymentStatusDeploying,
		StartedAt:    now,
		FinishedAt:   now,
		ErrorMessage: "",
	}
	domain := Domain{
		ID:          "dom_123",
		AppID:       "app_123",
		ServiceName: "web",
		DomainName:  "example.com",
		Port:        3000,
		HTTPS:       true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	event := Event{
		ID:        "evt_123",
		AppID:     "app_123",
		Type:      "app.created",
		Message:   "App created",
		CreatedAt: now,
	}

	if node.ID == "" || deployment.ID == "" || domain.ID == "" || event.ID == "" {
		t.Fatal("expected runtime models to retain IDs")
	}
}

func TestStatusConstants(t *testing.T) {
	appStatuses := []AppStatus{
		AppStatusCreated,
		AppStatusDeploying,
		AppStatusHealthy,
		AppStatusFailed,
		AppStatusUnknown,
	}
	releaseStatuses := []ReleaseStatus{
		ReleaseStatusPending,
		ReleaseStatusDeploying,
		ReleaseStatusSucceeded,
		ReleaseStatusFailed,
		ReleaseStatusRolledBack,
	}
	deploymentStatuses := []DeploymentStatus{
		DeploymentStatusPending,
		DeploymentStatusDeploying,
		DeploymentStatusSucceeded,
		DeploymentStatusFailed,
	}

	for _, status := range appStatuses {
		if status == "" {
			t.Fatal("app status constant is empty")
		}
	}
	for _, status := range releaseStatuses {
		if status == "" {
			t.Fatal("release status constant is empty")
		}
	}
	for _, status := range deploymentStatuses {
		if status == "" {
			t.Fatal("deployment status constant is empty")
		}
	}
}
