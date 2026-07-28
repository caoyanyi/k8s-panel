package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

const (
	kubernetesTableAccept = "application/json;as=Table;g=meta.k8s.io;v=v1"
	maxTableColumns       = 64
	maxTableStringBytes   = 256
)

type configurationTableItem struct {
	Namespace string
	Name      string
	Type      string
	DataCount int
	CreatedAt time.Time
}

type kubernetesTable struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Continue string `json:"continue"`
	} `json:"metadata"`
	ColumnDefinitions []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"columnDefinitions"`
	Rows []kubernetesTableRow `json:"rows"`
}

type kubernetesTableRow struct {
	Cells  []json.RawMessage `json:"cells"`
	Object json.RawMessage   `json:"object"`
}

type partialObjectMetadata struct {
	Name              string
	Namespace         string
	CreationTimestamp time.Time
}

func (c *Client) ConfigMaps(ctx context.Context, namespace string) ([]domain.KubernetesConfigMap, error) {
	path := "/api/v1/configmaps"
	if namespace != "" {
		if err := domain.ValidateNamespace(namespace); err != nil {
			return nil, err
		}
		path = "/api/v1/namespaces/" + namespace + "/configmaps"
	}
	items, err := c.listConfigurationTable(ctx, path, namespace, false)
	if err != nil {
		return nil, err
	}
	configMaps := make([]domain.KubernetesConfigMap, 0, len(items))
	for _, item := range items {
		configMaps = append(configMaps, domain.KubernetesConfigMap{
			Namespace: item.Namespace,
			Name:      item.Name,
			DataCount: item.DataCount,
			CreatedAt: item.CreatedAt,
		})
	}
	return configMaps, nil
}

func (c *Client) Secrets(ctx context.Context, namespace string) ([]domain.KubernetesSecret, error) {
	if err := domain.ValidateNamespace(namespace); err != nil {
		return nil, err
	}
	items, err := c.listConfigurationTable(ctx, "/api/v1/namespaces/"+namespace+"/secrets", namespace, true)
	if err != nil {
		return nil, err
	}
	secrets := make([]domain.KubernetesSecret, 0, len(items))
	for _, item := range items {
		secrets = append(secrets, domain.KubernetesSecret{
			Namespace: item.Namespace,
			Name:      item.Name,
			Type:      item.Type,
			DataCount: item.DataCount,
			CreatedAt: item.CreatedAt,
		})
	}
	return secrets, nil
}

func (c *Client) listConfigurationTable(
	ctx context.Context,
	path, expectedNamespace string,
	includeType bool,
) ([]configurationTableItem, error) {
	query := url.Values{
		"includeObject": {"Metadata"},
		"limit":         {listPageSize},
	}
	items := make([]configurationTableItem, 0)
	var totalBytes int64
	for page := 0; page < maxListPages; page++ {
		payload, _, err := c.getPayload(ctx, path, query, kubernetesTableAccept, maxResponseBytes, false)
		if err != nil {
			return nil, err
		}
		if int64(len(payload)) > maxListBytes-totalBytes {
			return nil, fmt.Errorf("Kubernetes table exceeded safe byte limit: %w", domain.ErrUpstream)
		}
		totalBytes += int64(len(payload))

		var response kubernetesTable
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, fmt.Errorf("decode Kubernetes table: %w", domain.ErrUpstream)
		}
		if response.APIVersion != "meta.k8s.io/v1" || response.Kind != "Table" ||
			len(response.ColumnDefinitions) == 0 || len(response.ColumnDefinitions) > maxTableColumns {
			return nil, fmt.Errorf("unsupported Kubernetes table response: %w", domain.ErrUpstream)
		}
		if len(response.Rows) > maxListItems-len(items) {
			return nil, fmt.Errorf("Kubernetes table exceeded safe item limit: %w", domain.ErrUpstream)
		}
		dataColumn, err := configurationTableColumn(response.ColumnDefinitions, "Data")
		if err != nil {
			return nil, err
		}
		typeColumn := -1
		if includeType {
			typeColumn, err = configurationTableColumn(response.ColumnDefinitions, "Type")
			if err != nil {
				return nil, err
			}
		}
		for _, row := range response.Rows {
			item, err := decodeConfigurationTableRow(row, dataColumn, typeColumn)
			if err != nil {
				return nil, err
			}
			if expectedNamespace != "" && item.Namespace != expectedNamespace {
				return nil, fmt.Errorf("Kubernetes table row exceeded namespace scope: %w", domain.ErrUpstream)
			}
			items = append(items, item)
		}
		if response.Metadata.Continue == "" {
			sort.Slice(items, func(i, j int) bool {
				if items[i].Namespace != items[j].Namespace {
					return items[i].Namespace < items[j].Namespace
				}
				return items[i].Name < items[j].Name
			})
			return items, nil
		}
		query.Set("continue", response.Metadata.Continue)
	}
	return nil, fmt.Errorf("Kubernetes table exceeded safe page limit: %w", domain.ErrUpstream)
}

func configurationTableColumn(columns []struct {
	Name string `json:"name"`
	Type string `json:"type"`
}, name string) (int, error) {
	index := -1
	for candidate, column := range columns {
		if column.Name != name {
			continue
		}
		if index >= 0 {
			return -1, fmt.Errorf("duplicate Kubernetes table column: %w", domain.ErrUpstream)
		}
		index = candidate
	}
	if index < 0 {
		return -1, fmt.Errorf("missing Kubernetes table column: %w", domain.ErrUpstream)
	}
	return index, nil
}

func decodeConfigurationTableRow(row kubernetesTableRow, dataColumn, typeColumn int) (configurationTableItem, error) {
	if len(row.Cells) == 0 || len(row.Cells) > maxTableColumns || dataColumn >= len(row.Cells) || typeColumn >= len(row.Cells) {
		return configurationTableItem{}, fmt.Errorf("invalid Kubernetes table row: %w", domain.ErrUpstream)
	}
	metadata, err := decodePartialObjectMetadata(row.Object)
	if err != nil {
		return configurationTableItem{}, err
	}
	var dataCount int
	if err := json.Unmarshal(row.Cells[dataColumn], &dataCount); err != nil || dataCount < 0 {
		return configurationTableItem{}, fmt.Errorf("invalid Kubernetes table data count: %w", domain.ErrUpstream)
	}
	item := configurationTableItem{
		Namespace: metadata.Namespace,
		Name:      metadata.Name,
		DataCount: dataCount,
		CreatedAt: metadata.CreationTimestamp,
	}
	if typeColumn >= 0 {
		if err := json.Unmarshal(row.Cells[typeColumn], &item.Type); err != nil || !validTableString(item.Type) {
			return configurationTableItem{}, fmt.Errorf("invalid Kubernetes table type: %w", domain.ErrUpstream)
		}
		if item.Type == "" {
			item.Type = "Opaque"
		}
	}
	return item, nil
}

func decodePartialObjectMetadata(raw json.RawMessage) (partialObjectMetadata, error) {
	return decodePartialObjectMetadataForScope(raw, true)
}

func decodePartialObjectMetadataForScope(raw json.RawMessage, namespaced bool) (partialObjectMetadata, error) {
	var empty partialObjectMetadata
	var fields map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &fields) != nil {
		return empty, fmt.Errorf("decode Kubernetes table metadata: %w", domain.ErrUpstream)
	}
	for field := range fields {
		if field != "apiVersion" && field != "kind" && field != "metadata" {
			return empty, fmt.Errorf("unsafe Kubernetes table row object: %w", domain.ErrUpstream)
		}
	}
	var envelope struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name              string    `json:"name"`
			Namespace         string    `json:"namespace"`
			CreationTimestamp time.Time `json:"creationTimestamp"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.APIVersion != "meta.k8s.io/v1" || envelope.Kind != "PartialObjectMetadata" {
		return empty, fmt.Errorf("unsupported Kubernetes table row object: %w", domain.ErrUpstream)
	}
	if !validKubernetesMetadataString(envelope.Metadata.Name) || envelope.Metadata.CreationTimestamp.IsZero() {
		return empty, fmt.Errorf("invalid Kubernetes table metadata: %w", domain.ErrUpstream)
	}
	if namespaced {
		if domain.ValidateNamespace(envelope.Metadata.Namespace) != nil {
			return empty, fmt.Errorf("invalid Kubernetes table namespace: %w", domain.ErrUpstream)
		}
	} else if envelope.Metadata.Namespace != "" {
		return empty, fmt.Errorf("cluster-scoped Kubernetes table row contains a namespace: %w", domain.ErrUpstream)
	}
	return partialObjectMetadata{
		Name:              envelope.Metadata.Name,
		Namespace:         envelope.Metadata.Namespace,
		CreationTimestamp: envelope.Metadata.CreationTimestamp,
	}, nil
}

func validKubernetesMetadataString(value string) bool {
	return value != "" && len(value) <= 253 && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

func validTableString(value string) bool {
	return len(value) <= maxTableStringBytes && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}
