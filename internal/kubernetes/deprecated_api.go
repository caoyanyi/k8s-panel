package kubernetes

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

const (
	deprecatedAPIMetricName             = "apiserver_requested_deprecated_apis"
	deprecatedAPIMetricsAccept          = "text/plain;version=0.0.4"
	deprecatedAPIMaxResponseBytes int64 = 8 * 1024 * 1024
	deprecatedAPIMaxLineBytes           = 64 * 1024
	deprecatedAPIMaxSamples             = 512
	deprecatedAPIMaxLabelBytes          = 253
	deprecatedAPIMetricLabelCount       = 5
	deprecatedAPIMaxValueBytes          = 32
)

type deprecatedAPIRequestKey struct {
	group          string
	version        string
	resource       string
	subresource    string
	removedRelease string
}

func (c *Client) DeprecatedAPIRequests(ctx context.Context) ([]domain.KubernetesDeprecatedAPIRequest, error) {
	payload, _, err := c.getPayload(
		ctx,
		"/metrics",
		nil,
		deprecatedAPIMetricsAccept,
		deprecatedAPIMaxResponseBytes,
		false,
	)
	if err != nil {
		return nil, err
	}
	items, err := parseDeprecatedAPIMetrics(payload)
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		leftMajor, leftMinor, _ := parseDeprecatedAPIRemovedRelease(items[i].RemovedRelease)
		rightMajor, rightMinor, _ := parseDeprecatedAPIRemovedRelease(items[j].RemovedRelease)
		if leftMajor != rightMajor {
			return leftMajor < rightMajor
		}
		if leftMinor != rightMinor {
			return leftMinor < rightMinor
		}
		if items[i].Group != items[j].Group {
			return items[i].Group < items[j].Group
		}
		if items[i].Version != items[j].Version {
			return items[i].Version < items[j].Version
		}
		if items[i].Resource != items[j].Resource {
			return items[i].Resource < items[j].Resource
		}
		return items[i].Subresource < items[j].Subresource
	})
	return items, nil
}

func parseDeprecatedAPIMetrics(payload []byte) ([]domain.KubernetesDeprecatedAPIRequest, error) {
	items := make([]domain.KubernetesDeprecatedAPIRequest, 0)
	seen := make(map[deprecatedAPIRequestKey]struct{})
	scanner := bufio.NewScanner(bytes.NewReader(payload))
	scanner.Buffer(make([]byte, 4096), deprecatedAPIMaxLineBytes+1)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > deprecatedAPIMaxLineBytes {
			return nil, fmt.Errorf("Kubernetes metrics line exceeded safe limit: %w", domain.ErrUpstream)
		}
		line = strings.TrimSuffix(line, "\r")
		item, relevant, err := parseDeprecatedAPIMetricLine(line)
		if err != nil {
			return nil, err
		}
		if !relevant {
			continue
		}
		if len(items) >= deprecatedAPIMaxSamples {
			return nil, fmt.Errorf("Kubernetes deprecated API metrics exceeded safe sample limit: %w", domain.ErrUpstream)
		}
		key := deprecatedAPIRequestKey{
			group:          item.Group,
			version:        item.Version,
			resource:       item.Resource,
			subresource:    item.Subresource,
			removedRelease: item.RemovedRelease,
		}
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("duplicate Kubernetes deprecated API metric: %w", domain.ErrUpstream)
		}
		seen[key] = struct{}{}
		items = append(items, item)
	}
	if scanner.Err() != nil {
		return nil, fmt.Errorf("scan Kubernetes deprecated API metrics: %w", domain.ErrUpstream)
	}
	return items, nil
}

