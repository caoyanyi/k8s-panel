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
	kubernetesPartialMetadataAccept           = "application/json;as=PartialObjectMetadata;g=meta.k8s.io;v=v1"
	kubernetesPartialMetadataListAccept       = "application/json;as=PartialObjectMetadataList;g=meta.k8s.io;v=v1"
	accessListPageSize                        = "250"
	accessMaxListPages                        = 8
	accessMaxListItems                        = 2000
	accessMaxListBytes                  int64 = 16 * 1024 * 1024
	maxAccessDetailBytes                int64 = 2 * 1024 * 1024
	maxAccessContinueBytes                    = 16 * 1024
	maxAccessRules                            = 128
	maxAccessSubjects                         = 128
	maxAccessRuleValues                       = 64
	maxAccessStringBytes                      = 512
)

type accessResourceSpec struct {
	apiVersion string
	kind       string
	resource   string
	namespaced bool
}

type partialObjectMetadataList struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Continue string `json:"continue"`
	} `json:"metadata"`
	Items []json.RawMessage `json:"items"`
}

func (c *Client) AccessResources(
	ctx context.Context,
	kind domain.KubernetesAccessResourceKind,
	namespace string,
) ([]domain.KubernetesAccessResource, error) {
	if err := domain.ValidateAccessResourceScope(kind, namespace); err != nil {
		return nil, err
	}
	spec, err := accessSpec(kind)
	if err != nil {
		return nil, err
	}
	path := accessCollectionPath(spec, namespace)
	query := url.Values{"limit": {accessListPageSize}}
	items := make([]domain.KubernetesAccessResource, 0)
	var totalBytes int64
	for page := 0; page < accessMaxListPages; page++ {
		payload, _, err := c.getPayload(ctx, path, query, kubernetesPartialMetadataListAccept, maxResponseBytes, false)
		if err != nil {
			return nil, err
		}
		if int64(len(payload)) > accessMaxListBytes-totalBytes {
			return nil, fmt.Errorf("Kubernetes access list exceeded safe byte limit: %w", domain.ErrUpstream)
		}
		totalBytes += int64(len(payload))

		var response partialObjectMetadataList
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, fmt.Errorf("decode Kubernetes access metadata list: %w", domain.ErrUpstream)
		}
		if response.APIVersion != "meta.k8s.io/v1" || response.Kind != "PartialObjectMetadataList" {
			return nil, fmt.Errorf("unsupported Kubernetes access metadata list: %w", domain.ErrUpstream)
		}
		if len(response.Items) > accessMaxListItems-len(items) {
			return nil, fmt.Errorf("Kubernetes access list exceeded safe item limit: %w", domain.ErrUpstream)
		}
		for _, raw := range response.Items {
			metadata, err := decodePartialObjectMetadataForScope(raw, spec.namespaced)
			if err != nil {
				return nil, err
			}
			if spec.namespaced && metadata.Namespace != namespace {
				return nil, fmt.Errorf("Kubernetes access metadata exceeded namespace scope: %w", domain.ErrUpstream)
			}
			items = append(items, domain.KubernetesAccessResource{
				Kind: spec.kind, Namespace: metadata.Namespace, Name: metadata.Name, CreatedAt: metadata.CreationTimestamp,
			})
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
		if !validAccessContinue(response.Metadata.Continue) {
			return nil, fmt.Errorf("invalid Kubernetes access continuation token: %w", domain.ErrUpstream)
		}
		query.Set("continue", response.Metadata.Continue)
	}
	return nil, fmt.Errorf("Kubernetes access list exceeded safe page limit: %w", domain.ErrUpstream)
}

