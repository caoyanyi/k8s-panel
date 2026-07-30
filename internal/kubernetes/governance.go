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
	"unicode/utf8"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

const (
	governanceListPageSize              = "250"
	maxGovernanceListPages              = 4
	maxGovernanceListItems              = 1000
	maxGovernancePageBytes        int64 = 2 * 1024 * 1024
	maxGovernanceListBytes        int64 = 4 * 1024 * 1024
	maxGovernanceContinueBytes          = 16 * 1024
	maxGovernanceScalarBytes            = 512
	maxQuotaResourcesPerObject          = 64
	maxQuotaScopesPerObject             = 16
	maxLimitConstraintsPerObject        = 128
	maxGovernanceProjectedEntries       = 4096
	maxGovernanceProjectedScopes        = 1024
)

type governanceMetadata struct {
	Name              string    `json:"name"`
	Namespace         string    `json:"namespace"`
	Generation        int64     `json:"generation"`
	CreationTimestamp time.Time `json:"creationTimestamp"`
}

type governanceList struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Continue string `json:"continue"`
	} `json:"metadata"`
	Items []json.RawMessage `json:"items"`
}

type limitRangeSource struct {
	Type                 string            `json:"type"`
	DefaultRequest       map[string]string `json:"defaultRequest"`
	Default              map[string]string `json:"default"`
	Min                  map[string]string `json:"min"`
	Max                  map[string]string `json:"max"`
	MaxLimitRequestRatio map[string]string `json:"maxLimitRequestRatio"`
}

func (c *Client) ResourceQuotas(ctx context.Context, namespace string) ([]domain.KubernetesResourceQuota, error) {
	if err := domain.ValidateNamespace(namespace); err != nil {
		return nil, err
	}
	items, err := c.listGovernanceRaw(
		ctx,
		"/api/v1/namespaces/"+namespace+"/resourcequotas",
		"v1",
		"ResourceQuotaList",
	)
	if err != nil {
		return nil, err
	}
	remainingResources := maxGovernanceProjectedEntries
	remainingScopes := maxGovernanceProjectedScopes
	quotas := make([]domain.KubernetesResourceQuota, 0, len(items))
	for _, item := range items {
		quota, err := decodeResourceQuota(item, namespace, &remainingResources, &remainingScopes)
		if err != nil {
			return nil, err
		}
		quotas = append(quotas, quota)
	}
	sort.Slice(quotas, func(i, j int) bool { return quotas[i].Name < quotas[j].Name })
	return quotas, nil
}

func (c *Client) LimitRanges(ctx context.Context, namespace string) ([]domain.KubernetesLimitRange, error) {
	if err := domain.ValidateNamespace(namespace); err != nil {
		return nil, err
	}
	items, err := c.listGovernanceRaw(
		ctx,
		"/api/v1/namespaces/"+namespace+"/limitranges",
		"v1",
		"LimitRangeList",
	)
	if err != nil {
		return nil, err
	}
	remainingConstraints := maxGovernanceProjectedEntries
	limitRanges := make([]domain.KubernetesLimitRange, 0, len(items))
	for _, item := range items {
		limitRange, err := decodeLimitRange(item, namespace, &remainingConstraints)
		if err != nil {
			return nil, err
		}
		limitRanges = append(limitRanges, limitRange)
	}
	sort.Slice(limitRanges, func(i, j int) bool { return limitRanges[i].Name < limitRanges[j].Name })
	return limitRanges, nil
}

func (c *Client) listGovernanceRaw(
	ctx context.Context,
	path, expectedAPIVersion, expectedKind string,
) ([]json.RawMessage, error) {
	query := url.Values{"limit": {governanceListPageSize}}
	items := make([]json.RawMessage, 0)
	var totalBytes int64
	for page := 0; page < maxGovernanceListPages; page++ {
		payload, _, err := c.getPayload(ctx, path, query, "application/json", maxGovernancePageBytes, false)
		if err != nil {
			return nil, err
		}
		if int64(len(payload)) > maxGovernanceListBytes-totalBytes {
			return nil, fmt.Errorf("Kubernetes governance list exceeded safe byte limit: %w", domain.ErrUpstream)
		}
		totalBytes += int64(len(payload))

		var response governanceList
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, fmt.Errorf("decode Kubernetes governance list: %w", domain.ErrUpstream)
		}
		if response.APIVersion != expectedAPIVersion || response.Kind != expectedKind {
			return nil, fmt.Errorf("unsupported Kubernetes governance list: %w", domain.ErrUpstream)
		}
		if len(response.Items) > maxGovernanceListItems-len(items) {
			return nil, fmt.Errorf("Kubernetes governance list exceeded safe item limit: %w", domain.ErrUpstream)
		}
		items = append(items, response.Items...)
		if response.Metadata.Continue == "" {
			return items, nil
		}
		if !validGovernanceContinue(response.Metadata.Continue) {
			return nil, fmt.Errorf("invalid Kubernetes governance continuation token: %w", domain.ErrUpstream)
		}
		query.Set("continue", response.Metadata.Continue)
	}
	return nil, fmt.Errorf("Kubernetes governance list exceeded safe page limit: %w", domain.ErrUpstream)
}

