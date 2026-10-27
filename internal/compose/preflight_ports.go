package compose

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
)

type preflightPort struct {
	service, host, protocol string
	port                    int
}

func (p preflightPort) address() string {
	host := p.host
	if host == "" {
		host = "*"
	}
	return p.protocol + " " + net.JoinHostPort(host, strconv.Itoa(p.port))
}

func (r *PreflightReport) preflightPublishedPorts(model composeModel) []preflightPort {
	var ports []preflightPort
	for _, service := range sortedKeys(modelServices(model)) {
		entries, _ := modelMapping(modelServices(model)[service])["ports"].([]any)
		for _, entry := range entries {
			value := modelMapping(entry)
			published, fixed := modelInt(value["published"])
			protocol, _ := modelString(value["protocol"])
			if protocol == "" {
				protocol = "tcp"
			}
			host, _ := modelString(value["host_ip"])
			if !fixed || published < 1 || published > 65535 || protocol != "tcp" && protocol != "udp" || host != "" && net.ParseIP(host) == nil {
				r.add("published ports "+service, "unknown", "A dynamic, ranged or unsupported published binding cannot be checked as a fixed TCP/UDP host port.", "Review the effective Compose ports; verify the allocated binding on the target server after deployment.", false)
				continue
			}
			port := preflightPort{service: service, host: host, protocol: protocol, port: published}
			for _, earlier := range ports {
				if overlap, certain := preflightPortsOverlap(port, earlier); overlap {
					status := "unknown"
					if certain {
						status = "fail"
					}
					r.add("published port collision", status, fmt.Sprintf("Services %s and %s request overlapping %s bindings.", earlier.service, service, port.address()), "Assign distinct host ports or non-overlapping bind addresses.", certain)
				}
			}
			ports = append(ports, port)
		}
	}
	if len(ports) == 0 {
		r.add("published ports", "unknown", "No fixed TCP/UDP host port was available for collision inspection; worker-only apps may need no published ports.", "Review service ports if public routing is intended.", false)
	}
	return ports
}

func (r *DockerRunner) preflightServerPorts(ctx context.Context, request PreflightRequest, projectName string, ports []preflightPort, runtimeAvailable bool, report *PreflightReport) {
	if len(ports) == 0 {
		return
	}
	bindings, bindingsKnown := r.preflightDockerBindings(ctx, request, runtimeAvailable)
	listeners := map[string][]preflightPort{}
	listenersKnown := map[string]bool{}
	for _, protocol := range []string{"tcp", "udp"} {
		needed := false
		for _, port := range ports {
			needed = needed || port.protocol == protocol
		}
		if !needed {
			continue
		}
		flag := "-ltn"
		if protocol == "udp" {
			flag = "-lun"
		}
		output, _, err := r.preflightCommand(ctx, Command{Name: "ss", Dir: request.ProjectDir, Env: request.Env, Args: []string{"-H", flag}})
		if err == nil {
			listeners[protocol], listenersKnown[protocol] = preflightListeners(output, protocol)
		}
	}
	for _, port := range ports {
		status, detail := "ok", "No conflicting current listener or Docker binding was observed; the port was not reserved."
		var own []preflightPort
		for _, binding := range bindings {
			overlap, certain := preflightPortsOverlap(port, binding.port)
			if !overlap {
				continue
			}
			if binding.project == projectName {
				if binding.port.service == port.service {
					own = append(own, binding.port)
				} else if status == "ok" {
					status, detail = "warn", "Another service in this Compose project publishes the binding; replacement ordering may conflict."
				}
				continue
			}
			if certain {
				status, detail = "fail", "An overlapping binding is published by another running Docker container."
				break
			}
			status, detail = "unknown", "Another Docker binding may overlap; wildcard/IPv6 binding behavior is indeterminate."
		}
		if status == "ok" {
			if len(own) > 0 {
				detail = "This app's current Compose service publishes the port; its own occupancy is expected."
			}
			for _, listener := range listeners[port.protocol] {
				if overlap, _ := preflightPortsOverlap(port, listener); overlap {
					accounted := false
					for _, binding := range own {
						accounted = accounted || preflightBindingCovers(binding, listener)
					}
					if !accounted {
						status, detail = "warn", "A current host listener may conflict; listener ownership is unverified."
						break
					}
				}
			}
			if status == "ok" && (!bindingsKnown || !listenersKnown[port.protocol]) {
				status, detail = "unknown", "Host listeners or Docker publisher ownership could not be fully inspected."
			}
		}
		fix := ""
		if status != "ok" {
			fix = "Inspect docker ps and sudo ss -ltnup on the target server; choose another host port if a different workload owns it."
		}
		report.add("port "+port.service+" "+port.address(), status, detail+" Host listener observations assume a local Docker engine; remote engine host listeners are unverified.", fix, status == "fail")
	}
}