func (c *Client) AccessResourceDetail(
	ctx context.Context,
	reference domain.KubernetesAccessResourceReference,
) (domain.KubernetesAccessResourceDetail, error) {
	if err := domain.ValidateAccessResourceReference(reference); err != nil {
		return domain.KubernetesAccessResourceDetail{}, err
	}
	spec, err := accessSpec(reference.Kind)
	if err != nil {
		return domain.KubernetesAccessResourceDetail{}, err
	}
	path := accessCollectionPath(spec, reference.Namespace) + "/" + reference.Name
	payload, _, err := c.getPayload(ctx, path, nil, "application/json", maxAccessDetailBytes, false)
	if err != nil {
		return domain.KubernetesAccessResourceDetail{}, err
	}
	return decodeAccessResourceDetail(payload, reference, spec)
}

func accessSpec(kind domain.KubernetesAccessResourceKind) (accessResourceSpec, error) {
	switch kind {
	case domain.AccessResourceServiceAccounts:
		return accessResourceSpec{apiVersion: "v1", kind: "ServiceAccount", resource: "serviceaccounts", namespaced: true}, nil
	case domain.AccessResourceRoles:
		return accessResourceSpec{apiVersion: "rbac.authorization.k8s.io/v1", kind: "Role", resource: "roles", namespaced: true}, nil
	case domain.AccessResourceRoleBindings:
		return accessResourceSpec{apiVersion: "rbac.authorization.k8s.io/v1", kind: "RoleBinding", resource: "rolebindings", namespaced: true}, nil
	case domain.AccessResourceClusterRoles:
		return accessResourceSpec{apiVersion: "rbac.authorization.k8s.io/v1", kind: "ClusterRole", resource: "clusterroles"}, nil
	case domain.AccessResourceClusterRoleBindings:
		return accessResourceSpec{apiVersion: "rbac.authorization.k8s.io/v1", kind: "ClusterRoleBinding", resource: "clusterrolebindings"}, nil
	default:
		return accessResourceSpec{}, domain.Invalid("kind", "must be a supported access resource kind")
	}
}

func accessCollectionPath(spec accessResourceSpec, namespace string) string {
	prefix := "/apis/rbac.authorization.k8s.io/v1"
	if spec.apiVersion == "v1" {
		prefix = "/api/v1"
	}
	if spec.namespaced {
		return prefix + "/namespaces/" + namespace + "/" + spec.resource
	}
	return prefix + "/" + spec.resource
}

