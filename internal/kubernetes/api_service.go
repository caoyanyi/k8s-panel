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
	apiServiceCollectionPath           = "/apis/apiregistration.k8s.io/v1/apiservices"
	apiServiceListPageSize             = "250"
	apiServiceMaxListPages             = 4
	apiServiceMaxListItems             = 1000
	apiServiceMaxListBytes       int64 = 4 * 1024 * 1024
	apiServiceMaxContinueBytes         = 16 * 1024
	maxAPIServiceConditions            = 32
	apiServiceMaxTotalConditions       = 4096
	maxAPIServiceTextBytes             = 256
	defaultAPIServicePort        int32 = 443
	maxAPIServicePort            int32 = 65535
	maxAPIServiceVersionPriority       = 1000
)

type apiServiceConditionSource struct {
	Type               string     `json:"type"`
	Status             string     `json:"status"`
	Reason             string     `json:"reason"`
	LastTransitionTime *time.Time `json:"lastTransitionTime"`
}

type apiServiceSource struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name              string    `json:"name"`
		Namespace         string    `json:"namespace"`
		CreationTimestamp time.Time `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		Group                string `json:"group"`
		Version              string `json:"version"`
		GroupPriorityMinimum int32  `json:"groupPriorityMinimum"`
		VersionPriority      int32  `json:"versionPriority"`
		Service              *struct {
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
			Port      *int32 `json:"port"`
		} `json:"service"`
		InsecureSkipTLSVerify bool `json:"insecureSkipTLSVerify"`
	} `json:"spec"`
	Status struct {
		Conditions []apiServiceConditionSource `json:"conditions"`
	} `json:"status"`
}

type apiServiceListSource struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Continue string `json:"continue"`
	} `json:"metadata"`
	Items []apiServiceSource `json:"items"`
}

func (c *Client) APIServices(ctx context.Context) ([]domain.KubernetesAPIService, error) {
	query := url.Values{"limit": {apiServiceListPageSize}}
	items := make([]domain.KubernetesAPIService, 0)
	seenNames := make(map[string]struct{})
	totalConditions := 0
	var totalBytes int64

	for page := 0; page < apiServiceMaxListPages; page++ {
		remainingBytes := apiServiceMaxListBytes - totalBytes
		if remainingBytes <= 0 {
			return nil, fmt.Errorf("Kubernetes APIService list exceeded safe byte limit: %w", domain.ErrUpstream)
		}
		payload, _, err := c.getPayload(
			ctx, apiServiceCollectionPath, query, "application/json", remainingBytes, false,
		)
		if err != nil {
			return nil, err
		}
		totalBytes += int64(len(payload))

		var response apiServiceListSource
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, fmt.Errorf("decode Kubernetes APIService list: %w", domain.ErrUpstream)
		}
		if response.APIVersion != "apiregistration.k8s.io/v1" || response.Kind != "APIServiceList" {
			return nil, fmt.Errorf("unsupported Kubernetes APIService list: %w", domain.ErrUpstream)
		}
		if len(response.Items) > apiServiceMaxListItems-len(items) {
			return nil, fmt.Errorf("Kubernetes APIService list exceeded safe item limit: %w", domain.ErrUpstream)
		}
		for _, source := range response.Items {
			totalConditions += len(source.Status.Conditions)
			if totalConditions > apiServiceMaxTotalConditions {
				return nil, fmt.Errorf("Kubernetes APIService list exceeded safe condition limit: %w", domain.ErrUpstream)
			}
			item, err := decodeAPIService(source)
			if err != nil {
				return nil, err
			}
			if _, duplicate := seenNames[item.Name]; duplicate {
				return nil, fmt.Errorf("duplicate Kubernetes APIService: %w", domain.ErrUpstream)
			}
			seenNames[item.Name] = struct{}{}
			items = append(items, item)
		}

		if response.Metadata.Continue == "" {
			sort.Slice(items, func(i, j int) bool {
				if items[i].Group != items[j].Group {
					return items[i].Group < items[j].Group
				}
				if items[i].Version != items[j].Version {
					return items[i].Version < items[j].Version
				}
				return items[i].Name < items[j].Name
			})
			return items, nil
		}
		if !validAPIServiceContinue(response.Metadata.Continue) {
			return nil, fmt.Errorf("invalid Kubernetes APIService continuation token: %w", domain.ErrUpstream)
		}
		query.Set("continue", response.Metadata.Continue)
	}

	return nil, fmt.Errorf("Kubernetes APIService list exceeded safe page limit: %w", domain.ErrUpstream)
}

func decodeAPIService(source apiServiceSource) (domain.KubernetesAPIService, error) {
	version, group, err := splitAPIServiceName(source.Metadata.Name)
	if err != nil || source.APIVersion != "apiregistration.k8s.io/v1" || source.Kind != "APIService" ||
		source.Metadata.Namespace != "" || source.Metadata.CreationTimestamp.IsZero() || source.Spec.Group != group ||
		source.Spec.Version != version || source.Spec.GroupPriorityMinimum < 1 || source.Spec.VersionPriority < 1 ||
		source.Spec.VersionPriority > maxAPIServiceVersionPriority {
		return domain.KubernetesAPIService{}, fmt.Errorf("invalid Kubernetes APIService identity: %w", domain.ErrUpstream)
	}
	if len(source.Status.Conditions) > maxAPIServiceConditions {
		return domain.KubernetesAPIService{}, fmt.Errorf("Kubernetes APIService exceeded safe condition limit: %w", domain.ErrUpstream)
	}

	item := domain.KubernetesAPIService{
		Name: source.Metadata.Name, Group: group, Version: version, Local: source.Spec.Service == nil,
		ConditionCount: len(source.Status.Conditions), InsecureSkipTLSVerify: source.Spec.InsecureSkipTLSVerify,
		GroupPriorityMinimum: source.Spec.GroupPriorityMinimum, VersionPriority: source.Spec.VersionPriority,
		CreatedAt: source.Metadata.CreationTimestamp,
	}
	if source.Spec.Service != nil {
		if err := domain.ValidateNamespace(source.Spec.Service.Namespace); err != nil {
			return domain.KubernetesAPIService{}, fmt.Errorf("invalid Kubernetes APIService service namespace: %w", domain.ErrUpstream)
		}
		if err := domain.ValidateNamespace(source.Spec.Service.Name); err != nil {
			return domain.KubernetesAPIService{}, fmt.Errorf("invalid Kubernetes APIService service name: %w", domain.ErrUpstream)
		}
		item.ServiceNamespace = source.Spec.Service.Namespace
		item.ServiceName = source.Spec.Service.Name
		item.ServicePort = defaultAPIServicePort
		item.ServicePortDefaulted = source.Spec.Service.Port == nil
		if source.Spec.Service.Port != nil {
			if *source.Spec.Service.Port < 1 || *source.Spec.Service.Port > maxAPIServicePort {
				return domain.KubernetesAPIService{}, fmt.Errorf("invalid Kubernetes APIService service port: %w", domain.ErrUpstream)
			}
			item.ServicePort = *source.Spec.Service.Port
		}
	}

	seenConditions := make(map[string]struct{}, len(source.Status.Conditions))
	for _, condition := range source.Status.Conditions {
		if !validAPIServiceText(condition.Type, true) || !validAPIServiceText(condition.Reason, false) ||
			(condition.Status != "True" && condition.Status != "False" && condition.Status != "Unknown") ||
			(condition.LastTransitionTime != nil && condition.LastTransitionTime.IsZero()) {
			return domain.KubernetesAPIService{}, fmt.Errorf("invalid Kubernetes APIService condition: %w", domain.ErrUpstream)
		}
		if _, duplicate := seenConditions[condition.Type]; duplicate {
			return domain.KubernetesAPIService{}, fmt.Errorf("duplicate Kubernetes APIService condition: %w", domain.ErrUpstream)
		}
		seenConditions[condition.Type] = struct{}{}
		if condition.Type == "Available" {
			item.AvailabilityObserved = true
			item.AvailabilityStatus = condition.Status
			item.AvailabilityReason = condition.Reason
			item.AvailabilityTransitionTime = condition.LastTransitionTime
		}
	}
	return item, nil
}

func splitAPIServiceName(name string) (string, string, error) {
	if err := domain.ValidateAPIServiceName(name); err != nil {
		return "", "", err
	}
	version, group, _ := strings.Cut(name, ".")
	return version, group, nil
}

func validAPIServiceContinue(value string) bool {
	return value != "" && len(value) <= apiServiceMaxContinueBytes && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

func validAPIServiceText(value string, required bool) bool {
	if value == "" {
		return !required
	}
	return len(value) <= maxAPIServiceTextBytes && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}
