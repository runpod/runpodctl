package template

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/runpod/runpodctl/internal/api"
)

func parsePortLabels(raw string) ([]api.TemplatePortConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []api.TemplatePortConfig{}, nil
	}

	var labels []api.TemplatePortConfig
	switch raw[0] {
	case '{':
		var values map[string]string
		if err := json.Unmarshal([]byte(raw), &values); err != nil {
			return nil, fmt.Errorf("invalid port labels json: %w", err)
		}
		ports := make([]string, 0, len(values))
		for port := range values {
			ports = append(ports, port)
		}
		sort.Slice(ports, func(i, j int) bool {
			left, leftErr := strconv.Atoi(strings.TrimSpace(strings.SplitN(ports[i], "/", 2)[0]))
			right, rightErr := strconv.Atoi(strings.TrimSpace(strings.SplitN(ports[j], "/", 2)[0]))
			if leftErr == nil && rightErr == nil && left != right {
				return left < right
			}
			return ports[i] < ports[j]
		})
		for _, port := range ports {
			labels = append(labels, api.TemplatePortConfig{Port: port, Name: values[port]})
		}
	case '[':
		if err := json.Unmarshal([]byte(raw), &labels); err != nil {
			return nil, fmt.Errorf("invalid port labels json: %w", err)
		}
	default:
		for _, item := range strings.Split(raw, ",") {
			parts := strings.SplitN(item, "=", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid port label %q: expected port=name", strings.TrimSpace(item))
			}
			labels = append(labels, api.TemplatePortConfig{Port: parts[0], Name: parts[1]})
		}
	}

	seen := make(map[string]struct{}, len(labels))
	for i := range labels {
		port, err := api.NormalizePort(labels[i].Port)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSpace(labels[i].Name)
		if name == "" {
			return nil, fmt.Errorf("port label name is required for port %s", port)
		}
		if _, exists := seen[port]; exists {
			return nil, fmt.Errorf("duplicate port label for %s", port)
		}
		seen[port] = struct{}{}
		labels[i] = api.TemplatePortConfig{Port: port, Name: name}
	}

	return labels, nil
}

func validatePortLabelsAgainstPorts(labels []api.TemplatePortConfig, rawPorts string) error {
	available := make(map[string]struct{})
	for _, value := range strings.Split(rawPorts, ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		port, err := api.NormalizePort(value)
		if err != nil {
			return fmt.Errorf("invalid --ports value %q: %w", value, err)
		}
		available[port] = struct{}{}
	}

	for _, label := range labels {
		if _, exists := available[label.Port]; !exists {
			return fmt.Errorf("port label %s does not match any value in --ports", label.Port)
		}
	}

	return nil
}
