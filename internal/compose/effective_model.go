package compose

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
)

const trustedComposeWarning = "trusted Compose pushes have host-level impact; SSHDock does not sandbox this configuration"

type routeCandidate struct {
	target   RouteTarget
	loopback bool
}

func analyzeEffectiveModel(output string, projectName string) (DeployResult, error) {
	var model composeModel
	if err := json.Unmarshal([]byte(output), &model); err != nil {
		return DeployResult{}, fmt.Errorf("decode effective Compose model: %w", err)
	}

	result := inferEffectiveRoute(model)
	result.Warnings = effectiveModelWarnings(model, projectName)
	return result, nil
}

func inferEffectiveRoute(model composeModel) DeployResult {
	services := modelServices(model)
	serviceNames := sortedKeys(services)
	candidates := make([]routeCandidate, 0, len(serviceNames))
	for _, serviceName := range serviceNames {
		ports := publishedTCPPorts(modelMapping(services[serviceName])["ports"])
		if len(ports) != 1 || !routeHostReachable(ports[0].hostIP) {
			continue
		}
		candidate := routeCandidate{
			target:   RouteTarget{ServiceName: serviceName, Port: ports[0].published},
			loopback: isLoopbackHost(ports[0].hostIP),
		}
		if serviceName == "web" {
			return DeployResult{RouteTarget: candidate.target, RouteFound: true}
		}
		candidates = append(candidates, candidate)
	}

	loopback := make([]routeCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.loopback {
			loopback = append(loopback, candidate)
		}
	}
	if len(loopback) == 1 {
		return DeployResult{RouteTarget: loopback[0].target, RouteFound: true}
	}
	if len(candidates) == 1 {
		return DeployResult{RouteTarget: candidates[0].target, RouteFound: true}
	}
	if len(candidates) == 0 {
		return DeployResult{RouteReason: "effective Compose model has no service with exactly one published TCP port"}
	}

	names := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		names = append(names, candidate.target.ServiceName)
	}
	return DeployResult{RouteReason: "effective Compose model has multiple route candidates: " + strings.Join(names, ", ")}
}

type effectivePort struct {
	hostIP    string
	published int
}

func publishedTCPPorts(value any) []effectivePort {
	return publishedPorts(value, true)
}

func publishedPorts(value any, tcpOnly bool) []effectivePort {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	ports := make([]effectivePort, 0, len(items))
	for _, item := range items {
		port := modelMapping(item)
		protocol, _ := modelString(port["protocol"])
		if tcpOnly && protocol != "" && protocol != "tcp" {
			continue
		}
		published, ok := modelInt(port["published"])
		if !ok || published < 1 || published > 65535 {
			continue
		}
		hostIP, _ := modelString(port["host_ip"])
		ports = append(ports, effectivePort{hostIP: hostIP, published: published})
	}
	return ports
}

func effectiveModelWarnings(model composeModel, projectName string) []string {
	var warnings []string
	services := modelServices(model)
	for _, serviceName := range sortedKeys(services) {
		service := modelMapping(services[serviceName])
		for _, port := range publishedPorts(service["ports"], false) {
			if isAllInterfaces(port.hostIP) {
				hostIP := port.hostIP
				if hostIP == "" {
					hostIP = "0.0.0.0"
				}
				warnings = append(warnings, fmt.Sprintf("service %s publishes %s:%d on all interfaces", serviceName, hostIP, port.published))
			}
		}
		if privileged, _ := service["privileged"].(bool); privileged {
			warnings = append(warnings, "service "+serviceName+" uses privileged mode")
		}
		if networkMode, _ := modelString(service["network_mode"]); networkMode == "host" {
			warnings = append(warnings, "service "+serviceName+" uses host networking")
		}
		warnings = append(warnings, serviceVolumeWarnings(serviceName, service["volumes"])...)
	}

	for _, volumeName := range sortedKeys(modelMapping(model["volumes"])) {
		volume := modelMapping(modelMapping(model["volumes"])[volumeName])
		name, _ := modelString(volume["name"])
		external, _ := volume["external"].(bool)
		if external {
			warnings = append(warnings, "uses external volume "+name)
			continue
		}
		if name != "" && name != projectName+"_"+volumeName {
			warnings = append(warnings, "uses explicit volume name "+name)
		}
	}

	sort.Strings(warnings)
	for index := range warnings {
		warnings[index] += "; " + trustedComposeWarning
	}
	return warnings
}

func serviceVolumeWarnings(serviceName string, value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	var warnings []string
	for _, item := range items {
		volume := modelMapping(item)
		volumeType, _ := modelString(volume["type"])
		if volumeType != "bind" {
			continue
		}
		source, _ := modelString(volume["source"])
		target, _ := modelString(volume["target"])
		if isDockerSocket(source) || isDockerSocket(target) {
			warnings = append(warnings, "service "+serviceName+" mounts the Docker socket")
			continue
		}
		warnings = append(warnings, "service "+serviceName+" uses host bind mount "+source)
	}
	return warnings
}

func modelInt(value any) (int, bool) {
	switch value := value.(type) {
	case float64:
		return int(value), value == float64(int(value))
	case string:
		parsed, err := strconv.Atoi(value)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func isLoopbackHost(host string) bool {
	return net.ParseIP(host).IsLoopback()
}

func isAllInterfaces(host string) bool {
	return host == "" || host == "0.0.0.0" || host == "::"
}

func routeHostReachable(host string) bool {
	return host == "" || host == "0.0.0.0" || host == "127.0.0.1"
}

func isDockerSocket(path string) bool {
	return path == "/var/run/docker.sock" || path == "/run/docker.sock"
}
