package kubernetes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/caoyanyi/k8s-panel/internal/domain"
	apiValidation "k8s.io/apimachinery/pkg/util/validation"
)

const (
	csiNodeCollectionPath                 = "/apis/storage.k8s.io/v1/csinodes"
	csiNodeListPageSize                   = "250"
	csiNodeMaxListPages                   = 4
	csiNodeMaxListItems                   = 1000
	csiNodeMaxPageBytes             int64 = 2 * 1024 * 1024
	csiNodeMaxListBytes             int64 = 4 * 1024 * 1024
	csiNodeMaxDetailBytes           int64 = 1024 * 1024
	csiNodeMaxContinueBytes               = 16 * 1024
	csiNodeMaxDrivers                     = 128
	csiNodeMaxNodeIDBytes                 = 192
	csiNodeMaxTopologyKeysPerDriver       = 64
	csiNodeMaxTopologyKeysTotal           = 4096
	csiNodeMaxTopologyKeyBytes            = 317
)

type discardedCSINodeID struct {
	present bool
}

func (value *discardedCSINodeID) UnmarshalJSON(raw []byte) error {
	var nodeID string
	if len(raw) > csiNodeMaxNodeIDBytes*6+2 {
		return fmt.Errorf("invalid Kubernetes CSINode node ID")
	}
	if err := json.Unmarshal(raw, &nodeID); err != nil || nodeID == "" || len(nodeID) > csiNodeMaxNodeIDBytes ||
		nodeID != strings.TrimSpace(nodeID) || strings.IndexFunc(nodeID, unicode.IsControl) >= 0 {
		return fmt.Errorf("invalid Kubernetes CSINode node ID")
	}
	value.present = true
	return nil
}

type discardedCSINodeTopologyKeys struct {
	count int
}

func (value *discardedCSINodeTopologyKeys) UnmarshalJSON(raw []byte) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		value.count = 0
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('[') {
		return fmt.Errorf("invalid Kubernetes CSINode topology keys")
	}
	count := 0
	for decoder.More() {
		if count >= csiNodeMaxTopologyKeysPerDriver {
			return fmt.Errorf("invalid Kubernetes CSINode topology keys")
		}
		var key string
		if err := decoder.Decode(&key); err != nil {
			return fmt.Errorf("invalid Kubernetes CSINode topology keys")
		}
		if key == "" || len(key) > csiNodeMaxTopologyKeyBytes || len(apiValidation.IsQualifiedName(key)) != 0 {
			return fmt.Errorf("invalid Kubernetes CSINode topology key")
		}
		count++
	}
	if closing, err := decoder.Token(); err != nil || closing != json.Delim(']') || !csiNodeJSONEnded(decoder) {
		return fmt.Errorf("invalid Kubernetes CSINode topology keys")
	}
	value.count = count
	return nil
}

type csiNodeDriverSource struct {
	Name         string                       `json:"name"`
	NodeID       discardedCSINodeID           `json:"nodeID"`
	TopologyKeys discardedCSINodeTopologyKeys `json:"topologyKeys"`
	Allocatable  *struct {
		Count *int32 `json:"count"`
	} `json:"allocatable"`
}

type csiNodeDriverListSource struct {
	present bool
	drivers []csiNodeDriverSource
}

func (value *csiNodeDriverListSource) UnmarshalJSON(raw []byte) error {
	value.present = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		value.drivers = []csiNodeDriverSource{}
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('[') {
		return fmt.Errorf("invalid Kubernetes CSINode drivers")
	}
	drivers := make([]csiNodeDriverSource, 0)
	for decoder.More() {
		if len(drivers) >= csiNodeMaxDrivers {
			return fmt.Errorf("invalid Kubernetes CSINode drivers")
		}
		var driver csiNodeDriverSource
		if err := decoder.Decode(&driver); err != nil {
			return fmt.Errorf("invalid Kubernetes CSINode drivers")
		}
		drivers = append(drivers, driver)
	}
	if closing, err := decoder.Token(); err != nil || closing != json.Delim(']') || !csiNodeJSONEnded(decoder) {
		return fmt.Errorf("invalid Kubernetes CSINode drivers")
	}
	value.drivers = drivers
	return nil
}

func csiNodeJSONEnded(decoder *json.Decoder) bool {
	var trailing json.RawMessage
	return decoder.Decode(&trailing) == io.EOF
}

