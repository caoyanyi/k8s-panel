package kubernetes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

const (
	nodeVersionTablePageSize                 = "250"
	nodeVersionMaxTablePages                 = 4
	nodeVersionMaxTableItems                 = 1000
	nodeVersionMaxPageBytes            int64 = 1024 * 1024
	nodeVersionMaxTableBytes           int64 = 2 * 1024 * 1024
	nodeVersionMaxVersionResponseBytes int64 = 64 * 1024
	nodeVersionMaxContinuationBytes          = 16 * 1024
	nodeVersionMaxVersionBytes               = 128
	nodeVersionMaxNumericPart                = 65535
)

type kubernetesComponentVersion struct {
	Major int
	Minor int
	Patch int
}

func (c *Client) NodeVersionSkew(ctx context.Context) (domain.KubernetesNodeVersionSkewReport, error) {
	apiServerVersion, parsedAPIServerVersion, err := c.observedAPIServerVersion(ctx)
	if err != nil {
		return domain.KubernetesNodeVersionSkewReport{}, err
	}
	nodes, err := c.listNodeVersionSkewTable(ctx, parsedAPIServerVersion)
	if err != nil {
		return domain.KubernetesNodeVersionSkewReport{}, err
	}
	return domain.KubernetesNodeVersionSkewReport{APIServerVersion: apiServerVersion, Nodes: nodes}, nil
}

