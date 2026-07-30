package kubernetes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

const (
	runtimeClassCollectionPath             = "/apis/node.k8s.io/v1/runtimeclasses"
	runtimeClassListPageSize               = "250"
	runtimeClassMaxListPages               = 4
	runtimeClassMaxListItems               = 1000
	runtimeClassMaxListBytes         int64 = 4 * 1024 * 1024
	runtimeClassMaxDetailBytes       int64 = 1024 * 1024
	runtimeClassMaxContinueBytes           = 16 * 1024
	runtimeClassMaxScalarBytes             = 512
	runtimeClassMaxOverheadResources       = 64
	runtimeClassMaxNodeSelectors           = 256
	runtimeClassMaxTolerations             = 256
)

type runtimeClassTolerationSource struct{}

func (*runtimeClassTolerationSource) UnmarshalJSON(value []byte) error {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' || !json.Valid(trimmed) {
		return fmt.Errorf("invalid Kubernetes RuntimeClass toleration")
	}
	return nil
}

type runtimeClassSource struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name              string    `json:"name"`
		Namespace         string    `json:"namespace"`
		CreationTimestamp time.Time `json:"creationTimestamp"`
	} `json:"metadata"`
	Handler  string `json:"handler"`
	Overhead *struct {
		PodFixed map[string]string `json:"podFixed"`
	} `json:"overhead"`
	Scheduling *struct {
		NodeSelector map[string]string              `json:"nodeSelector"`
		Tolerations  []runtimeClassTolerationSource `json:"tolerations"`
	} `json:"scheduling"`
}

func (c *Client) RuntimeClasses(ctx context.Context) ([]domain.KubernetesRuntimeClass, error) {
	query := url.Values{"limit": {runtimeClassListPageSize}}
	items := make([]domain.KubernetesRuntimeClass, 0)
	seenNames := make(map[string]struct{})
	seenContinue := make(map[string]struct{})
	var totalBytes int64
	for page := 0; page < runtimeClassMaxListPages; page++ {
		remainingBytes := runtimeClassMaxListBytes - totalBytes
		if remainingBytes <= 0 {
			return nil, fmt.Errorf("Kubernetes RuntimeClass list exceeded safe byte limit: %w", domain.ErrUpstream)
		}
		payload, _, err := c.getPayload(
			ctx, runtimeClassCollectionPath, query, kubernetesPartialMetadataListAccept, remainingBytes, false,
		)
		if err != nil {
			return nil, err
		}
		totalBytes += int64(len(payload))

		var response partialObjectMetadataList
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, fmt.Errorf("decode Kubernetes RuntimeClass metadata list: %w", domain.ErrUpstream)
		}
		if response.APIVersion != "meta.k8s.io/v1" || response.Kind != "PartialObjectMetadataList" {
			return nil, fmt.Errorf("unsupported Kubernetes RuntimeClass metadata list: %w", domain.ErrUpstream)
		}
		if len(response.Items) > runtimeClassMaxListItems-len(items) {
			return nil, fmt.Errorf("Kubernetes RuntimeClass list exceeded safe item limit: %w", domain.ErrUpstream)
		}
		for _, raw := range response.Items {
			metadata, err := decodePartialObjectMetadataForScope(raw, false)
			if err != nil {
				return nil, err
			}
			if domain.ValidateRuntimeClassName(metadata.Name) != nil {
				return nil, fmt.Errorf("invalid Kubernetes RuntimeClass metadata identity: %w", domain.ErrUpstream)
			}
			if _, duplicate := seenNames[metadata.Name]; duplicate {
				return nil, fmt.Errorf("duplicate Kubernetes RuntimeClass metadata identity: %w", domain.ErrUpstream)
			}
			seenNames[metadata.Name] = struct{}{}
			items = append(items, domain.KubernetesRuntimeClass{
				Name: metadata.Name, CreatedAt: metadata.CreationTimestamp.UTC(),
			})
		}

		continuation := response.Metadata.Continue
		if continuation == "" {
			sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
			return items, nil
		}
		if !validRuntimeClassContinue(continuation) {
			return nil, fmt.Errorf("invalid Kubernetes RuntimeClass continuation token: %w", domain.ErrUpstream)
		}
		if _, duplicate := seenContinue[continuation]; duplicate {
			return nil, fmt.Errorf("repeated Kubernetes RuntimeClass continuation token: %w", domain.ErrUpstream)
		}
		seenContinue[continuation] = struct{}{}
		query.Set("continue", continuation)
	}
	return nil, fmt.Errorf("Kubernetes RuntimeClass list exceeded safe page limit: %w", domain.ErrUpstream)
}