func decodeResourceQuota(
	raw json.RawMessage,
	expectedNamespace string,
	remainingResources, remainingScopes *int,
) (domain.KubernetesResourceQuota, error) {
	var source struct {
		APIVersion string             `json:"apiVersion"`
		Kind       string             `json:"kind"`
		Metadata   governanceMetadata `json:"metadata"`
		Spec       struct {
			Hard          map[string]string `json:"hard"`
			Scopes        []string          `json:"scopes"`
			ScopeSelector struct {
				MatchExpressions []json.RawMessage `json:"matchExpressions"`
			} `json:"scopeSelector"`
		} `json:"spec"`
		Status struct {
			Hard map[string]string `json:"hard"`
			Used map[string]string `json:"used"`
		} `json:"status"`
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		return domain.KubernetesResourceQuota{}, fmt.Errorf("decode Kubernetes ResourceQuota: %w", domain.ErrUpstream)
	}
	if err := validateGovernanceIdentity(
		source.APIVersion, source.Kind, "v1", "ResourceQuota", source.Metadata, expectedNamespace,
	); err != nil {
		return domain.KubernetesResourceQuota{}, err
	}

	resources, resourceCount, resourcesTruncated, err := projectQuotaResources(
		source.Spec.Hard, source.Status.Hard, source.Status.Used, remainingResources,
	)
	if err != nil {
		return domain.KubernetesResourceQuota{}, err
	}
	scopes, scopesTruncated, err := projectQuotaScopes(source.Spec.Scopes, remainingScopes)
	if err != nil {
		return domain.KubernetesResourceQuota{}, err
	}
	return domain.KubernetesResourceQuota{
		Namespace: source.Metadata.Namespace, Name: source.Metadata.Name,
		Scopes: scopes, ScopeCount: len(source.Spec.Scopes), ScopesTruncated: scopesTruncated,
		ScopeSelectorCount: len(source.Spec.ScopeSelector.MatchExpressions),
		Resources:          resources, ResourceCount: resourceCount, ResourcesTruncated: resourcesTruncated,
		CreatedAt: source.Metadata.CreationTimestamp,
	}, nil
}

func projectQuotaResources(
	specHard, statusHard, used map[string]string,
	remaining *int,
) ([]domain.KubernetesQuotaResource, int, bool, error) {
	keys := sortedGovernanceKeys(specHard, statusHard, used)
	limit := min(len(keys), maxQuotaResourcesPerObject, max(0, *remaining))
	resources := make([]domain.KubernetesQuotaResource, 0, limit)
	for _, key := range keys[:limit] {
		name, err := governanceScalar(key, false)
		if err != nil {
			return nil, 0, false, fmt.Errorf("invalid Kubernetes quota resource name: %w", domain.ErrUpstream)
		}
		hard, observed := statusHard[key]
		if !observed {
			hard = specHard[key]
		}
		hard, err = governanceScalar(hard, true)
		if err != nil {
			return nil, 0, false, fmt.Errorf("invalid Kubernetes quota hard value: %w", domain.ErrUpstream)
		}
		usedValue, err := governanceScalar(used[key], true)
		if err != nil {
			return nil, 0, false, fmt.Errorf("invalid Kubernetes quota used value: %w", domain.ErrUpstream)
		}
		resources = append(resources, domain.KubernetesQuotaResource{
			Name: name, Hard: hard, Used: usedValue, Observed: observed,
		})
	}
	*remaining -= limit
	return resources, len(keys), len(keys) > limit, nil
}

func projectQuotaScopes(scopes []string, remaining *int) ([]string, bool, error) {
	sortedScopes := append([]string(nil), scopes...)
	sort.Strings(sortedScopes)
	limit := min(len(sortedScopes), maxQuotaScopesPerObject, max(0, *remaining))
	projected := make([]string, 0, limit)
	for _, scope := range sortedScopes[:limit] {
		value, err := governanceScalar(scope, false)
		if err != nil {
			return nil, false, fmt.Errorf("invalid Kubernetes quota scope: %w", domain.ErrUpstream)
		}
		projected = append(projected, value)
	}
	*remaining -= limit
	return projected, len(sortedScopes) > limit, nil
}

