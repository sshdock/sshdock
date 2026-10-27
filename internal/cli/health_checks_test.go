package cli

import "testing"

func TestHealthCheckForIncompleteReleaseWarns(t *testing.T) {
	for _, status := range []string{"pending", "deploying"} {
		check := healthCheckForRelease("rel_current", status)
		if check.Status != "warn" {
			t.Fatalf("health check for %s = %#v, want warning", status, check)
		}
	}
}