func validAccessContinue(value string) bool {
	return value != "" && len(value) <= maxAccessContinueBytes && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

func decodeAccessResourceDetail(
	payload []byte,
	reference domain.KubernetesAccessResourceReference,
	spec accessResourceSpec,
) (domain.KubernetesAccessResourceDetail, error) {
	var response struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name              string    `json:"name"`
			Namespace         string    `json:"namespace"`
			CreationTimestamp time.Time `json:"creationTimestamp"`
		} `json:"metadata"`
		Rules                        []json.RawMessage `json:"rules"`
		RoleRef                      json.RawMessage   `json:"roleRef"`
		Subjects                     []json.RawMessage `json:"subjects"`
		AutomountServiceAccountToken *bool             `json:"automountServiceAccountToken"`
		Secrets                      []json.RawMessage `json:"secrets"`
		ImagePullSecrets             []json.RawMessage `json:"imagePullSecrets"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return domain.KubernetesAccessResourceDetail{}, fmt.Errorf("decode Kubernetes access detail: %w", domain.ErrUpstream)
	}
	if response.APIVersion != spec.apiVersion || response.Kind != spec.kind ||
		response.Metadata.Name != reference.Name || response.Metadata.Namespace != reference.Namespace ||
		!validKubernetesMetadataString(response.Metadata.Name) || response.Metadata.CreationTimestamp.IsZero() {
		return domain.KubernetesAccessResourceDetail{}, fmt.Errorf("invalid Kubernetes access detail identity: %w", domain.ErrUpstream)
	}
	if spec.namespaced {
		if domain.ValidateNamespace(response.Metadata.Namespace) != nil {
			return domain.KubernetesAccessResourceDetail{}, fmt.Errorf("invalid Kubernetes access detail namespace: %w", domain.ErrUpstream)
		}
	} else if response.Metadata.Namespace != "" {
		return domain.KubernetesAccessResourceDetail{}, fmt.Errorf("cluster-scoped Kubernetes access detail contains a namespace: %w", domain.ErrUpstream)
	}

	detail := domain.KubernetesAccessResourceDetail{
		KubernetesAccessResource: domain.KubernetesAccessResource{
			Kind: spec.kind, Namespace: reference.Namespace, Name: reference.Name,
			CreatedAt: response.Metadata.CreationTimestamp,
		},
		Rules:    make([]domain.KubernetesRoleRule, 0),
		Subjects: make([]domain.KubernetesAccessSubject, 0),
	}
	switch reference.Kind {
	case domain.AccessResourceServiceAccounts:
		detail.AutomountServiceAccountToken = cloneBool(response.AutomountServiceAccountToken)
		detail.SecretCount = len(response.Secrets)
		detail.ImagePullSecretCount = len(response.ImagePullSecrets)
	case domain.AccessResourceRoles, domain.AccessResourceClusterRoles:
		if err := decodeAccessRules(&detail, response.Rules); err != nil {
			return domain.KubernetesAccessResourceDetail{}, err
		}
	case domain.AccessResourceRoleBindings, domain.AccessResourceClusterRoleBindings:
		roleRef, err := decodeAccessRoleReference(response.RoleRef, reference.Kind, reference.Namespace)
		if err != nil {
			return domain.KubernetesAccessResourceDetail{}, err
		}
		detail.RoleRef = &roleRef
		if err := decodeAccessSubjects(&detail, response.Subjects, reference.Namespace); err != nil {
			return domain.KubernetesAccessResourceDetail{}, err
		}
	default:
		return domain.KubernetesAccessResourceDetail{}, fmt.Errorf("unsupported Kubernetes access detail kind: %w", domain.ErrUpstream)
	}
	return detail, nil
}

func decodeAccessRules(detail *domain.KubernetesAccessResourceDetail, rawRules []json.RawMessage) error {
	detail.RuleCount = len(rawRules)
	detail.RulesTruncated = len(rawRules) > maxAccessRules
	limit := min(len(rawRules), maxAccessRules)
	detail.Rules = make([]domain.KubernetesRoleRule, 0, limit)
	for _, raw := range rawRules[:limit] {
		var source struct {
			APIGroups       []string `json:"apiGroups"`
			Resources       []string `json:"resources"`
			ResourceNames   []string `json:"resourceNames"`
			Verbs           []string `json:"verbs"`
			NonResourceURLs []string `json:"nonResourceURLs"`
		}
		if err := json.Unmarshal(raw, &source); err != nil {
			return fmt.Errorf("decode Kubernetes access rule: %w", domain.ErrUpstream)
		}
		rule := domain.KubernetesRoleRule{}
		var truncated bool
		var err error
		if rule.APIGroups, truncated, err = boundedAccessStrings(source.APIGroups); err != nil {
			return err
		}
		detail.RulesTruncated = detail.RulesTruncated || truncated
		if rule.Resources, truncated, err = boundedAccessStrings(source.Resources); err != nil {
			return err
		}
		detail.RulesTruncated = detail.RulesTruncated || truncated
		if rule.ResourceNames, truncated, err = boundedAccessStrings(source.ResourceNames); err != nil {
			return err
		}
		detail.RulesTruncated = detail.RulesTruncated || truncated
		if rule.Verbs, truncated, err = boundedAccessStrings(source.Verbs); err != nil {
			return err
		}
		detail.RulesTruncated = detail.RulesTruncated || truncated
		if rule.NonResourceURLs, truncated, err = boundedAccessStrings(source.NonResourceURLs); err != nil {
			return err
		}
		detail.RulesTruncated = detail.RulesTruncated || truncated
		detail.Rules = append(detail.Rules, rule)
	}
	return nil
}

func boundedAccessStrings(values []string) ([]string, bool, error) {
	truncated := len(values) > maxAccessRuleValues
	limit := min(len(values), maxAccessRuleValues)
	result := make([]string, 0, limit)
	for _, value := range values[:limit] {
		if len(value) > maxAccessStringBytes || value != strings.TrimSpace(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return nil, false, fmt.Errorf("invalid Kubernetes access rule value: %w", domain.ErrUpstream)
		}
		result = append(result, value)
	}
	return result, truncated, nil
}

func decodeAccessRoleReference(
	raw json.RawMessage,
	bindingKind domain.KubernetesAccessResourceKind,
	bindingNamespace string,
) (domain.KubernetesRoleReference, error) {
	var source struct {
		APIGroup string `json:"apiGroup"`
		Kind     string `json:"kind"`
		Name     string `json:"name"`
	}
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &source) != nil ||
		source.APIGroup != "rbac.authorization.k8s.io" {
		return domain.KubernetesRoleReference{}, fmt.Errorf("invalid Kubernetes role reference: %w", domain.ErrUpstream)
	}
	reference := domain.KubernetesAccessResourceReference{Name: source.Name}
	switch source.Kind {
	case "Role":
		if bindingKind != domain.AccessResourceRoleBindings {
			return domain.KubernetesRoleReference{}, fmt.Errorf("invalid Kubernetes role reference scope: %w", domain.ErrUpstream)
		}
		reference.Kind = domain.AccessResourceRoles
		reference.Namespace = bindingNamespace
	case "ClusterRole":
		reference.Kind = domain.AccessResourceClusterRoles
	default:
		return domain.KubernetesRoleReference{}, fmt.Errorf("invalid Kubernetes role reference kind: %w", domain.ErrUpstream)
	}
	if domain.ValidateAccessResourceReference(reference) != nil {
		return domain.KubernetesRoleReference{}, fmt.Errorf("invalid Kubernetes role reference name: %w", domain.ErrUpstream)
	}
	return domain.KubernetesRoleReference{Kind: source.Kind, Name: source.Name}, nil
}

func decodeAccessSubjects(
	detail *domain.KubernetesAccessResourceDetail,
	rawSubjects []json.RawMessage,
	bindingNamespace string,
) error {
	detail.SubjectCount = len(rawSubjects)
	detail.SubjectsTruncated = len(rawSubjects) > maxAccessSubjects
	limit := min(len(rawSubjects), maxAccessSubjects)
	detail.Subjects = make([]domain.KubernetesAccessSubject, 0, limit)
	for _, raw := range rawSubjects[:limit] {
		var source struct {
			APIGroup  string `json:"apiGroup"`
			Kind      string `json:"kind"`
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
		}
		if err := json.Unmarshal(raw, &source); err != nil || !validAccessSubjectName(source.Name) {
			return fmt.Errorf("invalid Kubernetes access subject: %w", domain.ErrUpstream)
		}
		subject := domain.KubernetesAccessSubject{Kind: source.Kind, Name: source.Name}
		switch source.Kind {
		case "ServiceAccount":
			if source.APIGroup != "" {
				return fmt.Errorf("invalid Kubernetes service account subject group: %w", domain.ErrUpstream)
			}
			subject.Namespace = source.Namespace
			if subject.Namespace == "" {
				subject.Namespace = bindingNamespace
			}
			if domain.ValidateAccessResourceReference(domain.KubernetesAccessResourceReference{
				Kind: domain.AccessResourceServiceAccounts, Namespace: subject.Namespace, Name: subject.Name,
			}) != nil {
				return fmt.Errorf("invalid Kubernetes service account subject: %w", domain.ErrUpstream)
			}
		case "User", "Group":
			if source.APIGroup != "rbac.authorization.k8s.io" {
				return fmt.Errorf("invalid Kubernetes identity subject: %w", domain.ErrUpstream)
			}
		default:
			return fmt.Errorf("invalid Kubernetes access subject kind: %w", domain.ErrUpstream)
		}
		detail.Subjects = append(detail.Subjects, subject)
	}
	return nil
}

func validAccessSubjectName(value string) bool {
	return value != "" && len(value) <= maxAccessStringBytes && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
