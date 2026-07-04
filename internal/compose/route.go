package compose

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type RouteTarget struct {
	ServiceName string
	Port        int
}

func InferDefaultRoute(path string) (RouteTarget, bool, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RouteTarget{}, false, "", err
	}

	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return RouteTarget{}, false, "", fmt.Errorf("invalid YAML in %s: %w", path, err)
	}

	root := documentNode(&document)
	if root == nil || root.Kind != yaml.MappingNode {
		return RouteTarget{}, false, "compose file must be a mapping with services", nil
	}
	servicesNode := routeMappingValue(root, "services")
	if servicesNode == nil || servicesNode.Kind != yaml.MappingNode {
		return RouteTarget{}, false, "compose file has no services to infer a route from", nil
	}

	candidates := make([]RouteTarget, 0)
	for i := 0; i < len(servicesNode.Content); i += 2 {
		serviceName := servicesNode.Content[i].Value
		serviceNode := servicesNode.Content[i+1]
		ports := inferredServicePorts(serviceNode)
		if len(ports) != 1 {
			continue
		}

		candidate := RouteTarget{ServiceName: serviceName, Port: ports[0]}
		if serviceName == "web" {
			return candidate, true, "", nil
		}
		candidates = append(candidates, candidate)
	}

	if len(candidates) == 1 {
		return candidates[0], true, "", nil
	}
	if len(candidates) > 1 {
		names := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			names = append(names, candidate.ServiceName)
		}
		sort.Strings(names)
		return RouteTarget{}, false, "ambiguous route: services " + strings.Join(names, ", ") + " each expose one host-published TCP port; attach manually with sshdock domains attach", nil
	}
	return RouteTarget{}, false, "no service exposes exactly one host-published TCP port; publish one routed service port or attach manually with sshdock domains attach", nil
}

func inferredServicePorts(serviceNode *yaml.Node) []int {
	if serviceNode == nil || serviceNode.Kind != yaml.MappingNode {
		return nil
	}
	portsNode := routeMappingValue(serviceNode, "ports")
	if portsNode == nil || portsNode.Kind != yaml.SequenceNode {
		return nil
	}

	var ports []int
	for _, item := range portsNode.Content {
		switch item.Kind {
		case yaml.ScalarNode:
			if port, ok := parseShortPort(item.Value); ok {
				ports = append(ports, port)
			}
		case yaml.MappingNode:
			if port, ok := parseLongPort(item); ok {
				ports = append(ports, port)
			}
		}
	}
	return ports
}

func parseShortPort(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	spec, protocol, hasProtocol := strings.Cut(value, "/")
	if hasProtocol && !strings.EqualFold(protocol, "tcp") {
		return 0, false
	}

	parts := strings.Split(spec, ":")
	var published string
	switch len(parts) {
	case 2:
		published = parts[0]
	case 3:
		published = parts[1]
	default:
		return 0, false
	}
	return parsePublishedPort(published)
}

func parseLongPort(node *yaml.Node) (int, bool) {
	published := mappingScalarValue(node, "published")
	target := mappingScalarValue(node, "target")
	protocol := mappingScalarValue(node, "protocol")
	if published == "" || target == "" {
		return 0, false
	}
	if protocol != "" && !strings.EqualFold(protocol, "tcp") {
		return 0, false
	}
	return parsePublishedPort(published)
}

func parsePublishedPort(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "-") {
		return 0, false
	}
	port, err := strconv.Atoi(value)
	if err != nil || port <= 0 || port > 65535 {
		return 0, false
	}
	return port, true
}

func routeMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func mappingScalarValue(node *yaml.Node, key string) string {
	value := routeMappingValue(node, key)
	if value == nil || value.Kind != yaml.ScalarNode {
		return ""
	}
	return strings.TrimSpace(value.Value)
}