func parseDeprecatedAPIMetricLine(line string) (domain.KubernetesDeprecatedAPIRequest, bool, error) {
	var empty domain.KubernetesDeprecatedAPIRequest
	if !strings.HasPrefix(line, deprecatedAPIMetricName) {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), deprecatedAPIMetricName) {
			return empty, true, fmt.Errorf("invalid Kubernetes deprecated API metric: %w", domain.ErrUpstream)
		}
		return empty, false, nil
	}
	if len(line) > len(deprecatedAPIMetricName) && isPrometheusMetricNameCharacter(line[len(deprecatedAPIMetricName)]) {
		return empty, false, nil
	}
	position := len(deprecatedAPIMetricName)
	labels, next, err := parseDeprecatedAPILabelSet(line, position)
	if err != nil {
		return empty, true, err
	}
	if next >= len(line) || (line[next] != ' ' && line[next] != '\t') ||
		!validDeprecatedAPIMetricValue(strings.Trim(line[next:], " \t")) {
		return empty, true, fmt.Errorf("invalid Kubernetes deprecated API metric value: %w", domain.ErrUpstream)
	}
	item := domain.KubernetesDeprecatedAPIRequest{
		Group:          labels["group"],
		Version:        labels["version"],
		Resource:       labels["resource"],
		Subresource:    labels["subresource"],
		RemovedRelease: labels["removed_release"],
	}
	if len(labels) != deprecatedAPIMetricLabelCount ||
		domain.ValidateAPIServiceName(item.Version+"."+item.Group) != nil ||
		!validDeprecatedAPIResourceName(item.Resource, false) ||
		!validDeprecatedAPIResourceName(item.Subresource, true) {
		return empty, true, fmt.Errorf("invalid Kubernetes deprecated API metric labels: %w", domain.ErrUpstream)
	}
	if _, _, ok := parseDeprecatedAPIRemovedRelease(item.RemovedRelease); !ok {
		return empty, true, fmt.Errorf("invalid Kubernetes deprecated API removal release: %w", domain.ErrUpstream)
	}
	return item, true, nil
}

func parseDeprecatedAPILabelSet(line string, position int) (map[string]string, int, error) {
	labels := make(map[string]string, deprecatedAPIMetricLabelCount)
	if position >= len(line) || line[position] != '{' {
		return nil, 0, fmt.Errorf("invalid Kubernetes deprecated API metric label set: %w", domain.ErrUpstream)
	}
	position++
	for {
		position = skipPrometheusSpaces(line, position)
		if position >= len(line) || line[position] == '}' {
			return nil, 0, fmt.Errorf("invalid Kubernetes deprecated API metric label set: %w", domain.ErrUpstream)
		}
		nameStart := position
		for position < len(line) && isPrometheusLabelNameCharacter(line[position], position == nameStart) {
			position++
		}
		name := line[nameStart:position]
		position = skipPrometheusSpaces(line, position)
		if name == "" || position >= len(line) || line[position] != '=' {
			return nil, 0, fmt.Errorf("invalid Kubernetes deprecated API metric label: %w", domain.ErrUpstream)
		}
		if !isDeprecatedAPIMetricLabel(name) {
			return nil, 0, fmt.Errorf("unexpected Kubernetes deprecated API metric label: %w", domain.ErrUpstream)
		}
		if _, exists := labels[name]; exists {
			return nil, 0, fmt.Errorf("duplicate Kubernetes deprecated API metric label: %w", domain.ErrUpstream)
		}
		position = skipPrometheusSpaces(line, position+1)
		value, next, err := parsePrometheusLabelValue(line, position)
		if err != nil {
			return nil, 0, err
		}
		labels[name] = value
		position = skipPrometheusSpaces(line, next)
		if position >= len(line) {
			return nil, 0, fmt.Errorf("unterminated Kubernetes deprecated API metric: %w", domain.ErrUpstream)
		}
		switch line[position] {
		case '}':
			return labels, position + 1, nil
		case ',':
			position = skipPrometheusSpaces(line, position+1)
			if position >= len(line) || line[position] == '}' {
				return nil, 0, fmt.Errorf("invalid Kubernetes deprecated API metric label separator: %w", domain.ErrUpstream)
			}
		default:
			return nil, 0, fmt.Errorf("invalid Kubernetes deprecated API metric label separator: %w", domain.ErrUpstream)
		}
	}
}