type csiNodeSource struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name              string    `json:"name"`
		Namespace         string    `json:"namespace"`
		CreationTimestamp time.Time `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		Drivers csiNodeDriverListSource `json:"drivers"`
	} `json:"spec"`
}

func (c *Client) CSINodes(ctx context.Context) ([]domain.KubernetesCSINode, error) {
	query := url.Values{
		"includeObject": {"Metadata"},
		"limit":         {csiNodeListPageSize},
	}
	items := make([]domain.KubernetesCSINode, 0)
	seenNames := make(map[string]struct{})
	seenContinue := make(map[string]struct{})
	var totalBytes int64

	for page := 0; page < csiNodeMaxListPages; page++ {
		remainingBytes := csiNodeMaxListBytes - totalBytes
		if remainingBytes <= 0 {
			return nil, fmt.Errorf("Kubernetes CSINode list exceeded safe byte limit: %w", domain.ErrUpstream)
		}
		pageBytes := min(remainingBytes, csiNodeMaxPageBytes)
		payload, _, err := c.getPayload(ctx, csiNodeCollectionPath, query, kubernetesTableAccept, pageBytes, false)
		if err != nil {
			return nil, err
		}
		totalBytes += int64(len(payload))

		var response kubernetesTable
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, fmt.Errorf("decode Kubernetes CSINode table: %w", domain.ErrUpstream)
		}
		if response.APIVersion != "meta.k8s.io/v1" || response.Kind != "Table" ||
			len(response.ColumnDefinitions) == 0 || len(response.ColumnDefinitions) > maxTableColumns {
			return nil, fmt.Errorf("unsupported Kubernetes CSINode table: %w", domain.ErrUpstream)
		}
		if len(response.Rows) > csiNodeMaxListItems-len(items) {
			return nil, fmt.Errorf("Kubernetes CSINode list exceeded safe item limit: %w", domain.ErrUpstream)
		}
		columns, err := csiNodeColumns(response.ColumnDefinitions)
		if err != nil {
			return nil, err
		}
		for _, row := range response.Rows {
			item, err := decodeCSINodeTableRow(row, columns)
			if err != nil {
				return nil, err
			}
			if _, duplicate := seenNames[item.Name]; duplicate {
				return nil, fmt.Errorf("duplicate Kubernetes CSINode identity: %w", domain.ErrUpstream)
			}
			seenNames[item.Name] = struct{}{}
			items = append(items, item)
		}

		continuation := response.Metadata.Continue
		if continuation == "" {
			sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
			return items, nil
		}
		if !validCSINodeContinue(continuation) {
			return nil, fmt.Errorf("invalid Kubernetes CSINode continuation token: %w", domain.ErrUpstream)
		}
		if _, duplicate := seenContinue[continuation]; duplicate {
			return nil, fmt.Errorf("repeated Kubernetes CSINode continuation token: %w", domain.ErrUpstream)
		}
		seenContinue[continuation] = struct{}{}
		query.Set("continue", continuation)
	}
	return nil, fmt.Errorf("Kubernetes CSINode list exceeded safe page limit: %w", domain.ErrUpstream)
}

func (c *Client) CSINode(ctx context.Context, name string) (domain.KubernetesCSINodeDetail, error) {
	if err := domain.ValidateNodeName(name); err != nil {
		return domain.KubernetesCSINodeDetail{}, err
	}
	payload, _, err := c.getPayload(
		ctx, csiNodeCollectionPath+"/"+name, nil, "application/json", csiNodeMaxDetailBytes, false,
	)
	if err != nil {
		return domain.KubernetesCSINodeDetail{}, err
	}
	return decodeCSINode(payload, name)
}

func csiNodeColumns(columns []struct {
	Name string `json:"name"`
	Type string `json:"type"`
}) (map[string]int, error) {
	required := map[string]string{"Name": "string", "Drivers": "integer"}
	result := make(map[string]int, len(required))
	for index, column := range columns {
		if column.Name == "" || column.Type == "" || !validTableString(column.Name) || !validTableString(column.Type) {
			return nil, fmt.Errorf("invalid Kubernetes CSINode table column: %w", domain.ErrUpstream)
		}
		expectedType, wanted := required[column.Name]
		if !wanted {
			continue
		}
		if column.Type != expectedType {
			return nil, fmt.Errorf("invalid Kubernetes CSINode table column type: %w", domain.ErrUpstream)
		}
		if _, duplicate := result[column.Name]; duplicate {
			return nil, fmt.Errorf("duplicate Kubernetes CSINode table column: %w", domain.ErrUpstream)
		}
		result[column.Name] = index
	}
	if len(result) != len(required) {
		return nil, fmt.Errorf("missing Kubernetes CSINode table column: %w", domain.ErrUpstream)
	}
	return result, nil
}

func decodeCSINodeTableRow(row kubernetesTableRow, columns map[string]int) (domain.KubernetesCSINode, error) {
	var empty domain.KubernetesCSINode
	if len(row.Cells) == 0 || len(row.Cells) > maxTableColumns {
		return empty, fmt.Errorf("invalid Kubernetes CSINode table row: %w", domain.ErrUpstream)
	}
	metadata, err := decodePartialObjectMetadataForScope(row.Object, false)
	if err != nil {
		return empty, err
	}
	name, err := storageStringCell(row, columns["Name"])
	if err != nil || name != metadata.Name || domain.ValidateNodeName(name) != nil {
		return empty, fmt.Errorf("invalid Kubernetes CSINode identity: %w", domain.ErrUpstream)
	}
	driverCount, err := csiNodeIntegerCell(row, columns["Drivers"])
	if err != nil || driverCount > csiNodeMaxDrivers {
		return empty, fmt.Errorf("invalid Kubernetes CSINode driver count: %w", domain.ErrUpstream)
	}
	return domain.KubernetesCSINode{
		Name: name, DriverCount: driverCount, CreatedAt: metadata.CreationTimestamp.UTC(),
	}, nil
}

func csiNodeIntegerCell(row kubernetesTableRow, index int) (int, error) {
	if index < 0 || index >= len(row.Cells) {
		return 0, domain.ErrUpstream
	}
	value := bytes.TrimSpace(row.Cells[index])
	if len(value) == 0 {
		return 0, domain.ErrUpstream
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return 0, domain.ErrUpstream
		}
	}
	parsed, err := strconv.Atoi(string(value))
	if err != nil {
		return 0, domain.ErrUpstream
	}
	return parsed, nil
}

func decodeCSINode(payload []byte, expectedName string) (domain.KubernetesCSINodeDetail, error) {
	var source csiNodeSource
	if err := json.Unmarshal(payload, &source); err != nil {
		return domain.KubernetesCSINodeDetail{}, fmt.Errorf("decode Kubernetes CSINode detail: %w", domain.ErrUpstream)
	}
	if source.APIVersion != "storage.k8s.io/v1" || source.Kind != "CSINode" ||
		source.Metadata.Name != expectedName || source.Metadata.Namespace != "" || source.Metadata.CreationTimestamp.IsZero() ||
		domain.ValidateNodeName(source.Metadata.Name) != nil || !source.Spec.Drivers.present {
		return domain.KubernetesCSINodeDetail{}, fmt.Errorf("invalid Kubernetes CSINode identity: %w", domain.ErrUpstream)
	}

	drivers := make([]domain.KubernetesCSINodeDriver, 0, len(source.Spec.Drivers.drivers))
	seen := make(map[string]struct{}, len(source.Spec.Drivers.drivers))
	topologyKeyCount := 0
	for _, driver := range source.Spec.Drivers.drivers {
		if domain.ValidateCSIDriverName(driver.Name) != nil || !driver.NodeID.present {
			return domain.KubernetesCSINodeDetail{}, fmt.Errorf("invalid Kubernetes CSINode driver: %w", domain.ErrUpstream)
		}
		if _, duplicate := seen[driver.Name]; duplicate {
			return domain.KubernetesCSINodeDetail{}, fmt.Errorf("duplicate Kubernetes CSINode driver: %w", domain.ErrUpstream)
		}
		seen[driver.Name] = struct{}{}
		topologyKeyCount += driver.TopologyKeys.count
		if topologyKeyCount > csiNodeMaxTopologyKeysTotal {
			return domain.KubernetesCSINodeDetail{}, fmt.Errorf("Kubernetes CSINode topology keys exceeded safe limit: %w", domain.ErrUpstream)
		}
		var allocatableCount *int32
		if driver.Allocatable != nil && driver.Allocatable.Count != nil {
			if *driver.Allocatable.Count < 0 {
				return domain.KubernetesCSINodeDetail{}, fmt.Errorf("invalid Kubernetes CSINode allocatable count: %w", domain.ErrUpstream)
			}
			count := *driver.Allocatable.Count
			allocatableCount = &count
		}
		drivers = append(drivers, domain.KubernetesCSINodeDriver{
			Name: driver.Name, AllocatableCount: allocatableCount, TopologyKeyCount: driver.TopologyKeys.count,
		})
	}
	sort.Slice(drivers, func(i, j int) bool { return drivers[i].Name < drivers[j].Name })
	return domain.KubernetesCSINodeDetail{
		KubernetesCSINode: domain.KubernetesCSINode{
			Name: source.Metadata.Name, DriverCount: len(drivers), CreatedAt: source.Metadata.CreationTimestamp.UTC(),
		},
		Drivers: drivers,
	}, nil
}

func validCSINodeContinue(value string) bool {
	return value != "" && len(value) <= csiNodeMaxContinueBytes && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}
