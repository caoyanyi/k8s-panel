package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

type storageTableKind uint8

const (
	storageTablePersistentVolumeClaim storageTableKind = iota
	storageTablePersistentVolume
	storageTableStorageClass
)

type storageTableItem struct {
	Namespace            string
	Name                 string
	Status               string
	Volume               string
	Claim                string
	Capacity             string
	AccessModes          string
	StorageClass         string
	ReclaimPolicy        string
	VolumeMode           string
	Provisioner          string
	VolumeBindingMode    string
	AllowVolumeExpansion bool
	Default              bool
	CreatedAt            time.Time
}

func (c *Client) PersistentVolumeClaims(
	ctx context.Context,
	namespace string,
) ([]domain.KubernetesPersistentVolumeClaim, error) {
	path := "/api/v1/persistentvolumeclaims"
	if namespace != "" {
		if err := domain.ValidateNamespace(namespace); err != nil {
			return nil, err
		}
		path = "/api/v1/namespaces/" + namespace + "/persistentvolumeclaims"
	}
	items, err := c.listStorageTable(ctx, path, namespace, storageTablePersistentVolumeClaim)
	if err != nil {
		return nil, err
	}
	claims := make([]domain.KubernetesPersistentVolumeClaim, 0, len(items))
	for _, item := range items {
		claims = append(claims, domain.KubernetesPersistentVolumeClaim{
			Namespace: item.Namespace, Name: item.Name, Status: item.Status, Volume: item.Volume,
			Capacity: item.Capacity, AccessModes: item.AccessModes, StorageClass: item.StorageClass,
			VolumeMode: item.VolumeMode, CreatedAt: item.CreatedAt,
		})
	}
	return claims, nil
}

func (c *Client) PersistentVolumes(ctx context.Context) ([]domain.KubernetesPersistentVolume, error) {
	items, err := c.listStorageTable(ctx, "/api/v1/persistentvolumes", "", storageTablePersistentVolume)
	if err != nil {
		return nil, err
	}
	volumes := make([]domain.KubernetesPersistentVolume, 0, len(items))
	for _, item := range items {
		volumes = append(volumes, domain.KubernetesPersistentVolume{
			Name: item.Name, Status: item.Status, Claim: item.Claim, Capacity: item.Capacity,
			AccessModes: item.AccessModes, StorageClass: item.StorageClass, ReclaimPolicy: item.ReclaimPolicy,
			VolumeMode: item.VolumeMode, CreatedAt: item.CreatedAt,
		})
	}
	return volumes, nil
}

func (c *Client) StorageClasses(ctx context.Context) ([]domain.KubernetesStorageClass, error) {
	items, err := c.listStorageTable(ctx, "/apis/storage.k8s.io/v1/storageclasses", "", storageTableStorageClass)
	if err != nil {
		return nil, err
	}
	classes := make([]domain.KubernetesStorageClass, 0, len(items))
	for _, item := range items {
		classes = append(classes, domain.KubernetesStorageClass{
			Name: item.Name, Provisioner: item.Provisioner, ReclaimPolicy: item.ReclaimPolicy,
			VolumeBindingMode: item.VolumeBindingMode, AllowVolumeExpansion: item.AllowVolumeExpansion,
			Default: item.Default, CreatedAt: item.CreatedAt,
		})
	}
	return classes, nil
}

