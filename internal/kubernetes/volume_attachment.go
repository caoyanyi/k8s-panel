package kubernetes

import (
	"bytes"
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
	volumeAttachmentCollectionPath         = "/apis/storage.k8s.io/v1/volumeattachments"
	volumeAttachmentListPageSize           = "250"
	volumeAttachmentMaxListPages           = 4
	volumeAttachmentMaxListItems           = 1000
	volumeAttachmentMaxPageBytes     int64 = 2 * 1024 * 1024
	volumeAttachmentMaxListBytes     int64 = 4 * 1024 * 1024
	volumeAttachmentMaxContinueBytes       = 16 * 1024
)

func (c *Client) VolumeAttachments(ctx context.Context) ([]domain.KubernetesVolumeAttachment, error) {
	query := url.Values{
		"includeObject": {"Metadata"},
		"limit":         {volumeAttachmentListPageSize},
	}
	items := make([]domain.KubernetesVolumeAttachment, 0)
	seenNames := make(map[string]struct{})
	seenContinue := make(map[string]struct{})
	var totalBytes int64

	for page := 0; page < volumeAttachmentMaxListPages; page++ {
		remainingBytes := volumeAttachmentMaxListBytes - totalBytes
		if remainingBytes <= 0 {
			return nil, fmt.Errorf("Kubernetes VolumeAttachment list exceeded safe byte limit: %w", domain.ErrUpstream)
		}
		pageBytes := min(remainingBytes, volumeAttachmentMaxPageBytes)
		payload, _, err := c.getPayload(
			ctx, volumeAttachmentCollectionPath, query, kubernetesTableAccept, pageBytes, false,
		)
		if err != nil {
			return nil, err
		}
		totalBytes += int64(len(payload))

		var response kubernetesTable
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, fmt.Errorf("decode Kubernetes VolumeAttachment table: %w", domain.ErrUpstream)
		}
		if response.APIVersion != "meta.k8s.io/v1" || response.Kind != "Table" ||
			len(response.ColumnDefinitions) == 0 || len(response.ColumnDefinitions) > maxTableColumns {
			return nil, fmt.Errorf("unsupported Kubernetes VolumeAttachment table: %w", domain.ErrUpstream)
		}
		if len(response.Rows) > volumeAttachmentMaxListItems-len(items) {
			return nil, fmt.Errorf("Kubernetes VolumeAttachment list exceeded safe item limit: %w", domain.ErrUpstream)
		}
		columns, err := volumeAttachmentColumns(response.ColumnDefinitions)
		if err != nil {
			return nil, err
		}
		for _, row := range response.Rows {
			item, err := decodeVolumeAttachmentTableRow(row, columns)
			if err != nil {
				return nil, err
			}
			if _, duplicate := seenNames[item.Name]; duplicate {
				return nil, fmt.Errorf("duplicate Kubernetes VolumeAttachment identity: %w", domain.ErrUpstream)
			}
			seenNames[item.Name] = struct{}{}
			items = append(items, item)
		}

		continuation := response.Metadata.Continue
		if continuation == "" {
			sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
			return items, nil
		}
		if !validVolumeAttachmentContinue(continuation) {
			return nil, fmt.Errorf("invalid Kubernetes VolumeAttachment continuation token: %w", domain.ErrUpstream)
		}
		if _, duplicate := seenContinue[continuation]; duplicate {
			return nil, fmt.Errorf("repeated Kubernetes VolumeAttachment continuation token: %w", domain.ErrUpstream)
		}
		seenContinue[continuation] = struct{}{}
		query.Set("continue", continuation)
	}
	return nil, fmt.Errorf("Kubernetes VolumeAttachment list exceeded safe page limit: %w", domain.ErrUpstream)
}

