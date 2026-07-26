//go:build e2e

package e2e

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/sshdock/sshdock/internal/app"
	_ "modernc.org/sqlite"
)

func waitForVisibleDeploymentStatus(t *testing.T, dbPath string, appName string, commitSHA string) string {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	var lastErr error
	for time.Now().Before(deadline) {
		status, err := deploymentStatusForCommit(dbPath, appName, commitSHA, app.DeploymentTriggerPush)
		if err == nil {
			return status
		}
		lastErr = err
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("deployment status after client return was never readable: %v", lastErr)
	return ""
}

func waitForDeploymentTerminal(t *testing.T, dbPath string, appName string, commitSHA string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, err := deploymentStatusForCommit(dbPath, appName, commitSHA, app.DeploymentTriggerPush)
		if err == nil && (status == string(app.DeploymentStatusSucceeded) || status == string(app.DeploymentStatusFailed)) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("deployment for app %q commit %q did not reach a terminal state", appName, commitSHA)
}

func assertEventTypesContain(t *testing.T, dbPath string, appName string, want []string) {
	t.Helper()
	got := eventTypesForApp(t, dbPath, appName)
	for _, eventType := range want {
		if !strings.Contains(","+strings.Join(got, ",")+",", ","+eventType+",") {
			t.Fatalf("event types = %#v, want %q", got, eventType)
		}
	}
}

func assertAppStatus(t *testing.T, dbPath string, appID string, want app.AppStatus) {
	t.Helper()
	got := queryString(t, dbPath, `select status from apps where id = ?`, appID)
	if got != string(want) {
		t.Fatalf("app %s status = %q, want %q", appID, got, want)
	}
}

func assertReleaseStatus(t *testing.T, dbPath string, releaseID string, want app.ReleaseStatus) {
	t.Helper()
	got := queryString(t, dbPath, `select status from releases where id = ?`, releaseID)
	if got != string(want) {
		t.Fatalf("release %s status = %q, want %q", releaseID, got, want)
	}
}

func assertEventTypes(t *testing.T, dbPath string, appID string, want []string) {
	t.Helper()
	got := eventTypesForApp(t, dbPath, appID)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("event types = %#v, want %#v", got, want)
	}
}

func eventTypesForApp(t *testing.T, dbPath string, appID string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`select type from events where app_id = ? order by created_at, id`, appID)
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var eventType string
		if err := rows.Scan(&eventType); err != nil {
			t.Fatalf("scan event type: %v", err)
		}
		got = append(got, eventType)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("events rows: %v", err)
	}
	return got
}

func assertEventMessageContains(t *testing.T, dbPath string, appID string, want string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	rows, err := db.Query(`select message from events where app_id = ? order by created_at, id`, appID)
	if err != nil {
		t.Fatalf("query event messages: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var message string
		if err := rows.Scan(&message); err != nil {
			t.Fatalf("scan event message: %v", err)
		}
		if strings.Contains(message, want) {
			return
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("event message rows: %v", err)
	}
	t.Fatalf("event messages for %s do not contain %q", appID, want)
}

func countImageRepositoriesWithPrefix(images string, prefix string) int {
	count := 0
	for _, image := range strings.Split(images, "\n") {
		if strings.HasPrefix(image, prefix) {
			count++
		}
	}
	return count
}

func queryString(t *testing.T, dbPath string, query string, args ...any) string {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	var value string
	if err := db.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatalf("query string: %v", err)
	}
	return value
}