func parsePrometheusLabelValue(line string, position int) (string, int, error) {
	if position >= len(line) || line[position] != '"' {
		return "", 0, fmt.Errorf("invalid Kubernetes deprecated API metric label value: %w", domain.ErrUpstream)
	}
	position++
	var value strings.Builder
	value.Grow(min(len(line)-position, deprecatedAPIMaxLabelBytes))
	for position < len(line) {
		character := line[position]
		position++
		switch character {
		case '"':
			return value.String(), position, nil
		case '\\':
			if position >= len(line) {
				return "", 0, fmt.Errorf("invalid Kubernetes deprecated API metric escape: %w", domain.ErrUpstream)
			}
			escaped := line[position]
			position++
			switch escaped {
			case '\\', '"':
				character = escaped
			case 'n':
				character = '\n'
			default:
				return "", 0, fmt.Errorf("invalid Kubernetes deprecated API metric escape: %w", domain.ErrUpstream)
			}
		}
		if value.Len() >= deprecatedAPIMaxLabelBytes {
			return "", 0, fmt.Errorf("Kubernetes deprecated API metric label exceeded safe limit: %w", domain.ErrUpstream)
		}
		value.WriteByte(character)
	}
	return "", 0, fmt.Errorf("unterminated Kubernetes deprecated API metric label: %w", domain.ErrUpstream)
}

func parseDeprecatedAPIRemovedRelease(value string) (int, int, bool) {
	major, next, ok := parseVersionNumber(value, 0)
	if !ok || next >= len(value) || value[next] != '.' {
		return 0, 0, false
	}
	minor, next, ok := parseVersionNumber(value, next+1)
	return major, minor, ok && next == len(value)
}

func validDeprecatedAPIResourceName(value string, allowEmpty bool) bool {
	if value == "" {
		return allowEmpty
	}
	if len(value) > 63 || !isLowerAlphaNumeric(value[0]) || !isLowerAlphaNumeric(value[len(value)-1]) {
		return false
	}
	for index := 1; index < len(value)-1; index++ {
		if !isLowerAlphaNumeric(value[index]) && value[index] != '-' {
			return false
		}
	}
	return true
}

func validDeprecatedAPIMetricValue(value string) bool {
	if value == "" || len(value) > deprecatedAPIMaxValueBytes {
		return false
	}
	position := 0
	if value[position] == '+' || value[position] == '-' {
		position++
		if position == len(value) {
			return false
		}
	}
	digitCount := 0
	for position < len(value) && value[position] >= '0' && value[position] <= '9' {
		position++
		digitCount++
	}
	if position < len(value) && value[position] == '.' {
		position++
		for position < len(value) && value[position] >= '0' && value[position] <= '9' {
			position++
			digitCount++
		}
	}
	if digitCount == 0 {
		return false
	}
	if position < len(value) && (value[position] == 'e' || value[position] == 'E') {
		position++
		if position < len(value) && (value[position] == '+' || value[position] == '-') {
			position++
		}
		exponentStart := position
		for position < len(value) && value[position] >= '0' && value[position] <= '9' {
			position++
		}
		if position == exponentStart {
			return false
		}
	}
	if position != len(value) {
		return false
	}
	parsed, ok := new(big.Rat).SetString(value)
	return ok && parsed.Cmp(big.NewRat(1, 1)) == 0
}

func isDeprecatedAPIMetricLabel(name string) bool {
	switch name {
	case "group", "version", "resource", "subresource", "removed_release":
		return true
	default:
		return false
	}
}

func isLowerAlphaNumeric(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= '0' && character <= '9'
}

func isPrometheusMetricNameCharacter(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' || character == '_' || character == ':'
}

func isPrometheusLabelNameCharacter(character byte, first bool) bool {
	if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character == '_' {
		return true
	}
	return !first && character >= '0' && character <= '9'
}

func skipPrometheusSpaces(value string, position int) int {
	for position < len(value) && (value[position] == ' ' || value[position] == '\t') {
		position++
	}
	return position
}
