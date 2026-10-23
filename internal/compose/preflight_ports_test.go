package compose

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestPreflightServerPortOwnershipAndObservation(t *testing.T) {
	for _, test := range []struct {
		name, project, service, host, protocol, listener, status string
		inspectError, listenerError                              bool
	}{
		{name: "free snapshot", status: "ok"},
		{name: "own service occupancy", project: "sshdock_my-app", service: "web", host: "127.0.0.1", protocol: "tcp", listener: "LISTEN 0 128 127.0.0.1:3100 0.0.0.0:*", status: "ok"},
		{name: "another service in same project", project: "sshdock_my-app", service: "old-service", host: "127.0.0.1", protocol: "tcp", status: "warn"},
		{name: "foreign container collision", project: "sshdock_other", service: "web", host: "127.0.0.1", protocol: "tcp", status: "fail"},
		{name: "foreign wildcard collision", project: "sshdock_other", service: "web", host: "0.0.0.0", protocol: "tcp", status: "fail"},
		{name: "different bind address", project: "sshdock_other", service: "web", host: "127.0.0.2", protocol: "tcp", listener: "LISTEN 0 128 127.0.0.2:3100 0.0.0.0:*", status: "ok"},
		{name: "different protocol", project: "sshdock_other", service: "web", host: "127.0.0.1", protocol: "udp", status: "ok"},
		{name: "IPv6 dual stack indeterminate", project: "sshdock_other", service: "web", host: "::", protocol: "tcp", status: "unknown"},
		{name: "unowned host listener", listener: "LISTEN 0 128 127.0.0.1:3100 0.0.0.0:*", status: "warn"},
		{name: "listener ownership unobservable", listenerError: true, status: "unknown"},
		{name: "listener output unreadable", listener: "not a listener record", status: "unknown"},
		{name: "container ownership unobservable", project: "sshdock_my-app", inspectError: true, status: "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, executor := preflightFixture(t, "server")
			if test.project != "" {
				executor.responses["docker ps --quiet"] = preflightResponse{stdout: "abc123\n"}
				executor.responses["docker inspect abc123"] = preflightResponse{stdout: fmt.Sprintf(`[{"Config":{"Labels":{"com.docker.compose.project":%q,"com.docker.compose.service":%q}},"NetworkSettings":{"Ports":{"80/%s":[{"HostIp":%q,"HostPort":"3100"}]}}}]`, test.project, test.service, test.protocol, test.host)}
			}
			if test.inspectError {
				executor.responses["docker inspect abc123"] = preflightResponse{err: errors.New("container disappeared during inspection")}
			}
			executor.responses["ss -H -ltn"] = preflightResponse{stdout: test.listener}
			if test.listenerError {
				executor.responses["ss -H -ltn"] = preflightResponse{err: errors.New("ss unavailable")}
			}
			report, err := NewDockerRunner(executor).Preflight(context.Background(), request)
			if err != nil || report.OK == (test.status == "fail") {
				t.Fatalf("report=%#v error=%v", report, err)
			}
			check := findPreflightCheck(t, report, "port web tcp 127.0.0.1:3100", test.status)
			if check.Blocking != (test.status == "fail") || test.status != "ok" && check.Fix == "" {
				t.Fatalf("port check = %#v", check)
			}
			if test.name == "own service occupancy" && !strings.Contains(check.Detail, "own occupancy is expected") {
				t.Fatalf("own service occupancy was misreported: %#v", check)
			}
			for _, command := range executor.commands {
				if command.Name == "git" {
					t.Fatal("server scope resolved workstation Git metadata")
				}
				if command.Name == "docker" && (command.Dir != request.ProjectDir || command.Env[GitSHAEnv] != request.Env[GitSHAEnv]) {
					t.Fatalf("Docker observation used different environment or directory: %#v", command)
				}
			}
		})
	}
}

