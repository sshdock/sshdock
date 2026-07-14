package compose

import (
	"strings"
	"testing"
)

func TestEffectiveComposeRouteInference(t *testing.T) {
	tests := []struct {
		name       string
		model      string
		wantTarget RouteTarget
		wantFound  bool
		wantReason string
	}{
		{
			name: "web preference",
			model: `{"services":{
				"api":{"ports":[{"host_ip":"127.0.0.1","published":"4100","target":80,"protocol":"tcp"}]},
				"web":{"ports":[{"host_ip":"0.0.0.0","published":"3100","target":80,"protocol":"tcp"}]}
			}}`,
			wantTarget: RouteTarget{ServiceName: "web", Port: 3100},
			wantFound:  true,
		},
		{
			name:       "unique service fallback",
			model:      `{"services":{"api":{"ports":[{"published":"4100","target":80,"protocol":"tcp"}]},"worker":{}}}`,
			wantTarget: RouteTarget{ServiceName: "api", Port: 4100},
			wantFound:  true,
		},
		{
			name: "single loopback candidate preferred",
			model: `{"services":{
				"api":{"ports":[{"host_ip":"127.0.0.1","published":"4100","target":80,"protocol":"tcp"}]},
				"metrics":{"ports":[{"host_ip":"0.0.0.0","published":"4200","target":80,"protocol":"tcp"}]}
			}}`,
			wantTarget: RouteTarget{ServiceName: "api", Port: 4100},
			wantFound:  true,
		},
		{
			name: "ambiguous candidates",
			model: `{"services":{
				"api":{"ports":[{"published":"4100","target":80,"protocol":"tcp"}]},
				"admin":{"ports":[{"published":"4200","target":80,"protocol":"tcp"}]}
			}}`,
			wantReason: "multiple route candidates: admin, api",
		},
		{
			name:       "no published TCP port",
			model:      `{"services":{"worker":{"image":"example/worker"},"dns":{"ports":[{"published":"5353","target":53,"protocol":"udp"}]}}}`,
			wantReason: "no service with exactly one published TCP port",
		},
		{
			name:       "host binding unreachable from Caddy",
			model:      `{"services":{"web":{"ports":[{"host_ip":"::1","published":"3100","target":80,"protocol":"tcp"}]}}}`,
			wantReason: "no service with exactly one published TCP port",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given: Docker Compose produced the effective JSON model above.

			// When
			result, err := analyzeEffectiveModel(test.model, "sshdock_my-app")

			// Then
			if err != nil {
				t.Fatalf("analyzeEffectiveModel: %v", err)
			}
			if result.RouteFound != test.wantFound || result.RouteTarget != test.wantTarget {
				t.Fatalf("route result = %#v, want found=%t target=%#v", result, test.wantFound, test.wantTarget)
			}
			if test.wantReason != "" && !strings.Contains(result.RouteReason, test.wantReason) {
				t.Fatalf("route reason = %q, want substring %q", result.RouteReason, test.wantReason)
			}
		})
	}
}
