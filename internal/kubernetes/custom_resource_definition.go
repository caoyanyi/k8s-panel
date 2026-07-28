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
	customResourceDefinitionCollectionPath         = "/apis/apiextensions.k8s.io/v1/customresourcedefinitions"
	customResourceDefinitionListPageSize           = "250"
	customResourceDefinitionMaxListPages           = 8
	customResourceDefinitionMaxListItems           = 2000
	customResourceDefinitionMaxListBytes     int64 = 16 * 1024 * 1024
	customResourceDefinitionMaxDetailBytes   int64 = 2 * 1024 * 1024
	customResourceDefinitionMaxContinueBytes       = 16 * 1024
	maxCustomResourceDefinitionVersions            = 64
	maxCustomResourceDefinitionNames               = 32
	maxCustomResourceDefinitionConditions          = 32
	maxCustomResourceDefinitionNestedEntries       = 256
	maxCustomResourceDefinitionTextBytes           = 256
)

type customResourceDefinitionVersionSource struct {
	Name       string `json:"name"`
	Served     bool   `json:"served"`
	Storage    bool   `json:"storage"`
	Deprecated bool   `json:"deprecated"`
}

type customResourceDefinitionConditionSource struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Reason             string    `json:"reason"`
	ObservedGeneration int64     `json:"observedGeneration"`
	LastTransitionTime time.Time `json:"lastTransitionTime"`
}

