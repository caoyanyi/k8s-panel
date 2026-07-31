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
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	csiStorageCapacityCollectionPath         = "/apis/storage.k8s.io/v1/csistoragecapacities"
	csiStorageCapacityListPageSize           = "250"
	csiStorageCapacityMaxListPages           = 4
	csiStorageCapacityMaxListItems           = 1000
	csiStorageCapacityMaxPageBytes     int64 = 2 * 1024 * 1024
	csiStorageCapacityMaxListBytes     int64 = 4 * 1024 * 1024
	csiStorageCapacityMaxContinueBytes       = 16 * 1024
	csiStorageCapacityMaxQuantityBytes       = 64
)

func (c *Client) CSIStorageCapacities(
	ctx context.Context,
	namespace string,
) ([]domain.KubernetesCSIStorageCapacity, error) {
	path := csiStorageCapacityCollectionPath
	if namespace != "" {
		if err := domain.ValidateNamespace(namespace); err != nil {
			return nil, err
		}
		path = "/apis/storage.k8s.io/v1/namespaces/" + namespace + "/csistoragecapacities"
	}
	query := url.Values{
		"includeObject": {"Metadata"},
		"limit":         {csiStorageCapacityListPageSize},
	}
	items := make([]domain.KubernetesCSIStorageCapacity, 0)
	seenIdentities := make(map[string]struct{})
	seenContinue := make(map[string]struct{})
	var totalBytes int64

	for page := 0; page < csiStorageCapacityMaxListPages; page++ {
		remainingBytes := csiStorageCapacityMaxListBytes - totalBytes
		if remainingBytes <= 0 {
			return nil, fmt.Errorf("Kubernetes CSIStorageCapacity list exceeded safe byte limit: %w", domain.ErrUpstream)
		}
		pageBytes := min(remainingBytes, csiStorageCapacityMaxPageBytes)
		payload, _, err := c.getPayload(ctx, path, query, kubernetesTableAccept, pageBytes, false)
		if err != nil {
			return nil, err
		}
		totalBytes += int64(len(payload))

		var response kubernetesTable
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, fmt.Errorf("decode Kubernetes CSIStorageCapacity table: %w", domain.ErrUpstream)
		}
		if response.APIVersion != "meta.k8s.io/v1" || response.Kind != "Table" ||
			len(response.ColumnDefinitions) == 0 || len(response.ColumnDefinitions) > maxTableColumns {
			return nil, fmt.Errorf("unsupported Kubernetes CSIStorageCapacity table: %w", domain.ErrUpstream)
		}
		if len(response.Rows) > csiStorageCapacityMaxListItems-len(items) {
			return nil, fmt.Errorf("Kubernetes CSIStorageCapacity list exceeded safe item limit: %w", domain.ErrUpstream)
		}
		columns, err := csiStorageCapacityColumns(response.ColumnDefinitions)
		if err != nil {
			return nil, err
		}
		for _, row := range response.Rows {
			item, err := decodeCSIStorageCapacityTableRow(row, columns, namespace)
			if err != nil {
				return nil, err
			}
			identity := item.Namespace + "\x00" + item.Name
			if _, duplicate := seenIdentities[identity]; duplicate {
				return nil, fmt.Errorf("duplicate Kubernetes CSIStorageCapacity identity: %w", domain.ErrUpstream)
			}
			seenIdentities[identity] = struct{}{}
			items = append(items, item)
		}

		continuation := response.Metadata.Continue
		if continuation == "" {
			sort.Slice(items, func(i, j int) bool {
				if items[i].Namespace != items[j].Namespace {
					return items[i].Namespace < items[j].Namespace
				}
				if items[i].StorageClass != items[j].StorageClass {
					return items[i].StorageClass < items[j].StorageClass
				}
				return items[i].Name < items[j].Name
			})
			return items, nil
		}
		if !validCSIStorageCapacityContinue(continuation) {
			return nil, fmt.Errorf("invalid Kubernetes CSIStorageCapacity continuation token: %w", domain.ErrUpstream)
		}
		if _, duplicate := seenContinue[continuation]; duplicate {
			return nil, fmt.Errorf("repeated Kubernetes CSIStorageCapacity continuation token: %w", domain.ErrUpstream)
		}
		seenContinue[continuation] = struct{}{}
		query.Set("continue", continuation)
	}
	return nil, fmt.Errorf("Kubernetes CSIStorageCapacity list exceeded safe page limit: %w", domain.ErrUpstream)
}

