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

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

const (
	csiDriverCollectionPath         = "/apis/storage.k8s.io/v1/csidrivers"
	csiDriverListPageSize           = "250"
	csiDriverMaxListPages           = 4
	csiDriverMaxListItems           = 1000
	csiDriverMaxListBytes     int64 = 4 * 1024 * 1024
	csiDriverMaxDetailBytes   int64 = 1024 * 1024
	csiDriverMaxContinueBytes       = 16 * 1024
	csiDriverMaxTokenRequests       = 32
)

type csiDriverTokenRequestSource struct{}

func (*csiDriverTokenRequestSource) UnmarshalJSON(value []byte) error {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' || !json.Valid(trimmed) {
		return fmt.Errorf("invalid Kubernetes CSIDriver token request")
	}
	return nil
}

type csiDriverSource struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name              string    `json:"name"`
		Namespace         string    `json:"namespace"`
		CreationTimestamp time.Time `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		AttachRequired       *bool                         `json:"attachRequired"`
		PodInfoOnMount       *bool                         `json:"podInfoOnMount"`
		StorageCapacity      *bool                         `json:"storageCapacity"`
		RequiresRepublish    *bool                         `json:"requiresRepublish"`
		SELinuxMount         *bool                         `json:"seLinuxMount"`
		FSGroupPolicy        *string                       `json:"fsGroupPolicy"`
		VolumeLifecycleModes []string                      `json:"volumeLifecycleModes"`
		TokenRequests        []csiDriverTokenRequestSource `json:"tokenRequests"`
	} `json:"spec"`
}

func (c *Client) CSIDrivers(ctx context.Context) ([]domain.KubernetesCSIDriver, error) {
	query := url.Values{"limit": {csiDriverListPageSize}}
	items := make([]domain.KubernetesCSIDriver, 0)
	seenNames := make(map[string]struct{})
	seenContinue := make(map[string]struct{})
	var totalBytes int64
	for page := 0; page < csiDriverMaxListPages; page++ {
		remainingBytes := csiDriverMaxListBytes - totalBytes
		if remainingBytes <= 0 {
			return nil, fmt.Errorf("Kubernetes CSIDriver list exceeded safe byte limit: %w", domain.ErrUpstream)
		}
		payload, _, err := c.getPayload(
			ctx, csiDriverCollectionPath, query, kubernetesPartialMetadataListAccept, remainingBytes, false,
		)
		if err != nil {
			return nil, err
		}
		totalBytes += int64(len(payload))

		var response partialObjectMetadataList
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, fmt.Errorf("decode Kubernetes CSIDriver metadata list: %w", domain.ErrUpstream)
		}
		if response.APIVersion != "meta.k8s.io/v1" || response.Kind != "PartialObjectMetadataList" {
			return nil, fmt.Errorf("unsupported Kubernetes CSIDriver metadata list: %w", domain.ErrUpstream)
		}
		if len(response.Items) > csiDriverMaxListItems-len(items) {
			return nil, fmt.Errorf("Kubernetes CSIDriver list exceeded safe item limit: %w", domain.ErrUpstream)
		}
		for _, raw := range response.Items {
			metadata, err := decodePartialObjectMetadataForScope(raw, false)
			if err != nil {
				return nil, err
			}
			if domain.ValidateCSIDriverName(metadata.Name) != nil {
				return nil, fmt.Errorf("invalid Kubernetes CSIDriver metadata identity: %w", domain.ErrUpstream)
			}
			if _, duplicate := seenNames[metadata.Name]; duplicate {
				return nil, fmt.Errorf("duplicate Kubernetes CSIDriver metadata identity: %w", domain.ErrUpstream)
			}
			seenNames[metadata.Name] = struct{}{}
			items = append(items, domain.KubernetesCSIDriver{
				Name: metadata.Name, CreatedAt: metadata.CreationTimestamp.UTC(),
			})
		}

		continuation := response.Metadata.Continue
		if continuation == "" {
			sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
			return items, nil
		}
		if !validCSIDriverContinue(continuation) {
			return nil, fmt.Errorf("invalid Kubernetes CSIDriver continuation token: %w", domain.ErrUpstream)
		}
		if _, duplicate := seenContinue[continuation]; duplicate {
			return nil, fmt.Errorf("repeated Kubernetes CSIDriver continuation token: %w", domain.ErrUpstream)
		}
		seenContinue[continuation] = struct{}{}
		query.Set("continue", continuation)
	}
	return nil, fmt.Errorf("Kubernetes CSIDriver list exceeded safe page limit: %w", domain.ErrUpstream)
}

func (c *Client) CSIDriver(ctx context.Context, name string) (domain.KubernetesCSIDriverDetail, error) {
	if err := domain.ValidateCSIDriverName(name); err != nil {
		return domain.KubernetesCSIDriverDetail{}, err
	}
	payload, _, err := c.getPayload(
		ctx, csiDriverCollectionPath+"/"+name, nil, "application/json", csiDriverMaxDetailBytes, false,
	)
	if err != nil {
		return domain.KubernetesCSIDriverDetail{}, err
	}
	return decodeCSIDriver(payload, name)
}

func decodeCSIDriver(payload []byte, expectedName string) (domain.KubernetesCSIDriverDetail, error) {
	var source csiDriverSource
	if err := json.Unmarshal(payload, &source); err != nil {
		return domain.KubernetesCSIDriverDetail{}, fmt.Errorf("decode Kubernetes CSIDriver detail: %w", domain.ErrUpstream)
	}
	if source.APIVersion != "storage.k8s.io/v1" || source.Kind != "CSIDriver" ||
		source.Metadata.Name != expectedName || source.Metadata.Namespace != "" || source.Metadata.CreationTimestamp.IsZero() ||
		domain.ValidateCSIDriverName(source.Metadata.Name) != nil || source.Spec.AttachRequired == nil ||
		source.Spec.PodInfoOnMount == nil || source.Spec.StorageCapacity == nil || source.Spec.RequiresRepublish == nil ||
		source.Spec.FSGroupPolicy == nil {
		return domain.KubernetesCSIDriverDetail{}, fmt.Errorf("invalid Kubernetes CSIDriver identity: %w", domain.ErrUpstream)
	}

	fsGroupPolicy, err := normalizeCSIFSGroupPolicy(*source.Spec.FSGroupPolicy)
	if err != nil {
		return domain.KubernetesCSIDriverDetail{}, err
	}
	volumeLifecycleModes, err := normalizeCSIVolumeLifecycleModes(source.Spec.VolumeLifecycleModes)
	if err != nil {
		return domain.KubernetesCSIDriverDetail{}, err
	}
	if len(source.Spec.TokenRequests) > csiDriverMaxTokenRequests {
		return domain.KubernetesCSIDriverDetail{}, fmt.Errorf("Kubernetes CSIDriver token requests exceeded safe limit: %w", domain.ErrUpstream)
	}

	seLinuxMount := false
	if source.Spec.SELinuxMount != nil {
		seLinuxMount = *source.Spec.SELinuxMount
	}
	return domain.KubernetesCSIDriverDetail{
		KubernetesCSIDriver: domain.KubernetesCSIDriver{
			Name: source.Metadata.Name, CreatedAt: source.Metadata.CreationTimestamp.UTC(),
		},
		AttachRequired: *source.Spec.AttachRequired, PodInfoOnMount: *source.Spec.PodInfoOnMount,
		StorageCapacity: *source.Spec.StorageCapacity, RequiresRepublish: *source.Spec.RequiresRepublish,
		SELinuxMount: seLinuxMount, FSGroupPolicy: fsGroupPolicy,
		VolumeLifecycleModes: volumeLifecycleModes, TokenRequestCount: len(source.Spec.TokenRequests),
	}, nil
}

func normalizeCSIFSGroupPolicy(value string) (domain.KubernetesCSIFSGroupPolicy, error) {
	switch domain.KubernetesCSIFSGroupPolicy(value) {
	case domain.CSIFSGroupPolicyReadWriteOnceWithFSType, domain.CSIFSGroupPolicyFile, domain.CSIFSGroupPolicyNone:
		return domain.KubernetesCSIFSGroupPolicy(value), nil
	default:
		return "", fmt.Errorf("invalid Kubernetes CSIDriver FSGroupPolicy: %w", domain.ErrUpstream)
	}
}

func normalizeCSIVolumeLifecycleModes(values []string) ([]domain.KubernetesCSIVolumeLifecycleMode, error) {
	if len(values) == 0 || len(values) > 2 {
		return nil, fmt.Errorf("invalid Kubernetes CSIDriver volume lifecycle modes: %w", domain.ErrUpstream)
	}
	seen := make(map[domain.KubernetesCSIVolumeLifecycleMode]struct{}, len(values))
	for _, value := range values {
		mode := domain.KubernetesCSIVolumeLifecycleMode(value)
		if mode != domain.CSIVolumeLifecyclePersistent && mode != domain.CSIVolumeLifecycleEphemeral {
			return nil, fmt.Errorf("invalid Kubernetes CSIDriver volume lifecycle mode: %w", domain.ErrUpstream)
		}
		if _, duplicate := seen[mode]; duplicate {
			return nil, fmt.Errorf("duplicate Kubernetes CSIDriver volume lifecycle mode: %w", domain.ErrUpstream)
		}
		seen[mode] = struct{}{}
	}
	result := make([]domain.KubernetesCSIVolumeLifecycleMode, 0, len(values))
	for _, mode := range []domain.KubernetesCSIVolumeLifecycleMode{
		domain.CSIVolumeLifecyclePersistent, domain.CSIVolumeLifecycleEphemeral,
	} {
		if _, exists := seen[mode]; exists {
			result = append(result, mode)
		}
	}
	return result, nil
}

func validCSIDriverContinue(value string) bool {
	return value != "" && len(value) <= csiDriverMaxContinueBytes && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}