func (c *Client) listStorageTable(
	ctx context.Context,
	path, expectedNamespace string,
	kind storageTableKind,
) ([]storageTableItem, error) {
	query := url.Values{
		"includeObject": {"Metadata"},
		"limit":         {listPageSize},
	}
	items := make([]storageTableItem, 0)
	var totalBytes int64
	for page := 0; page < maxListPages; page++ {
		payload, _, err := c.getPayload(ctx, path, query, kubernetesTableAccept, maxResponseBytes, false)
		if err != nil {
			return nil, err
		}
		if int64(len(payload)) > maxListBytes-totalBytes {
			return nil, fmt.Errorf("Kubernetes storage table exceeded safe byte limit: %w", domain.ErrUpstream)
		}
		totalBytes += int64(len(payload))

		var response kubernetesTable
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, fmt.Errorf("decode Kubernetes storage table: %w", domain.ErrUpstream)
		}
		if response.APIVersion != "meta.k8s.io/v1" || response.Kind != "Table" ||
			len(response.ColumnDefinitions) == 0 || len(response.ColumnDefinitions) > maxTableColumns {
			return nil, fmt.Errorf("unsupported Kubernetes storage table response: %w", domain.ErrUpstream)
		}
		if len(response.Rows) > maxListItems-len(items) {
			return nil, fmt.Errorf("Kubernetes storage table exceeded safe item limit: %w", domain.ErrUpstream)
		}
		columns, err := storageColumns(response.ColumnDefinitions, kind)
		if err != nil {
			return nil, err
		}
		for _, row := range response.Rows {
			item, err := decodeStorageTableRow(row, columns, kind)
			if err != nil {
				return nil, err
			}
			if expectedNamespace != "" && item.Namespace != expectedNamespace {
				return nil, fmt.Errorf("Kubernetes storage table row exceeded namespace scope: %w", domain.ErrUpstream)
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
	return nil, fmt.Errorf("Kubernetes storage table exceeded safe page limit: %w", domain.ErrUpstream)
}

func storageColumns(columns []struct {
	Name string `json:"name"`
	Type string `json:"type"`
}, kind storageTableKind) (map[string]int, error) {
	required := []string{"Name"}
	switch kind {
	case storageTablePersistentVolumeClaim:
		required = append(required, "Status", "Volume", "Capacity", "Access Modes", "StorageClass", "VolumeMode")
	case storageTablePersistentVolume:
		required = append(required, "Status", "Claim", "Capacity", "Access Modes", "StorageClass", "Reclaim Policy", "VolumeMode")
	case storageTableStorageClass:
		required = append(required, "Provisioner", "ReclaimPolicy", "VolumeBindingMode", "AllowVolumeExpansion")
	default:
		return nil, fmt.Errorf("unsupported Kubernetes storage table kind: %w", domain.ErrUpstream)
	}
	result := make(map[string]int, len(required))
	for _, name := range required {
		index, err := configurationTableColumn(columns, name)
		if err != nil {
			return nil, err
		}
		result[name] = index
	}
	return result, nil
}

func decodeStorageTableRow(
	row kubernetesTableRow,
	columns map[string]int,
	kind storageTableKind,
) (storageTableItem, error) {
	if len(row.Cells) == 0 || len(row.Cells) > maxTableColumns {
		return storageTableItem{}, fmt.Errorf("invalid Kubernetes storage table row: %w", domain.ErrUpstream)
	}
	namespaced := kind == storageTablePersistentVolumeClaim
	metadata, err := decodePartialObjectMetadataForScope(row.Object, namespaced)
	if err != nil {
		return storageTableItem{}, err
	}
	printedName, err := storageStringCell(row, columns["Name"])
	defaultClass := false
	name := printedName
	if kind == storageTableStorageClass && strings.HasSuffix(printedName, " (default)") {
		defaultClass = true
		name = strings.TrimSuffix(printedName, " (default)")
	}
	if err != nil || name != metadata.Name {
		return storageTableItem{}, fmt.Errorf("invalid Kubernetes storage table name: %w", domain.ErrUpstream)
	}
	item := storageTableItem{
		Namespace: metadata.Namespace, Name: metadata.Name, Default: defaultClass,
		CreatedAt: metadata.CreationTimestamp,
	}
	switch kind {
	case storageTablePersistentVolumeClaim:
		item.Status, err = storageStringCell(row, columns["Status"])
		if err == nil {
			item.Volume, err = storageStringCell(row, columns["Volume"])
		}
		if err == nil {
			item.Capacity, err = storageStringCell(row, columns["Capacity"])
		}
		if err == nil {
			item.AccessModes, err = storageStringCell(row, columns["Access Modes"])
		}
		if err == nil {
			item.StorageClass, err = storageStringCell(row, columns["StorageClass"])
		}
		if err == nil {
			item.VolumeMode, err = storageStringCell(row, columns["VolumeMode"])
		}
	case storageTablePersistentVolume:
		item.Status, err = storageStringCell(row, columns["Status"])
		if err == nil {
			item.Claim, err = storageStringCell(row, columns["Claim"])
		}
		if err == nil {
			item.Capacity, err = storageStringCell(row, columns["Capacity"])
		}
		if err == nil {
			item.AccessModes, err = storageStringCell(row, columns["Access Modes"])
		}
		if err == nil {
			item.StorageClass, err = storageStringCell(row, columns["StorageClass"])
		}
		if err == nil {
			item.ReclaimPolicy, err = storageStringCell(row, columns["Reclaim Policy"])
		}
		if err == nil {
			item.VolumeMode, err = storageStringCell(row, columns["VolumeMode"])
		}
	case storageTableStorageClass:
		item.Provisioner, err = storageStringCell(row, columns["Provisioner"])
		if err == nil {
			item.ReclaimPolicy, err = storageStringCell(row, columns["ReclaimPolicy"])
		}
		if err == nil {
			item.VolumeBindingMode, err = storageStringCell(row, columns["VolumeBindingMode"])
		}
		if err == nil {
			item.AllowVolumeExpansion, err = storageBoolCell(row, columns["AllowVolumeExpansion"])
		}
	default:
		err = domain.ErrUpstream
	}
	if err != nil {
		return storageTableItem{}, fmt.Errorf("invalid Kubernetes storage table cell: %w", domain.ErrUpstream)
	}
	return item, nil
}

func storageStringCell(row kubernetesTableRow, index int) (string, error) {
	if index < 0 || index >= len(row.Cells) {
		return "", domain.ErrUpstream
	}
	if string(row.Cells[index]) == "null" {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(row.Cells[index], &value); err != nil || !validTableString(value) {
		return "", domain.ErrUpstream
	}
	return value, nil
}

func storageBoolCell(row kubernetesTableRow, index int) (bool, error) {
	if index < 0 || index >= len(row.Cells) {
		return false, domain.ErrUpstream
	}
	raw := row.Cells[index]
	if string(raw) == "null" {
		return false, nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil || !validTableString(text) {
		return false, domain.ErrUpstream
	}
	switch text {
	case "true":
		return true, nil
	case "", "false", "<unset>":
		return false, nil
	default:
		return false, domain.ErrUpstream
	}
}