func (c *Client) observedAPIServerVersion(
	ctx context.Context,
) (string, kubernetesComponentVersion, error) {
	payload, _, err := c.getPayload(
		ctx, "/version", nil, "application/json", nodeVersionMaxVersionResponseBytes, false,
	)
	if err != nil {
		return "", kubernetesComponentVersion{}, err
	}
	var response struct {
		GitVersion string `json:"gitVersion"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return "", kubernetesComponentVersion{}, fmt.Errorf("decode Kubernetes API Server version: %w", domain.ErrUpstream)
	}
	version, ok := parseKubernetesComponentVersion(response.GitVersion)
	if !ok {
		return "", kubernetesComponentVersion{}, fmt.Errorf("invalid Kubernetes API Server version: %w", domain.ErrUpstream)
	}
	return response.GitVersion, version, nil
}

func (c *Client) listNodeVersionSkewTable(
	ctx context.Context,
	apiServerVersion kubernetesComponentVersion,
) ([]domain.KubernetesNodeVersionSkew, error) {
	query := url.Values{
		"includeObject": {"None"},
		"limit":         {nodeVersionTablePageSize},
	}
	items := make([]domain.KubernetesNodeVersionSkew, 0)
	seenNames := make(map[string]struct{})
	seenContinue := make(map[string]struct{})
	var totalBytes int64

	for page := 0; page < nodeVersionMaxTablePages; page++ {
		remainingBytes := nodeVersionMaxTableBytes - totalBytes
		if remainingBytes <= 0 {
			return nil, fmt.Errorf("Kubernetes node version table exceeded safe byte limit: %w", domain.ErrUpstream)
		}
		payload, _, err := c.getPayload(
			ctx,
			"/api/v1/nodes",
			query,
			kubernetesTableAccept,
			min(remainingBytes, nodeVersionMaxPageBytes),
			false,
		)
		if err != nil {
			return nil, err
		}
		totalBytes += int64(len(payload))

		var response kubernetesTable
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, fmt.Errorf("decode Kubernetes node version table: %w", domain.ErrUpstream)
		}
		if response.APIVersion != "meta.k8s.io/v1" || response.Kind != "Table" ||
			len(response.ColumnDefinitions) == 0 || len(response.ColumnDefinitions) > maxTableColumns {
			return nil, fmt.Errorf("unsupported Kubernetes node version table: %w", domain.ErrUpstream)
		}
		if len(response.Rows) > nodeVersionMaxTableItems-len(items) {
			return nil, fmt.Errorf("Kubernetes node version table exceeded safe item limit: %w", domain.ErrUpstream)
		}
		nameColumn, versionColumn, err := nodeVersionTableColumns(response.ColumnDefinitions)
		if err != nil {
			return nil, err
		}
		for _, row := range response.Rows {
			item, err := decodeNodeVersionTableRow(row, len(response.ColumnDefinitions), nameColumn, versionColumn, apiServerVersion)
			if err != nil {
				return nil, err
			}
			if _, exists := seenNames[item.Name]; exists {
				return nil, fmt.Errorf("duplicate Kubernetes node version row: %w", domain.ErrUpstream)
			}
			seenNames[item.Name] = struct{}{}
			items = append(items, item)
		}

		continuation := response.Metadata.Continue
		if continuation == "" {
			sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
			return items, nil
		}
		if !validNodeVersionContinuation(continuation) {
			return nil, fmt.Errorf("invalid Kubernetes node version continuation token: %w", domain.ErrUpstream)
		}
		if _, exists := seenContinue[continuation]; exists {
			return nil, fmt.Errorf("repeated Kubernetes node version continuation token: %w", domain.ErrUpstream)
		}
		seenContinue[continuation] = struct{}{}
		if len(items) >= nodeVersionMaxTableItems {
			return nil, fmt.Errorf("Kubernetes node version table exceeded safe item limit: %w", domain.ErrUpstream)
		}
		query.Set("continue", continuation)
	}
	return nil, fmt.Errorf("Kubernetes node version table exceeded safe page limit: %w", domain.ErrUpstream)
}

func nodeVersionTableColumns(columns []struct {
	Name string `json:"name"`
	Type string `json:"type"`
}) (int, int, error) {
	for _, column := range columns {
		if !validTableString(column.Name) || !validTableString(column.Type) {
			return -1, -1, fmt.Errorf("invalid Kubernetes node version table column: %w", domain.ErrUpstream)
		}
	}
	nameColumn, err := configurationTableColumn(columns, "Name")
	if err != nil {
		return -1, -1, err
	}
	versionColumn, err := configurationTableColumn(columns, "Version")
	if err != nil {
		return -1, -1, err
	}
	if columns[nameColumn].Type != "string" || columns[versionColumn].Type != "string" {
		return -1, -1, fmt.Errorf("invalid Kubernetes node version table column type: %w", domain.ErrUpstream)
	}
	return nameColumn, versionColumn, nil
}

func decodeNodeVersionTableRow(
	row kubernetesTableRow,
	columnCount, nameColumn, versionColumn int,
	apiServerVersion kubernetesComponentVersion,
) (domain.KubernetesNodeVersionSkew, error) {
	if len(row.Cells) != columnCount || len(row.Cells) == 0 || len(row.Cells) > maxTableColumns ||
		nameColumn >= len(row.Cells) || versionColumn >= len(row.Cells) || !emptyTableRowObject(row.Object) {
		return domain.KubernetesNodeVersionSkew{}, fmt.Errorf("invalid Kubernetes node version table row: %w", domain.ErrUpstream)
	}
	name, err := nodeVersionStringCell(row.Cells[nameColumn], 253)
	if err != nil || domain.ValidateNodeName(name) != nil {
		return domain.KubernetesNodeVersionSkew{}, fmt.Errorf("invalid Kubernetes node version name: %w", domain.ErrUpstream)
	}
	kubeletVersionText, err := nodeVersionStringCell(row.Cells[versionColumn], nodeVersionMaxVersionBytes)
	if err != nil {
		return domain.KubernetesNodeVersionSkew{}, err
	}
	kubeletVersion, ok := parseKubernetesComponentVersion(kubeletVersionText)
	if !ok {
		return domain.KubernetesNodeVersionSkew{}, fmt.Errorf("invalid Kubernetes Kubelet version: %w", domain.ErrUpstream)
	}
	return classifyNodeVersion(name, kubeletVersionText, apiServerVersion, kubeletVersion), nil
}

func nodeVersionStringCell(raw json.RawMessage, maxBytes int) (string, error) {
	var value string
	if json.Unmarshal(raw, &value) != nil || value == "" || len(value) > maxBytes ||
		value != strings.TrimSpace(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("invalid Kubernetes node version table cell: %w", domain.ErrUpstream)
	}
	return value, nil
}

func emptyTableRowObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

func classifyNodeVersion(
	name, kubeletVersionText string,
	apiServerVersion, kubeletVersion kubernetesComponentVersion,
) domain.KubernetesNodeVersionSkew {
	item := domain.KubernetesNodeVersionSkew{
		Name: name, KubeletVersion: kubeletVersionText, Status: domain.NodeVersionMajorMismatch,
	}
	if apiServerVersion.Major != kubeletVersion.Major {
		return item
	}

	maximumMinorSkew := 3
	if kubeletVersion.Major < 1 || (kubeletVersion.Major == 1 && kubeletVersion.Minor < 25) {
		maximumMinorSkew = 2
	}
	minorSkew := apiServerVersion.Minor - kubeletVersion.Minor
	item.MinorSkew = minorSkew
	item.MaximumMinorSkew = maximumMinorSkew
	item.MinorSkewComparable = true
	switch {
	case minorSkew < 0:
		item.Status = domain.NodeVersionNewerThanServer
	case minorSkew == 0:
		item.Status = domain.NodeVersionSameMinor
	case minorSkew > maximumMinorSkew:
		item.Status = domain.NodeVersionOutsidePolicy
	case minorSkew == maximumMinorSkew:
		item.Status = domain.NodeVersionUpgradeBlocking
	default:
		item.Status = domain.NodeVersionWithinPolicy
	}
	return item
}

func parseKubernetesComponentVersion(value string) (kubernetesComponentVersion, bool) {
	var empty kubernetesComponentVersion
	if len(value) < 6 || len(value) > nodeVersionMaxVersionBytes || value[0] != 'v' {
		return empty, false
	}
	major, next, ok := parseVersionNumber(value, 1)
	if !ok || next >= len(value) || value[next] != '.' {
		return empty, false
	}
	minor, next, ok := parseVersionNumber(value, next+1)
	if !ok || next >= len(value) || value[next] != '.' {
		return empty, false
	}
	patch, next, ok := parseVersionNumber(value, next+1)
	if !ok || !validKubernetesVersionSuffix(value[next:]) {
		return empty, false
	}
	return kubernetesComponentVersion{Major: major, Minor: minor, Patch: patch}, true
}

func parseVersionNumber(value string, start int) (int, int, bool) {
	end := start
	for end < len(value) && value[end] >= '0' && value[end] <= '9' {
		end++
	}
	if end == start || (end-start > 1 && value[start] == '0') {
		return 0, start, false
	}
	parsed, err := strconv.ParseUint(value[start:end], 10, 16)
	if err != nil || parsed > nodeVersionMaxNumericPart {
		return 0, start, false
	}
	return int(parsed), end, true
}

func validKubernetesVersionSuffix(value string) bool {
	if value == "" {
		return true
	}
	if value[0] == '+' {
		return validVersionIdentifiers(value[1:])
	}
	if value[0] != '-' {
		return false
	}
	prerelease, build, hasBuild := strings.Cut(value[1:], "+")
	if !validVersionIdentifiers(prerelease) {
		return false
	}
	return !hasBuild || validVersionIdentifiers(build)
}

func validVersionIdentifiers(value string) bool {
	if value == "" {
		return false
	}
	for _, identifier := range strings.Split(value, ".") {
		if identifier == "" {
			return false
		}
		for _, character := range identifier {
			if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
				(character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}

func validNodeVersionContinuation(value string) bool {
	return value != "" && len(value) <= nodeVersionMaxContinuationBytes && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}