func volumeAttachmentColumns(columns []struct {
	Name string `json:"name"`
	Type string `json:"type"`
}) (map[string]int, error) {
	required := map[string]string{
		"Name":     "string",
		"Attacher": "string",
		"PV":       "string",
		"Node":     "string",
		"Attached": "boolean",
	}
	result := make(map[string]int, len(required))
	for index, column := range columns {
		if column.Name == "" || column.Type == "" || !validTableString(column.Name) || !validTableString(column.Type) {
			return nil, fmt.Errorf("invalid Kubernetes VolumeAttachment table column: %w", domain.ErrUpstream)
		}
		expectedType, wanted := required[column.Name]
		if !wanted {
			continue
		}
		if column.Type != expectedType {
			return nil, fmt.Errorf("invalid Kubernetes VolumeAttachment table column type: %w", domain.ErrUpstream)
		}
		if _, duplicate := result[column.Name]; duplicate {
			return nil, fmt.Errorf("duplicate Kubernetes VolumeAttachment table column: %w", domain.ErrUpstream)
		}
		result[column.Name] = index
	}
	if len(result) != len(required) {
		return nil, fmt.Errorf("missing Kubernetes VolumeAttachment table column: %w", domain.ErrUpstream)
	}
	return result, nil
}

func decodeVolumeAttachmentTableRow(
	row kubernetesTableRow,
	columns map[string]int,
) (domain.KubernetesVolumeAttachment, error) {
	var empty domain.KubernetesVolumeAttachment
	if len(row.Cells) == 0 || len(row.Cells) > maxTableColumns {
		return empty, fmt.Errorf("invalid Kubernetes VolumeAttachment table row: %w", domain.ErrUpstream)
	}
	metadata, err := decodePartialObjectMetadataForScope(row.Object, false)
	if err != nil {
		return empty, err
	}
	name, err := storageStringCell(row, columns["Name"])
	if err != nil || name != metadata.Name || domain.ValidateVolumeAttachmentName(name) != nil {
		return empty, fmt.Errorf("invalid Kubernetes VolumeAttachment identity: %w", domain.ErrUpstream)
	}
	attacher, err := storageStringCell(row, columns["Attacher"])
	if err != nil || attacher == "" {
		return empty, fmt.Errorf("invalid Kubernetes VolumeAttachment attacher: %w", domain.ErrUpstream)
	}
	persistentVolume, err := storageStringCell(row, columns["PV"])
	if err != nil || (persistentVolume != "" && domain.ValidatePersistentVolumeName(persistentVolume) != nil) {
		return empty, fmt.Errorf("invalid Kubernetes VolumeAttachment persistent volume: %w", domain.ErrUpstream)
	}
	node, err := storageStringCell(row, columns["Node"])
	if err != nil || domain.ValidateNodeName(node) != nil {
		return empty, fmt.Errorf("invalid Kubernetes VolumeAttachment node: %w", domain.ErrUpstream)
	}
	attached, err := volumeAttachmentBoolCell(row, columns["Attached"])
	if err != nil {
		return empty, fmt.Errorf("invalid Kubernetes VolumeAttachment attached status: %w", domain.ErrUpstream)
	}
	status := domain.VolumeAttachmentAttaching
	if metadata.DeletionTimestamp != nil {
		status = domain.VolumeAttachmentDetaching
	} else if attached {
		status = domain.VolumeAttachmentAttached
	}
	return domain.KubernetesVolumeAttachment{
		Name: name, Attacher: attacher, PersistentVolume: persistentVolume, Node: node,
		Status: status, CreatedAt: metadata.CreationTimestamp,
	}, nil
}

func volumeAttachmentBoolCell(row kubernetesTableRow, index int) (bool, error) {
	if index < 0 || index >= len(row.Cells) {
		return false, domain.ErrUpstream
	}
	trimmed := bytes.TrimSpace(row.Cells[index])
	if bytes.Equal(trimmed, []byte("true")) {
		return true, nil
	}
	if bytes.Equal(trimmed, []byte("false")) {
		return false, nil
	}
	return false, domain.ErrUpstream
}

func validVolumeAttachmentContinue(value string) bool {
	return value != "" && len(value) <= volumeAttachmentMaxContinueBytes && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}