func (c *Client) RuntimeClass(ctx context.Context, name string) (domain.KubernetesRuntimeClassDetail, error) {
	if err := domain.ValidateRuntimeClassName(name); err != nil {
		return domain.KubernetesRuntimeClassDetail{}, err
	}
	payload, _, err := c.getPayload(
		ctx, runtimeClassCollectionPath+"/"+name, nil, "application/json", runtimeClassMaxDetailBytes, false,
	)
	if err != nil {
		return domain.KubernetesRuntimeClassDetail{}, err
	}
	return decodeRuntimeClass(payload, name)
}

func decodeRuntimeClass(payload []byte, expectedName string) (domain.KubernetesRuntimeClassDetail, error) {
	var source runtimeClassSource
	if err := json.Unmarshal(payload, &source); err != nil {
		return domain.KubernetesRuntimeClassDetail{}, fmt.Errorf("decode Kubernetes RuntimeClass detail: %w", domain.ErrUpstream)
	}
	if source.APIVersion != "node.k8s.io/v1" || source.Kind != "RuntimeClass" ||
		source.Metadata.Name != expectedName || source.Metadata.Namespace != "" || source.Metadata.CreationTimestamp.IsZero() ||
		domain.ValidateRuntimeClassName(source.Metadata.Name) != nil || domain.ValidateRuntimeClassHandler(source.Handler) != nil {
		return domain.KubernetesRuntimeClassDetail{}, fmt.Errorf("invalid Kubernetes RuntimeClass identity: %w", domain.ErrUpstream)
	}

	detail := domain.KubernetesRuntimeClassDetail{
		KubernetesRuntimeClass: domain.KubernetesRuntimeClass{
			Name: source.Metadata.Name, CreatedAt: source.Metadata.CreationTimestamp.UTC(),
		},
		Handler: source.Handler,
	}
	if source.Overhead != nil {
		if len(source.Overhead.PodFixed) > runtimeClassMaxOverheadResources {
			return domain.KubernetesRuntimeClassDetail{}, fmt.Errorf("Kubernetes RuntimeClass overhead exceeded safe limit: %w", domain.ErrUpstream)
		}
		for name, quantity := range source.Overhead.PodFixed {
			if !validRuntimeClassScalar(name, false) || !validRuntimeClassScalar(quantity, false) {
				return domain.KubernetesRuntimeClassDetail{}, fmt.Errorf("invalid Kubernetes RuntimeClass overhead: %w", domain.ErrUpstream)
			}
		}
		detail.OverheadConfigured = true
		detail.OverheadResourceCount = len(source.Overhead.PodFixed)
		detail.PodOverheadCPU = copyRuntimeClassQuantity(source.Overhead.PodFixed, "cpu")
		detail.PodOverheadMemory = copyRuntimeClassQuantity(source.Overhead.PodFixed, "memory")
	}
	if source.Scheduling != nil {
		if len(source.Scheduling.NodeSelector) > runtimeClassMaxNodeSelectors ||
			len(source.Scheduling.Tolerations) > runtimeClassMaxTolerations {
			return domain.KubernetesRuntimeClassDetail{}, fmt.Errorf("Kubernetes RuntimeClass scheduling exceeded safe limit: %w", domain.ErrUpstream)
		}
		for key, value := range source.Scheduling.NodeSelector {
			if !validRuntimeClassScalar(key, false) || !validRuntimeClassScalar(value, true) {
				return domain.KubernetesRuntimeClassDetail{}, fmt.Errorf("invalid Kubernetes RuntimeClass node selector: %w", domain.ErrUpstream)
			}
		}
		detail.SchedulingConfigured = true
		detail.NodeSelectorCount = len(source.Scheduling.NodeSelector)
		detail.TolerationCount = len(source.Scheduling.Tolerations)
	}
	return detail, nil
}

func copyRuntimeClassQuantity(values map[string]string, name string) *string {
	value, exists := values[name]
	if !exists {
		return nil
	}
	return &value
}

func validRuntimeClassScalar(value string, allowEmpty bool) bool {
	return (allowEmpty || value != "") && len(value) <= runtimeClassMaxScalarBytes && utf8.ValidString(value) &&
		value == strings.TrimSpace(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}

func validRuntimeClassContinue(value string) bool {
	return value != "" && len(value) <= runtimeClassMaxContinueBytes && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}