func preflightBindingCovers(binding, listener preflightPort) bool {
	if binding.port != listener.port || binding.protocol != listener.protocol {
		return false
	}
	owner, observed := net.ParseIP(binding.host), net.ParseIP(listener.host)
	if owner == nil || observed == nil {
		return false
	}
	return owner.Equal(observed) || owner.IsUnspecified() && (owner.To4() == nil) == (observed.To4() == nil)
}

type preflightBinding struct {
	project string
	port    preflightPort
}

func (r *DockerRunner) preflightDockerBindings(ctx context.Context, request PreflightRequest, runtimeAvailable bool) ([]preflightBinding, bool) {
	if !runtimeAvailable {
		return nil, false
	}
	output, _, err := r.preflightCommand(ctx, Command{Name: "docker", Dir: request.ProjectDir, Env: request.Env, Args: []string{"ps", "--quiet"}})
	if err != nil {
		return nil, false
	}
	ids := strings.Fields(output)
	if len(ids) == 0 {
		return nil, true
	}
	output, _, err = r.preflightCommand(ctx, Command{Name: "docker", Dir: request.ProjectDir, Env: request.Env, Args: append([]string{"inspect"}, ids...)})
	if err != nil {
		return nil, false
	}
	var containers []struct {
		Config          struct{ Labels map[string]string }
		NetworkSettings struct {
			Ports map[string][]struct{ HostIp, HostPort string }
		}
	}
	if err := json.Unmarshal([]byte(output), &containers); err != nil {
		return nil, false
	}
	var bindings []preflightBinding
	for _, container := range containers {
		for key, values := range container.NetworkSettings.Ports {
			_, protocol, found := strings.Cut(key, "/")
			if !found {
				return nil, false
			}
			for _, value := range values {
				port, err := strconv.Atoi(value.HostPort)
				if err != nil || port < 1 || port > 65535 {
					return nil, false
				}
				bindings = append(bindings, preflightBinding{project: container.Config.Labels["com.docker.compose.project"], port: preflightPort{service: container.Config.Labels["com.docker.compose.service"], host: value.HostIp, protocol: protocol, port: port}})
			}
		}
	}
	return bindings, true
}

func preflightListeners(output, protocol string) ([]preflightPort, bool) {
	var ports []preflightPort
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return nil, false
		}
		endpoint := fields[3]
		colon := strings.LastIndex(endpoint, ":")
		if colon < 0 {
			return nil, false
		}
		port, err := strconv.Atoi(endpoint[colon+1:])
		if err != nil || port < 1 || port > 65535 {
			return nil, false
		}
		ports = append(ports, preflightPort{host: strings.Trim(endpoint[:colon], "[]"), port: port, protocol: protocol})
	}
	return ports, true
}

func preflightPortsOverlap(a, b preflightPort) (bool, bool) {
	if a.port != b.port || a.protocol != b.protocol {
		return false, true
	}
	if a.host == b.host {
		return true, true
	}
	left, right := net.ParseIP(a.host), net.ParseIP(b.host)
	if left == nil || right == nil {
		return true, false
	}
	if left.Equal(right) {
		return true, true
	}
	if (left.To4() == nil) == (right.To4() == nil) {
		return left.IsUnspecified() || right.IsUnspecified(), true
	}
	return left.IsUnspecified() || right.IsUnspecified(), false
}
