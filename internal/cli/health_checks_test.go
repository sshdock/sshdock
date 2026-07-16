package cli

import "testing"

func TestHealthCheckForHistoricalRolledBackReleaseWarns(t *testing.T) {
	check := healthCheckForRelease("rel_legacy", "rolled_back")
	if check.Status != "warn" {
		t.Fatalf("health check = %#v, want legacy rolled_back status to warn", check)
	}
}