func decodeLimitRange(
	raw json.RawMessage,
	expectedNamespace string,
	remaining *int,
) (domain.KubernetesLimitRange, error) {
	var source struct {
		APIVersion string             `json:"apiVersion"`
		Kind       string             `json:"kind"`
		Metadata   governanceMetadata `json:"metadata"`
		Spec       struct {
			Limits []limitRangeSource `json:"limits"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		return domain.KubernetesLimitRange{}, fmt.Errorf("decode Kubernetes LimitRange: %w", domain.ErrUpstream)
	}
	if err := validateGovernanceIdentity(
		source.APIVersion, source.Kind, "v1", "LimitRange", source.Metadata, expectedNamespace,
	); err != nil {
		return domain.KubernetesLimitRange{}, err
	}

	sort.SliceStable(source.Spec.Limits, func(i, j int) bool {
		return source.Spec.Limits[i].Type < source.Spec.Limits[j].Type
	})
	objectRemaining := min(maxLimitConstraintsPerObject, max(0, *remaining))
	constraints := make([]domain.KubernetesLimitRangeConstraint, 0, objectRemaining)
	constraintCount := 0
	for _, limit := range source.Spec.Limits {
		keys := sortedGovernanceKeys(
			limit.DefaultRequest, limit.Default, limit.Min, limit.Max, limit.MaxLimitRequestRatio,
		)
		constraintCount += len(keys)
		projectCount := min(len(keys), objectRemaining-len(constraints))
		for _, key := range keys[:projectCount] {
			constraint, err := projectLimitRangeConstraint(limit, key)
			if err != nil {
				return domain.KubernetesLimitRange{}, err
			}
			constraints = append(constraints, constraint)
		}
	}
	*remaining -= len(constraints)
	return domain.KubernetesLimitRange{
		Namespace: source.Metadata.Namespace, Name: source.Metadata.Name,
		Constraints: constraints, ConstraintCount: constraintCount,
		ConstraintsTruncated: constraintCount > len(constraints), CreatedAt: source.Metadata.CreationTimestamp,
	}, nil
}

func projectLimitRangeConstraint(
	source limitRangeSource,
	resource string,
) (domain.KubernetesLimitRangeConstraint, error) {
	constraintType, err := governanceScalar(source.Type, false)
	if err != nil {
		return domain.KubernetesLimitRangeConstraint{}, fmt.Errorf("invalid Kubernetes limit range type: %w", domain.ErrUpstream)
	}
	resource, err = governanceScalar(resource, false)
	if err != nil {
		return domain.KubernetesLimitRangeConstraint{}, fmt.Errorf("invalid Kubernetes limit range resource: %w", domain.ErrUpstream)
	}
	rawValues := []string{
		source.DefaultRequest[resource],
		source.Default[resource],
		source.Min[resource],
		source.Max[resource],
		source.MaxLimitRequestRatio[resource],
	}
	values := make([]string, len(rawValues))
	for index, value := range rawValues {
		values[index], err = governanceScalar(value, true)
		if err != nil {
			return domain.KubernetesLimitRangeConstraint{}, fmt.Errorf("invalid Kubernetes limit range value: %w", domain.ErrUpstream)
		}
	}
	return domain.KubernetesLimitRangeConstraint{
		Type: constraintType, Resource: resource,
		DefaultRequest: values[0], Default: values[1], Min: values[2], Max: values[3],
		MaxLimitRequestRatio: values[4],
	}, nil
}

func validateGovernanceIdentity(
	apiVersion, kind, expectedAPIVersion, expectedKind string,
	metadata governanceMetadata,
	expectedNamespace string,
) error {
	if err := validateGovernanceObjectIdentity(apiVersion, kind, expectedAPIVersion, expectedKind, metadata); err != nil {
		return err
	}
	if metadata.Namespace != expectedNamespace || domain.ValidateNamespace(metadata.Namespace) != nil {
		return fmt.Errorf("Kubernetes governance object exceeded namespace scope: %w", domain.ErrUpstream)
	}
	return nil
}

func validateGovernanceIdentityAnyNamespace(
	apiVersion, kind, expectedAPIVersion, expectedKind string,
	metadata governanceMetadata,
) error {
	if err := validateGovernanceObjectIdentity(apiVersion, kind, expectedAPIVersion, expectedKind, metadata); err != nil {
		return err
	}
	if domain.ValidateNamespace(metadata.Namespace) != nil {
		return fmt.Errorf("Kubernetes governance object exceeded namespace scope: %w", domain.ErrUpstream)
	}
	return nil
}

func validateGovernanceObjectIdentity(
	apiVersion, kind, expectedAPIVersion, expectedKind string,
	metadata governanceMetadata,
) error {
	if apiVersion != expectedAPIVersion || kind != expectedKind || !validKubernetesMetadataString(metadata.Name) ||
		metadata.CreationTimestamp.IsZero() {
		return fmt.Errorf("invalid Kubernetes governance object identity: %w", domain.ErrUpstream)
	}
	return nil
}

func sortedGovernanceKeys(values ...map[string]string) []string {
	unique := make(map[string]struct{})
	for _, entries := range values {
		for key := range entries {
			unique[key] = struct{}{}
		}
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func governanceScalar(value string, allowEmpty bool) (string, error) {
	if (!allowEmpty && value == "") || len(value) > maxGovernanceScalarBytes || !utf8.ValidString(value) ||
		value != strings.TrimSpace(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", domain.ErrUpstream
	}
	return value, nil
}

func validGovernanceContinue(value string) bool {
	return value != "" && len(value) <= maxGovernanceContinueBytes && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}