func csiStorageCapacityColumns(columns []struct {
	Name string `json:"name"`
	Type string `json:"type"`
}) (map[string]int, error) {
	required := map[string]string{
		"Name":             "string",
		"StorageClassName": "string",
		"Capacity":         "string",
	}
	result := make(map[string]int, len(required))
	for index, column := range columns {
		if column.Name == "" || column.Type == "" || !validTableString(column.Name) || !validTableString(column.Type) {
			return nil, fmt.Errorf("invalid Kubernetes CSIStorageCapacity table column: %w", domain.ErrUpstream)
		}
		expectedType, wanted := required[column.Name]
		if !wanted {
			continue
		}
		if column.Type != expectedType {
			return nil, fmt.Errorf("invalid Kubernetes CSIStorageCapacity table column type: %w", domain.ErrUpstream)
		}
		if _, duplicate := result[column.Name]; duplicate {
			return nil, fmt.Errorf("duplicate Kubernetes CSIStorageCapacity table column: %w", domain.ErrUpstream)
		}
		result[column.Name] = index
	}
	if len(result) != len(required) {
		return nil, fmt.Errorf("missing Kubernetes CSIStorageCapacity table column: %w", domain.ErrUpstream)
	}
	return result, nil
}

func decodeCSIStorageCapacityTableRow(
	row kubernetesTableRow,
	columns map[string]int,
	expectedNamespace string,
) (domain.KubernetesCSIStorageCapacity, error) {
	var empty domain.KubernetesCSIStorageCapacity
	if len(row.Cells) == 0 || len(row.Cells) > maxTableColumns {
		return empty, fmt.Errorf("invalid Kubernetes CSIStorageCapacity table row: %w", domain.ErrUpstream)
	}
	metadata, err := decodePartialObjectMetadataForScope(row.Object, true)
	if err != nil {
		return empty, err
	}
	if expectedNamespace != "" && metadata.Namespace != expectedNamespace {
		return empty, fmt.Errorf("Kubernetes CSIStorageCapacity row exceeded namespace scope: %w", domain.ErrUpstream)
	}
	name, err := storageStringCell(row, columns["Name"])
	if err != nil || name != metadata.Name || domain.ValidateCSIStorageCapacityName(name) != nil {
		return empty, fmt.Errorf("invalid Kubernetes CSIStorageCapacity identity: %w", domain.ErrUpstream)
	}
	storageClass, err := storageStringCell(row, columns["StorageClassName"])
	if err != nil || domain.ValidateStorageClassName(storageClass) != nil {
		return empty, fmt.Errorf("invalid Kubernetes CSIStorageCapacity storage class: %w", domain.ErrUpstream)
	}
	capacityCell, err := storageStringCell(row, columns["Capacity"])
	if err != nil {
		return empty, fmt.Errorf("invalid Kubernetes CSIStorageCapacity capacity: %w", domain.ErrUpstream)
	}
	capacity, err := normalizeCSIStorageCapacityQuantity(capacityCell)
	if err != nil {
		return empty, err
	}
	return domain.KubernetesCSIStorageCapacity{
		Namespace: metadata.Namespace, Name: name, StorageClass: storageClass,
		Capacity: capacity, CreatedAt: metadata.CreationTimestamp,
	}, nil
}

func normalizeCSIStorageCapacityQuantity(value string) (string, error) {
	if value == "<unset>" {
		return "", nil
	}
	if value == "" || len(value) > csiStorageCapacityMaxQuantityBytes || value != strings.TrimSpace(value) ||
		strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("invalid Kubernetes CSIStorageCapacity capacity: %w", domain.ErrUpstream)
	}
	quantity, err := resource.ParseQuantity(value)
	if err != nil || quantity.Sign() < 0 {
		return "", fmt.Errorf("invalid Kubernetes CSIStorageCapacity capacity: %w", domain.ErrUpstream)
	}
	canonical := quantity.String()
	if canonical == "" || len(canonical) > csiStorageCapacityMaxQuantityBytes {
		return "", fmt.Errorf("invalid Kubernetes CSIStorageCapacity capacity: %w", domain.ErrUpstream)
	}
	return canonical, nil
}

func validCSIStorageCapacityContinue(value string) bool {
	return value != "" && len(value) <= csiStorageCapacityMaxContinueBytes && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}