func TestPreflightWidenedOwnBindingStillInspectsOtherListeners(t *testing.T) {
	for _, test := range []struct {
		name, listeners, status string
		unavailable             bool
	}{
		{name: "additional loopback listener", listeners: "LISTEN 0 128 127.0.0.1:3100 0.0.0.0:*\nLISTEN 0 128 127.0.0.2:3100 0.0.0.0:*\n", status: "warn"},
		{name: "wildcard listener exceeds own binding", listeners: "LISTEN 0 128 0.0.0.0:3100 0.0.0.0:*\n", status: "warn"},
		{name: "listener inspection unavailable", unavailable: true, status: "unknown"},
		{name: "only observed own listener", listeners: "LISTEN 0 128 127.0.0.1:3100 0.0.0.0:*\n", status: "ok"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, executor := preflightFixture(t, "server")
			executor.responses["config"] = preflightResponse{stdout: `{"services":{"web":{"ports":[{"host_ip":"0.0.0.0","published":"3100","target":80,"protocol":"tcp"}]}}}`}
			executor.responses["docker ps --quiet"] = preflightResponse{stdout: "abc123\n"}
			executor.responses["docker inspect abc123"] = preflightResponse{stdout: `[{"Config":{"Labels":{"com.docker.compose.project":"sshdock_my-app","com.docker.compose.service":"web"}},"NetworkSettings":{"Ports":{"80/tcp":[{"HostIp":"127.0.0.1","HostPort":"3100"}]}}}]`}
			executor.responses["ss -H -ltn"] = preflightResponse{stdout: test.listeners}
			if test.unavailable {
				executor.responses["ss -H -ltn"] = preflightResponse{err: errors.New("ss unavailable")}
			}
			report, err := NewDockerRunner(executor).Preflight(context.Background(), request)
			if err != nil || !report.OK {
				t.Fatalf("report=%#v error=%v", report, err)
			}
			check := findPreflightCheck(t, report, "port web tcp 0.0.0.0:3100", test.status)
			if check.Blocking || test.status != "ok" && (check.Fix == "" || strings.Contains(check.Detail, "own occupancy is expected")) {
				t.Fatalf("widened binding falsely reported as accounted for: %#v", check)
			}
		})
	}
}

func TestPreflightPublishedPortsRoutingAndUnsupportedBindings(t *testing.T) {
	for _, test := range []struct {
		name, model, check, status string
		blocking                   bool
	}{
		{
			name: "ambiguous routing", check: "routing", status: "warn",
			model: `{"services":{"api":{"ports":[{"host_ip":"127.0.0.1","published":"3100","target":80}]},"admin":{"ports":[{"host_ip":"127.0.0.1","published":"3200","target":80}]}}}`,
		},
		{
			name: "duplicate bindings", check: "published port collision", status: "fail", blocking: true,
			model: `{"services":{"api":{"ports":[{"host_ip":"127.0.0.1","published":"3100","target":80}]},"admin":{"ports":[{"host_ip":"0.0.0.0","published":"3100","target":80}]}}}`,
		},
		{
			name: "dynamic port", check: "published ports web", status: "unknown",
			model: `{"services":{"web":{"ports":[{"target":80,"protocol":"tcp"}]}}}`,
		},
		{
			name: "ranged ports", check: "published ports web", status: "unknown",
			model: `{"services":{"web":{"ports":[{"published":"3100-3105","target":80,"protocol":"tcp"}]}}}`,
		},
		{
			name: "TCP UDP sharing is not a collision", check: "native Compose validation", status: "ok",
			model: `{"services":{"web":{"ports":[{"host_ip":"127.0.0.1","published":"3100","target":80,"protocol":"tcp"},{"host_ip":"127.0.0.1","published":"3100","target":80,"protocol":"udp"}]}}}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, executor := preflightFixture(t, "local")
			executor.responses["config"] = preflightResponse{stdout: test.model}
			report, err := NewDockerRunner(executor).Preflight(context.Background(), request)
			if err != nil || report.OK == test.blocking || findPreflightCheck(t, report, test.check, test.status).Blocking != test.blocking {
				t.Fatalf("report=%#v error=%v", report, err)
			}
		})
	}
}

func TestPreflightInspectsUDPListenersIndependently(t *testing.T) {
	request, executor := preflightFixture(t, "server")
	executor.responses["config"] = preflightResponse{stdout: `{"services":{"dns":{"ports":[{"host_ip":"127.0.0.1","published":"5353","target":53,"protocol":"udp"}]}}}`}
	executor.responses["ss -H -lun"] = preflightResponse{stdout: "UNCONN 0 0 127.0.0.1:5353 0.0.0.0:*\n"}
	report, err := NewDockerRunner(executor).Preflight(context.Background(), request)
	if err != nil || !report.OK {
		t.Fatalf("report=%#v error=%v", report, err)
	}
	findPreflightCheck(t, report, "port dns udp 127.0.0.1:5353", "warn")
	for _, command := range executor.commands {
		if command.Name == "ss" && strings.Join(command.Args, " ") == "-H -ltn" {
			t.Fatal("UDP-only model triggered unrelated TCP listener inspection")
		}
	}
}
