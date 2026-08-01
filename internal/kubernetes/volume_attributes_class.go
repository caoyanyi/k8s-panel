package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

const (
	volumeAttributesClassCollectionPath         = "/apis/storage.k8s.io/v1/volumeattributesclasses"
	volumeAttributesClassListPageSize           = "250"
	volumeAttributesClassMaxListPages           = 4
	volumeAttributesClassMaxListItems           = 1000
	volumeAttributesClassMaxPageBytes     int64 = 2 * 1024 * 1024
	volumeAttributesClassMaxListBytes     int64 = 4 * 1024 * 1024
	volumeAttributesClassMaxContinueBytes       = 16 * 1024
)

func (c *Client) VolumeAttributesClasses(ctx context.Context) ([]domain.KubernetesVolumeAttributesClass, error) {
	query := url.Values{
		"includeObject": {"Metadata"},
		"limit":         {volumeAttributesClassListPageSize},
	}
	items := make([]domain.KubernetesVolumeAttributesClass, 0)
	seenNames := make(map[string]struct{})
	seenContinue := make(map[string]struct{})
	var totalBytes int64

	for page := 0; page < volumeAttributesClassMaxListPages; page++ {
		remainingBytes := volumeAttributesClassMaxListBytes - totalBytes
		if remainingBytes <= 0 {
			return nil, fmt.Errorf("Kubernetes VolumeAttributesClass list exceeded safe byte limit: %w", domain.ErrUpstream)
		}
		pageBytes := min(remainingBytes, volumeAttributesClassMaxPageBytes)
		payload, _, err := c.getPayload(
			ctx, volumeAttributesClassCollectionPath, query, kubernetesTableAccept, pageBytes, false,
		)
		if err != nil {
			return nil, err
		}
		totalBytes += int64(len(payload))

		var response kubernetesTable
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, fmt.Errorf("decode Kubernetes VolumeAttributesClass table: %w", domain.ErrUpstream)
		}
		if response.APIVersion != "meta.k8s.io/v1" || response.Kind != "Table" ||
			len(response.ColumnDefinitions) == 0 || len(response.ColumnDefinitions) > maxTableColumns {
			return nil, fmt.Errorf("unsupported Kubernetes VolumeAttributesClass table: %w", domain.ErrUpstream)
		}
		if len(response.Rows) > volumeAttributesClassMaxListItems-len(items) {
			return nil, fmt.Errorf("Kubernetes VolumeAttributesClass list exceeded safe item limit: %w", domain.ErrUpstream)
		}
		columns, err := volumeAttributesClassColumns(response.ColumnDefinitions)
		if err != nil {
			return nil, err
		}
		for _, row := range response.Rows {
			item, err := decodeVolumeAttributesClassTableRow(row, columns)
			if err != nil {
				return nil, err
			}
			if _, duplicate := seenNames[item.Name]; duplicate {
				return nil, fmt.Errorf("duplicate Kubernetes VolumeAttributesClass identity: %w", domain.ErrUpstream)
			}
			seenNames[item.Name] = struct{}{}
			items = append(items, item)
		}

		continuation := response.Metadata.Continue
		if continuation == "" {
			sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
			return items, nil
		}
		if !validVolumeAttributesClassContinue(continuation) {
			return nil, fmt.Errorf("invalid Kubernetes VolumeAttributesClass continuation token: %w", domain.ErrUpstream)
		}
		if _, duplicate := seenContinue[continuation]; duplicate {
			return nil, fmt.Errorf("repeated Kubernetes VolumeAttributesClass continuation token: %w", domain.ErrUpstream)
		}
		seenContinue[continuation] = struct{}{}
		query.Set("continue", continuation)
	}
	return nil, fmt.Errorf("Kubernetes VolumeAttributesClass list exceeded safe page limit: %w", domain.ErrUpstream)
}

func volumeAttributesClassColumns(columns []struct {
	Name string `json:"name"`
	Type string `json:"type"`
}) (map[string]int, error) {
	required := map[string]string{
		"Name":       "string",
		"DriverName": "string",
	}
	result := make(map[string]int, len(required))
	for index, column := range columns {
		if column.Name == "" || column.Type == "" || !validTableString(column.Name) || !validTableString(column.Type) {
			return nil, fmt.Errorf("invalid Kubernetes VolumeAttributesClass table column: %w", domain.ErrUpstream)
		}
		expectedType, wanted := required[column.Name]
		if !wanted {
			continue
		}
		if column.Type != expectedType {
			return nil, fmt.Errorf("invalid Kubernetes VolumeAttributesClass table column type: %w", domain.ErrUpstream)
		}
		if _, duplicate := result[column.Name]; duplicate {
			return nil, fmt.Errorf("duplicate Kubernetes VolumeAttributesClass table column: %w", domain.ErrUpstream)
		}
		result[column.Name] = index
	}
	if len(result) != len(required) {
		return nil, fmt.Errorf("missing Kubernetes VolumeAttributesClass table column: %w", domain.ErrUpstream)
	}
	return result, nil
}

func decodeVolumeAttributesClassTableRow(
	row kubernetesTableRow,
	columns map[string]int,
) (domain.KubernetesVolumeAttributesClass, error) {
	var empty domain.KubernetesVolumeAttributesClass
	if len(row.Cells) == 0 || len(row.Cells) > maxTableColumns {
		return empty, fmt.Errorf("invalid Kubernetes VolumeAttributesClass table row: %w", domain.ErrUpstream)
	}
	metadata, err := decodePartialObjectMetadataForScope(row.Object, false)
	if err != nil {
		return empty, err
	}
	name, err := storageStringCell(row, columns["Name"])
	if err != nil || name != metadata.Name || domain.ValidateVolumeAttributesClassName(name) != nil {
		return empty, fmt.Errorf("invalid Kubernetes VolumeAttributesClass identity: %w", domain.ErrUpstream)
	}
	driverName, err := storageStringCell(row, columns["DriverName"])
	if err != nil || domain.ValidateCSIDriverName(driverName) != nil {
		return empty, fmt.Errorf("invalid Kubernetes VolumeAttributesClass driver: %w", domain.ErrUpstream)
	}
	return domain.KubernetesVolumeAttributesClass{
		Name: name, DriverName: driverName, CreatedAt: metadata.CreationTimestamp,
	}, nil
}

func validVolumeAttributesClassContinue(value string) bool {
	return value != "" && len(value) <= volumeAttributesClassMaxContinueBytes && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}
