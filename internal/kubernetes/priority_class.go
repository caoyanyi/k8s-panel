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
	priorityClassCollectionPath         = "/apis/scheduling.k8s.io/v1/priorityclasses"
	priorityClassListPageSize           = "250"
	priorityClassMaxListPages           = 4
	priorityClassMaxListItems           = 1000
	priorityClassMaxListBytes     int64 = 4 * 1024 * 1024
	priorityClassMaxDetailBytes   int64 = 1024 * 1024
	priorityClassMaxContinueBytes       = 16 * 1024
)

type priorityClassSource struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name              string    `json:"name"`
		Namespace         string    `json:"namespace"`
		CreationTimestamp time.Time `json:"creationTimestamp"`
	} `json:"metadata"`
	Value            *int32  `json:"value"`
	GlobalDefault    bool    `json:"globalDefault"`
	PreemptionPolicy *string `json:"preemptionPolicy"`
}

func (c *Client) PriorityClasses(ctx context.Context) ([]domain.KubernetesPriorityClass, error) {
	query := url.Values{"limit": {priorityClassListPageSize}}
	items := make([]domain.KubernetesPriorityClass, 0)
	seenNames := make(map[string]struct{})
	seenContinue := make(map[string]struct{})
	var totalBytes int64
	for page := 0; page < priorityClassMaxListPages; page++ {
		remainingBytes := priorityClassMaxListBytes - totalBytes
		if remainingBytes <= 0 {
			return nil, fmt.Errorf("Kubernetes PriorityClass list exceeded safe byte limit: %w", domain.ErrUpstream)
		}
		payload, _, err := c.getPayload(
			ctx, priorityClassCollectionPath, query, kubernetesPartialMetadataListAccept, remainingBytes, false,
		)
		if err != nil {
			return nil, err
		}
		totalBytes += int64(len(payload))

		var response partialObjectMetadataList
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, fmt.Errorf("decode Kubernetes PriorityClass metadata list: %w", domain.ErrUpstream)
		}
		if response.APIVersion != "meta.k8s.io/v1" || response.Kind != "PartialObjectMetadataList" {
			return nil, fmt.Errorf("unsupported Kubernetes PriorityClass metadata list: %w", domain.ErrUpstream)
		}
		if len(response.Items) > priorityClassMaxListItems-len(items) {
			return nil, fmt.Errorf("Kubernetes PriorityClass list exceeded safe item limit: %w", domain.ErrUpstream)
		}
		for _, raw := range response.Items {
			metadata, err := decodePartialObjectMetadataForScope(raw, false)
			if err != nil {
				return nil, err
			}
			if domain.ValidatePriorityClassName(metadata.Name) != nil {
				return nil, fmt.Errorf("invalid Kubernetes PriorityClass metadata identity: %w", domain.ErrUpstream)
			}
			if _, duplicate := seenNames[metadata.Name]; duplicate {
				return nil, fmt.Errorf("duplicate Kubernetes PriorityClass metadata identity: %w", domain.ErrUpstream)
			}
			seenNames[metadata.Name] = struct{}{}
			items = append(items, domain.KubernetesPriorityClass{
				Name: metadata.Name, CreatedAt: metadata.CreationTimestamp.UTC(),
			})
		}

		continuation := response.Metadata.Continue
		if continuation == "" {
			sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
			return items, nil
		}
		if !validPriorityClassContinue(continuation) {
			return nil, fmt.Errorf("invalid Kubernetes PriorityClass continuation token: %w", domain.ErrUpstream)
		}
		if _, duplicate := seenContinue[continuation]; duplicate {
			return nil, fmt.Errorf("repeated Kubernetes PriorityClass continuation token: %w", domain.ErrUpstream)
		}
		seenContinue[continuation] = struct{}{}
		query.Set("continue", continuation)
	}
	return nil, fmt.Errorf("Kubernetes PriorityClass list exceeded safe page limit: %w", domain.ErrUpstream)
}

func (c *Client) PriorityClass(ctx context.Context, name string) (domain.KubernetesPriorityClassDetail, error) {
	if err := domain.ValidatePriorityClassName(name); err != nil {
		return domain.KubernetesPriorityClassDetail{}, err
	}
	payload, _, err := c.getPayload(
		ctx, priorityClassCollectionPath+"/"+name, nil, "application/json", priorityClassMaxDetailBytes, false,
	)
	if err != nil {
		return domain.KubernetesPriorityClassDetail{}, err
	}
	return decodePriorityClass(payload, name)
}

func decodePriorityClass(payload []byte, expectedName string) (domain.KubernetesPriorityClassDetail, error) {
	var source priorityClassSource
	if err := json.Unmarshal(payload, &source); err != nil {
		return domain.KubernetesPriorityClassDetail{}, fmt.Errorf("decode Kubernetes PriorityClass detail: %w", domain.ErrUpstream)
	}
	if source.APIVersion != "scheduling.k8s.io/v1" || source.Kind != "PriorityClass" ||
		source.Metadata.Name != expectedName || source.Metadata.Namespace != "" || source.Metadata.CreationTimestamp.IsZero() ||
		domain.ValidatePriorityClassName(source.Metadata.Name) != nil || source.Value == nil {
		return domain.KubernetesPriorityClassDetail{}, fmt.Errorf("invalid Kubernetes PriorityClass identity: %w", domain.ErrUpstream)
	}

	preemptionPolicy := domain.PriorityClassPreemptLower
	preemptionPolicyDefaulted := source.PreemptionPolicy == nil
	if source.PreemptionPolicy != nil {
		switch *source.PreemptionPolicy {
		case string(domain.PriorityClassPreemptNever):
			preemptionPolicy = domain.PriorityClassPreemptNever
		case string(domain.PriorityClassPreemptLower):
			preemptionPolicy = domain.PriorityClassPreemptLower
		default:
			return domain.KubernetesPriorityClassDetail{}, fmt.Errorf("invalid Kubernetes PriorityClass preemption policy: %w", domain.ErrUpstream)
		}
	}

	return domain.KubernetesPriorityClassDetail{
		KubernetesPriorityClass: domain.KubernetesPriorityClass{
			Name: source.Metadata.Name, CreatedAt: source.Metadata.CreationTimestamp.UTC(),
		},
		Value: *source.Value, GlobalDefault: source.GlobalDefault,
		PreemptionPolicy: preemptionPolicy, PreemptionPolicyDefaulted: preemptionPolicyDefaulted,
	}, nil
}

func validPriorityClassContinue(value string) bool {
	return value != "" && len(value) <= priorityClassMaxContinueBytes && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}