type customResourceDefinitionSource struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name              string    `json:"name"`
		Namespace         string    `json:"namespace"`
		Generation        int64     `json:"generation"`
		CreationTimestamp time.Time `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		Group string `json:"group"`
		Scope string `json:"scope"`
		Names struct {
			Plural     string   `json:"plural"`
			Singular   string   `json:"singular"`
			Kind       string   `json:"kind"`
			ListKind   string   `json:"listKind"`
			ShortNames []string `json:"shortNames"`
			Categories []string `json:"categories"`
		} `json:"names"`
		Versions   []customResourceDefinitionVersionSource `json:"versions"`
		Conversion *struct {
			Strategy string `json:"strategy"`
		} `json:"conversion"`
	} `json:"spec"`
	Status struct {
		ObservedGeneration int64                                     `json:"observedGeneration"`
		StoredVersions     []string                                  `json:"storedVersions"`
		Conditions         []customResourceDefinitionConditionSource `json:"conditions"`
	} `json:"status"`
}

func (c *Client) CustomResourceDefinitions(ctx context.Context) ([]domain.KubernetesCustomResourceDefinition, error) {
	query := url.Values{"limit": {customResourceDefinitionListPageSize}}
	items := make([]domain.KubernetesCustomResourceDefinition, 0)
	var totalBytes int64
	for page := 0; page < customResourceDefinitionMaxListPages; page++ {
		payload, _, err := c.getPayload(
			ctx, customResourceDefinitionCollectionPath, query, kubernetesPartialMetadataListAccept, maxResponseBytes, false,
		)
		if err != nil {
			return nil, err
		}
		if int64(len(payload)) > customResourceDefinitionMaxListBytes-totalBytes {
			return nil, fmt.Errorf("Kubernetes CRD list exceeded safe byte limit: %w", domain.ErrUpstream)
		}
		totalBytes += int64(len(payload))

		var response partialObjectMetadataList
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, fmt.Errorf("decode Kubernetes CRD metadata list: %w", domain.ErrUpstream)
		}
		if response.APIVersion != "meta.k8s.io/v1" || response.Kind != "PartialObjectMetadataList" {
			return nil, fmt.Errorf("unsupported Kubernetes CRD metadata list: %w", domain.ErrUpstream)
		}
		if len(response.Items) > customResourceDefinitionMaxListItems-len(items) {
			return nil, fmt.Errorf("Kubernetes CRD list exceeded safe item limit: %w", domain.ErrUpstream)
		}
		for _, raw := range response.Items {
			metadata, err := decodePartialObjectMetadataForScope(raw, false)
			if err != nil {
				return nil, err
			}
			resource, group, err := splitCustomResourceDefinitionName(metadata.Name)
			if err != nil {
				return nil, fmt.Errorf("invalid Kubernetes CRD metadata identity: %w", domain.ErrUpstream)
			}
			items = append(items, domain.KubernetesCustomResourceDefinition{
				Name: metadata.Name, Resource: resource, Group: group, CreatedAt: metadata.CreationTimestamp,
			})
		}
		if response.Metadata.Continue == "" {
			sort.Slice(items, func(i, j int) bool {
				if items[i].Group != items[j].Group {
					return items[i].Group < items[j].Group
				}
				return items[i].Resource < items[j].Resource
			})
			return items, nil
		}
		if !validCustomResourceDefinitionContinue(response.Metadata.Continue) {
			return nil, fmt.Errorf("invalid Kubernetes CRD continuation token: %w", domain.ErrUpstream)
		}
		query.Set("continue", response.Metadata.Continue)
	}
	return nil, fmt.Errorf("Kubernetes CRD list exceeded safe page limit: %w", domain.ErrUpstream)
}

func (c *Client) CustomResourceDefinition(
	ctx context.Context,
	name string,
) (domain.KubernetesCustomResourceDefinitionDetail, error) {
	if err := domain.ValidateCustomResourceDefinitionName(name); err != nil {
		return domain.KubernetesCustomResourceDefinitionDetail{}, err
	}
	payload, _, err := c.getPayload(
		ctx, customResourceDefinitionCollectionPath+"/"+name, nil, "application/json",
		customResourceDefinitionMaxDetailBytes, false,
	)
	if err != nil {
		return domain.KubernetesCustomResourceDefinitionDetail{}, err
	}
	return decodeCustomResourceDefinition(payload, name)
}

func decodeCustomResourceDefinition(
	payload []byte,
	expectedName string,
) (domain.KubernetesCustomResourceDefinitionDetail, error) {
	var source customResourceDefinitionSource
	if err := json.Unmarshal(payload, &source); err != nil {
		return domain.KubernetesCustomResourceDefinitionDetail{}, fmt.Errorf("decode Kubernetes CRD detail: %w", domain.ErrUpstream)
	}
	resource, group, err := validateCustomResourceDefinitionIdentity(source, expectedName)
	if err != nil {
		return domain.KubernetesCustomResourceDefinitionDetail{}, err
	}
	nestedEntries := len(source.Spec.Versions) + len(source.Spec.Names.ShortNames) + len(source.Spec.Names.Categories) +
		len(source.Status.StoredVersions) + len(source.Status.Conditions)
	if nestedEntries > maxCustomResourceDefinitionNestedEntries {
		return domain.KubernetesCustomResourceDefinitionDetail{}, fmt.Errorf("Kubernetes CRD detail exceeded safe nested entry limit: %w", domain.ErrUpstream)
	}

	shortNames, shortNamesTruncated, err := boundedCustomResourceDefinitionNames(source.Spec.Names.ShortNames)
	if err != nil {
		return domain.KubernetesCustomResourceDefinitionDetail{}, err
	}
	categories, categoriesTruncated, err := boundedCustomResourceDefinitionNames(source.Spec.Names.Categories)
	if err != nil {
		return domain.KubernetesCustomResourceDefinitionDetail{}, err
	}
	versions, versionsTruncated, versionNames, err := decodeCustomResourceDefinitionVersions(source.Spec.Versions)
	if err != nil {
		return domain.KubernetesCustomResourceDefinitionDetail{}, err
	}
	storedVersions, storedVersionsTruncated, err := boundedCustomResourceDefinitionStoredVersions(
		source.Status.StoredVersions, versionNames,
	)
	if err != nil {
		return domain.KubernetesCustomResourceDefinitionDetail{}, err
	}
	conditions, conditionsTruncated, err := decodeCustomResourceDefinitionConditions(
		source.Status.Conditions, source.Metadata.Generation,
	)
	if err != nil {
		return domain.KubernetesCustomResourceDefinitionDetail{}, err
	}

	conversionStrategy := "None"
	conversionStrategyDefaulted := source.Spec.Conversion == nil || source.Spec.Conversion.Strategy == ""
	if source.Spec.Conversion != nil && source.Spec.Conversion.Strategy != "" {
		conversionStrategy = source.Spec.Conversion.Strategy
	}
	if conversionStrategy != "None" && conversionStrategy != "Webhook" {
		return domain.KubernetesCustomResourceDefinitionDetail{}, fmt.Errorf("invalid Kubernetes CRD conversion strategy: %w", domain.ErrUpstream)
	}
	singular := source.Spec.Names.Singular
	if singular == "" {
		singular = strings.ToLower(source.Spec.Names.Kind)
	}
	listKind := source.Spec.Names.ListKind
	if listKind == "" {
		listKind = source.Spec.Names.Kind + "List"
	}
	if !validCustomResourceDefinitionLowerIdentifier(singular) || !validCustomResourceDefinitionKind(listKind) {
		return domain.KubernetesCustomResourceDefinitionDetail{}, fmt.Errorf("invalid Kubernetes CRD names: %w", domain.ErrUpstream)
	}
	if source.Status.ObservedGeneration < 0 || source.Status.ObservedGeneration > source.Metadata.Generation {
		return domain.KubernetesCustomResourceDefinitionDetail{}, fmt.Errorf("invalid Kubernetes CRD observed generation: %w", domain.ErrUpstream)
	}

	return domain.KubernetesCustomResourceDefinitionDetail{
		KubernetesCustomResourceDefinition: domain.KubernetesCustomResourceDefinition{
			Name: source.Metadata.Name, Resource: resource, Group: group, CreatedAt: source.Metadata.CreationTimestamp,
		},
		Scope: source.Spec.Scope, Singular: singular, Kind: source.Spec.Names.Kind, ListKind: listKind,
		ShortNames: shortNames, ShortNameCount: len(source.Spec.Names.ShortNames), ShortNamesTruncated: shortNamesTruncated,
		Categories: categories, CategoryCount: len(source.Spec.Names.Categories), CategoriesTruncated: categoriesTruncated,
		Versions: versions, VersionCount: len(source.Spec.Versions), VersionsTruncated: versionsTruncated,
		StoredVersions: storedVersions, StoredVersionCount: len(source.Status.StoredVersions),
		StoredVersionsTruncated: storedVersionsTruncated,
		ConversionStrategy:      conversionStrategy, ConversionStrategyDefaulted: conversionStrategyDefaulted,
		Generation: source.Metadata.Generation, ObservedGeneration: source.Status.ObservedGeneration,
		Conditions: conditions, ConditionCount: len(source.Status.Conditions), ConditionsTruncated: conditionsTruncated,
	}, nil
}

func validateCustomResourceDefinitionIdentity(
	source customResourceDefinitionSource,
	expectedName string,
) (string, string, error) {
	resource, group, err := splitCustomResourceDefinitionName(source.Metadata.Name)
	if err != nil || source.APIVersion != "apiextensions.k8s.io/v1" || source.Kind != "CustomResourceDefinition" ||
		source.Metadata.Name != expectedName || source.Metadata.Namespace != "" || source.Metadata.Generation < 1 ||
		source.Metadata.CreationTimestamp.IsZero() || source.Spec.Group != group || source.Spec.Names.Plural != resource ||
		!validCustomResourceDefinitionKind(source.Spec.Names.Kind) ||
		(source.Spec.Scope != "Cluster" && source.Spec.Scope != "Namespaced") {
		return "", "", fmt.Errorf("invalid Kubernetes CRD detail identity: %w", domain.ErrUpstream)
	}
	return resource, group, nil
}

func decodeCustomResourceDefinitionVersions(
	source []customResourceDefinitionVersionSource,
) ([]domain.KubernetesCustomResourceDefinitionVersion, bool, map[string]struct{}, error) {
	if len(source) == 0 {
		return nil, false, nil, fmt.Errorf("Kubernetes CRD has no versions: %w", domain.ErrUpstream)
	}
	versionNames := make(map[string]struct{}, len(source))
	storageCount := 0
	limit := min(len(source), maxCustomResourceDefinitionVersions)
	versions := make([]domain.KubernetesCustomResourceDefinitionVersion, 0, limit)
	for index, version := range source {
		if !validCustomResourceDefinitionLowerIdentifier(version.Name) {
			return nil, false, nil, fmt.Errorf("invalid Kubernetes CRD version: %w", domain.ErrUpstream)
		}
		if _, duplicate := versionNames[version.Name]; duplicate {
			return nil, false, nil, fmt.Errorf("duplicate Kubernetes CRD version: %w", domain.ErrUpstream)
		}
		versionNames[version.Name] = struct{}{}
		if version.Storage {
			storageCount++
		}
		if index < limit {
			versions = append(versions, domain.KubernetesCustomResourceDefinitionVersion{
				Name: version.Name, Served: version.Served, Storage: version.Storage, Deprecated: version.Deprecated,
			})
		}
	}
	if storageCount != 1 {
		return nil, false, nil, fmt.Errorf("invalid Kubernetes CRD storage version: %w", domain.ErrUpstream)
	}
	return versions, len(source) > limit, versionNames, nil
}

func boundedCustomResourceDefinitionNames(values []string) ([]string, bool, error) {
	seen := make(map[string]struct{}, len(values))
	limit := min(len(values), maxCustomResourceDefinitionNames)
	result := make([]string, 0, limit)
	for index, value := range values {
		if !validCustomResourceDefinitionLowerIdentifier(value) {
			return nil, false, fmt.Errorf("invalid Kubernetes CRD resource name: %w", domain.ErrUpstream)
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, false, fmt.Errorf("duplicate Kubernetes CRD resource name: %w", domain.ErrUpstream)
		}
		seen[value] = struct{}{}
		if index < limit {
			result = append(result, value)
		}
	}
	return result, len(values) > limit, nil
}

func boundedCustomResourceDefinitionStoredVersions(
	values []string,
	versionNames map[string]struct{},
) ([]string, bool, error) {
	seen := make(map[string]struct{}, len(values))
	limit := min(len(values), maxCustomResourceDefinitionNames)
	result := make([]string, 0, limit)
	for index, value := range values {
		if !validCustomResourceDefinitionLowerIdentifier(value) {
			return nil, false, fmt.Errorf("invalid Kubernetes CRD stored version: %w", domain.ErrUpstream)
		}
		if _, exists := versionNames[value]; !exists {
			return nil, false, fmt.Errorf("unknown Kubernetes CRD stored version: %w", domain.ErrUpstream)
		}
		if _, duplicate := seen[value]; duplicate {
			return nil, false, fmt.Errorf("duplicate Kubernetes CRD stored version: %w", domain.ErrUpstream)
		}
		seen[value] = struct{}{}
		if index < limit {
			result = append(result, value)
		}
	}
	return result, len(values) > limit, nil
}

func decodeCustomResourceDefinitionConditions(
	source []customResourceDefinitionConditionSource,
	generation int64,
) ([]domain.KubernetesCustomResourceDefinitionCondition, bool, error) {
	seen := make(map[string]struct{}, len(source))
	limit := min(len(source), maxCustomResourceDefinitionConditions)
	conditions := make([]domain.KubernetesCustomResourceDefinitionCondition, 0, limit)
	for index, condition := range source {
		if !validCustomResourceDefinitionText(condition.Type, true) ||
			!validCustomResourceDefinitionText(condition.Reason, false) ||
			(condition.Status != "True" && condition.Status != "False" && condition.Status != "Unknown") ||
			condition.ObservedGeneration < 0 || condition.ObservedGeneration > generation || condition.LastTransitionTime.IsZero() {
			return nil, false, fmt.Errorf("invalid Kubernetes CRD condition: %w", domain.ErrUpstream)
		}
		if _, duplicate := seen[condition.Type]; duplicate {
			return nil, false, fmt.Errorf("duplicate Kubernetes CRD condition: %w", domain.ErrUpstream)
		}
		seen[condition.Type] = struct{}{}
		if index < limit {
			conditions = append(conditions, domain.KubernetesCustomResourceDefinitionCondition{
				Type: condition.Type, Status: condition.Status, Reason: condition.Reason,
				ObservedGeneration: condition.ObservedGeneration, LastTransitionTime: condition.LastTransitionTime,
			})
		}
	}
	return conditions, len(source) > limit, nil
}

func splitCustomResourceDefinitionName(name string) (string, string, error) {
	if err := domain.ValidateCustomResourceDefinitionName(name); err != nil {
		return "", "", err
	}
	resource, group, _ := strings.Cut(name, ".")
	return resource, group, nil
}

func validCustomResourceDefinitionContinue(value string) bool {
	return value != "" && len(value) <= customResourceDefinitionMaxContinueBytes && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

func validCustomResourceDefinitionLowerIdentifier(value string) bool {
	return domain.ValidateNamespace(value) == nil
}

func validCustomResourceDefinitionKind(value string) bool {
	if value == "" || len(value) > 128 || value != strings.TrimSpace(value) {
		return false
	}
	for index, char := range value {
		if char > unicode.MaxASCII || (!unicode.IsLetter(char) && (index == 0 || !unicode.IsDigit(char))) {
			return false
		}
	}
	return true
}

func validCustomResourceDefinitionText(value string, required bool) bool {
	if value == "" {
		return !required
	}
	return len(value) <= maxCustomResourceDefinitionTextBytes && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}
