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
	validatingAdmissionPolicyCollectionPath              = "/apis/admissionregistration.k8s.io/v1/validatingadmissionpolicies"
	validatingAdmissionPolicyBindingCollectionPath       = "/apis/admissionregistration.k8s.io/v1/validatingadmissionpolicybindings"
	admissionPolicyListPageSize                          = "250"
	admissionPolicyMaxListPages                          = 4
	admissionPolicyMaxListItems                          = 1000
	admissionPolicyMaxListBytes                    int64 = 4 * 1024 * 1024
	admissionPolicyMaxDetailBytes                  int64 = 2 * 1024 * 1024
	admissionPolicyMaxContinueBytes                      = 16 * 1024
	maxAdmissionPolicyRules                              = 128
	maxAdmissionPolicyRuleValues                         = 128
	maxAdmissionPolicyValidations                        = 128
	maxAdmissionPolicyAuditAnnotations                   = 128
	maxAdmissionPolicyMatchConditions                    = 64
	maxAdmissionPolicyVariables                          = 128
	maxAdmissionPolicyExpressionWarnings                 = 128
	maxAdmissionPolicyConditions                         = 32
	maxAdmissionPolicySelectorEntries                    = 128
	maxAdmissionPolicyNestedEntries                      = 8192
	maxAdmissionPolicyTextBytes                          = 1024
)

type admissionPolicyResourceSpec struct {
	kind           domain.KubernetesAdmissionPolicyResourceKind
	objectKind     string
	collectionPath string
}

type admissionPolicyMetadataSource struct {
	Name              string    `json:"name"`
	Namespace         string    `json:"namespace"`
	Generation        int64     `json:"generation"`
	CreationTimestamp time.Time `json:"creationTimestamp"`
}

type admissionPolicyRuleSource struct {
	Operations    []string          `json:"operations"`
	APIGroups     []string          `json:"apiGroups"`
	APIVersions   []string          `json:"apiVersions"`
	Resources     []string          `json:"resources"`
	ResourceNames []json.RawMessage `json:"resourceNames"`
	Scope         *string           `json:"scope"`
}

type admissionPolicySelectorSource struct {
	MatchLabels      map[string]json.RawMessage `json:"matchLabels"`
	MatchExpressions []struct{}                 `json:"matchExpressions"`
}

type admissionPolicyMatchResourcesSource struct {
	NamespaceSelector    *admissionPolicySelectorSource `json:"namespaceSelector"`
	ObjectSelector       *admissionPolicySelectorSource `json:"objectSelector"`
	ResourceRules        []admissionPolicyRuleSource    `json:"resourceRules"`
	ExcludeResourceRules []admissionPolicyRuleSource    `json:"excludeResourceRules"`
	MatchPolicy          *string                        `json:"matchPolicy"`
}

type admissionPolicyParamRefSource struct {
	Name                    string                         `json:"name"`
	Namespace               string                         `json:"namespace"`
	Selector                *admissionPolicySelectorSource `json:"selector"`
	ParameterNotFoundAction *string                        `json:"parameterNotFoundAction"`
}

type validatingAdmissionPolicySource struct {
	APIVersion string                        `json:"apiVersion"`
	Kind       string                        `json:"kind"`
	Metadata   admissionPolicyMetadataSource `json:"metadata"`
	Spec       struct {
		ParamKind *struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
		} `json:"paramKind"`
		MatchConstraints *admissionPolicyMatchResourcesSource `json:"matchConstraints"`
		Validations      []struct{}                           `json:"validations"`
		FailurePolicy    *string                              `json:"failurePolicy"`
		AuditAnnotations []struct{}                           `json:"auditAnnotations"`
		MatchConditions  []struct{}                           `json:"matchConditions"`
		Variables        []struct{}                           `json:"variables"`
	} `json:"spec"`
	Status struct {
		ObservedGeneration int64 `json:"observedGeneration"`
		TypeChecking       *struct {
			ExpressionWarnings []struct{} `json:"expressionWarnings"`
		} `json:"typeChecking"`
		Conditions []struct{} `json:"conditions"`
	} `json:"status"`
}

type validatingAdmissionPolicyBindingSource struct {
	APIVersion string                        `json:"apiVersion"`
	Kind       string                        `json:"kind"`
	Metadata   admissionPolicyMetadataSource `json:"metadata"`
	Spec       struct {
		PolicyName        string                               `json:"policyName"`
		ParamRef          *admissionPolicyParamRefSource       `json:"paramRef"`
		MatchResources    *admissionPolicyMatchResourcesSource `json:"matchResources"`
		ValidationActions []string                             `json:"validationActions"`
	} `json:"spec"`
}

func (c *Client) ValidatingAdmissionPolicies(ctx context.Context) ([]domain.KubernetesAdmissionPolicyResource, error) {
	return c.listAdmissionPolicyResources(ctx, admissionPolicySpec(domain.AdmissionPolicyResourcePolicy))
}

func (c *Client) ValidatingAdmissionPolicyBindings(ctx context.Context) ([]domain.KubernetesAdmissionPolicyResource, error) {
	return c.listAdmissionPolicyResources(ctx, admissionPolicySpec(domain.AdmissionPolicyResourceBinding))
}

func (c *Client) listAdmissionPolicyResources(
	ctx context.Context,
	spec admissionPolicyResourceSpec,
) ([]domain.KubernetesAdmissionPolicyResource, error) {
	query := url.Values{"limit": {admissionPolicyListPageSize}}
	items := make([]domain.KubernetesAdmissionPolicyResource, 0)
	seen := make(map[string]struct{})
	var totalBytes int64
	for page := 0; page < admissionPolicyMaxListPages; page++ {
		remainingBytes := admissionPolicyMaxListBytes - totalBytes
		if remainingBytes <= 0 {
			return nil, fmt.Errorf("Kubernetes admission policy metadata exceeded safe byte limit: %w", domain.ErrUpstream)
		}
		payload, _, err := c.getPayload(
			ctx, spec.collectionPath, query, kubernetesPartialMetadataListAccept, remainingBytes, false,
		)
		if err != nil {
			return nil, err
		}
		totalBytes += int64(len(payload))

		var response partialObjectMetadataList
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, fmt.Errorf("decode Kubernetes admission policy metadata list: %w", domain.ErrUpstream)
		}
		if response.APIVersion != "meta.k8s.io/v1" || response.Kind != "PartialObjectMetadataList" {
			return nil, fmt.Errorf("unsupported Kubernetes admission policy metadata list: %w", domain.ErrUpstream)
		}
		if len(response.Items) > admissionPolicyMaxListItems-len(items) {
			return nil, fmt.Errorf("Kubernetes admission policy metadata exceeded safe item limit: %w", domain.ErrUpstream)
		}
		for _, raw := range response.Items {
			metadata, err := decodePartialObjectMetadataForScope(raw, false)
			if err != nil {
				return nil, err
			}
			if domain.ValidateAdmissionPolicyResourceName(metadata.Name) != nil {
				return nil, fmt.Errorf("invalid Kubernetes admission policy metadata identity: %w", domain.ErrUpstream)
			}
			if _, exists := seen[metadata.Name]; exists {
				return nil, fmt.Errorf("duplicate Kubernetes admission policy metadata: %w", domain.ErrUpstream)
			}
			seen[metadata.Name] = struct{}{}
			items = append(items, domain.KubernetesAdmissionPolicyResource{
				Kind: spec.kind, Name: metadata.Name, CreatedAt: metadata.CreationTimestamp,
			})
		}
		if response.Metadata.Continue == "" {
			sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
			return items, nil
		}
		if !validAdmissionPolicyContinue(response.Metadata.Continue) {
			return nil, fmt.Errorf("invalid Kubernetes admission policy continuation token: %w", domain.ErrUpstream)
		}
		query.Set("continue", response.Metadata.Continue)
	}
	return nil, fmt.Errorf("Kubernetes admission policy metadata exceeded safe page limit: %w", domain.ErrUpstream)
}

func (c *Client) ValidatingAdmissionPolicy(
	ctx context.Context,
	name string,
) (domain.KubernetesValidatingAdmissionPolicyDetail, error) {
	if err := domain.ValidateAdmissionPolicyResourceName(name); err != nil {
		return domain.KubernetesValidatingAdmissionPolicyDetail{}, err
	}
	payload, _, err := c.getPayload(
		ctx, validatingAdmissionPolicyCollectionPath+"/"+name, nil, "application/json", admissionPolicyMaxDetailBytes, false,
	)
	if err != nil {
		return domain.KubernetesValidatingAdmissionPolicyDetail{}, err
	}
	return decodeValidatingAdmissionPolicy(payload, name)
}

func (c *Client) ValidatingAdmissionPolicyBinding(
	ctx context.Context,
	name string,
) (domain.KubernetesValidatingAdmissionPolicyBindingDetail, error) {
	if err := domain.ValidateAdmissionPolicyResourceName(name); err != nil {
		return domain.KubernetesValidatingAdmissionPolicyBindingDetail{}, err
	}
	payload, _, err := c.getPayload(
		ctx, validatingAdmissionPolicyBindingCollectionPath+"/"+name, nil, "application/json", admissionPolicyMaxDetailBytes, false,
	)
	if err != nil {
		return domain.KubernetesValidatingAdmissionPolicyBindingDetail{}, err
	}
	return decodeValidatingAdmissionPolicyBinding(payload, name)
}

func decodeValidatingAdmissionPolicy(
	payload []byte,
	expectedName string,
) (domain.KubernetesValidatingAdmissionPolicyDetail, error) {
	var source validatingAdmissionPolicySource
	if err := json.Unmarshal(payload, &source); err != nil {
		return domain.KubernetesValidatingAdmissionPolicyDetail{}, fmt.Errorf("decode Kubernetes validating admission policy: %w", domain.ErrUpstream)
	}
	if err := validateAdmissionPolicyIdentity(
		source.APIVersion, source.Kind, source.Metadata, admissionPolicySpec(domain.AdmissionPolicyResourcePolicy), expectedName,
	); err != nil {
		return domain.KubernetesValidatingAdmissionPolicyDetail{}, err
	}
	if len(source.Spec.Validations) > maxAdmissionPolicyValidations ||
		len(source.Spec.AuditAnnotations) > maxAdmissionPolicyAuditAnnotations ||
		len(source.Spec.MatchConditions) > maxAdmissionPolicyMatchConditions ||
		len(source.Spec.Variables) > maxAdmissionPolicyVariables ||
		len(source.Status.Conditions) > maxAdmissionPolicyConditions ||
		(source.Status.TypeChecking != nil && len(source.Status.TypeChecking.ExpressionWarnings) > maxAdmissionPolicyExpressionWarnings) ||
		(len(source.Spec.Validations) == 0 && len(source.Spec.AuditAnnotations) == 0) {
		return domain.KubernetesValidatingAdmissionPolicyDetail{}, fmt.Errorf("Kubernetes admission policy exceeded safe field limit: %w", domain.ErrUpstream)
	}
	match, matchEntries, err := summarizeAdmissionPolicyMatch(source.Spec.MatchConstraints, true)
	if err != nil {
		return domain.KubernetesValidatingAdmissionPolicyDetail{}, err
	}
	nestedEntries := matchEntries + len(source.Spec.Validations) + len(source.Spec.AuditAnnotations) +
		len(source.Spec.MatchConditions) + len(source.Spec.Variables) + len(source.Status.Conditions)
	if source.Status.TypeChecking != nil {
		nestedEntries += len(source.Status.TypeChecking.ExpressionWarnings)
	}
	if nestedEntries > maxAdmissionPolicyNestedEntries {
		return domain.KubernetesValidatingAdmissionPolicyDetail{}, fmt.Errorf("Kubernetes admission policy exceeded safe nested entry limit: %w", domain.ErrUpstream)
	}
	if source.Status.ObservedGeneration < 0 || source.Status.ObservedGeneration > source.Metadata.Generation {
		return domain.KubernetesValidatingAdmissionPolicyDetail{}, fmt.Errorf("invalid Kubernetes admission policy observed generation: %w", domain.ErrUpstream)
	}

	detail := domain.KubernetesValidatingAdmissionPolicyDetail{
		KubernetesAdmissionPolicyResource: domain.KubernetesAdmissionPolicyResource{
			Kind: domain.AdmissionPolicyResourcePolicy, Name: expectedName, CreatedAt: source.Metadata.CreationTimestamp,
		},
		Generation: source.Metadata.Generation, FailurePolicy: "Fail", FailurePolicyDefaulted: source.Spec.FailurePolicy == nil,
		Match: match, ValidationCount: len(source.Spec.Validations), AuditAnnotationCount: len(source.Spec.AuditAnnotations),
		MatchConditionCount: len(source.Spec.MatchConditions), VariableCount: len(source.Spec.Variables),
		ObservedGeneration: source.Status.ObservedGeneration, TypeCheckingObserved: source.Status.TypeChecking != nil,
		ConditionCount: len(source.Status.Conditions),
	}
	if source.Spec.FailurePolicy != nil {
		detail.FailurePolicy = *source.Spec.FailurePolicy
	}
	if detail.FailurePolicy != "Fail" && detail.FailurePolicy != "Ignore" {
		return domain.KubernetesValidatingAdmissionPolicyDetail{}, fmt.Errorf("invalid Kubernetes admission policy failure policy: %w", domain.ErrUpstream)
	}
	if source.Spec.ParamKind != nil {
		if !validAdmissionPolicyAPIVersion(source.Spec.ParamKind.APIVersion) || !validAdmissionPolicyKind(source.Spec.ParamKind.Kind) {
			return domain.KubernetesValidatingAdmissionPolicyDetail{}, fmt.Errorf("invalid Kubernetes admission policy parameter kind: %w", domain.ErrUpstream)
		}
		detail.ParamKindConfigured = true
		detail.ParamAPIVersion = source.Spec.ParamKind.APIVersion
		detail.ParamKind = source.Spec.ParamKind.Kind
	}
	if source.Status.TypeChecking != nil {
		detail.ExpressionWarningCount = len(source.Status.TypeChecking.ExpressionWarnings)
	}
	return detail, nil
}

func decodeValidatingAdmissionPolicyBinding(
	payload []byte,
	expectedName string,
) (domain.KubernetesValidatingAdmissionPolicyBindingDetail, error) {
	var source validatingAdmissionPolicyBindingSource
	if err := json.Unmarshal(payload, &source); err != nil {
		return domain.KubernetesValidatingAdmissionPolicyBindingDetail{}, fmt.Errorf("decode Kubernetes validating admission policy binding: %w", domain.ErrUpstream)
	}
	if err := validateAdmissionPolicyIdentity(
		source.APIVersion, source.Kind, source.Metadata, admissionPolicySpec(domain.AdmissionPolicyResourceBinding), expectedName,
	); err != nil {
		return domain.KubernetesValidatingAdmissionPolicyBindingDetail{}, err
	}
	if domain.ValidateAdmissionPolicyResourceName(source.Spec.PolicyName) != nil {
		return domain.KubernetesValidatingAdmissionPolicyBindingDetail{}, fmt.Errorf("invalid Kubernetes admission policy binding policy reference: %w", domain.ErrUpstream)
	}
	actions, err := canonicalAdmissionPolicyActions(source.Spec.ValidationActions)
	if err != nil {
		return domain.KubernetesValidatingAdmissionPolicyBindingDetail{}, err
	}
	match, nestedEntries, err := summarizeAdmissionPolicyMatch(source.Spec.MatchResources, false)
	if err != nil {
		return domain.KubernetesValidatingAdmissionPolicyBindingDetail{}, err
	}
	detail := domain.KubernetesValidatingAdmissionPolicyBindingDetail{
		KubernetesAdmissionPolicyResource: domain.KubernetesAdmissionPolicyResource{
			Kind: domain.AdmissionPolicyResourceBinding, Name: expectedName, CreatedAt: source.Metadata.CreationTimestamp,
		},
		Generation: source.Metadata.Generation, PolicyName: source.Spec.PolicyName, ValidationActions: actions, Match: match,
	}
	if source.Spec.ParamRef != nil {
		paramEntries, err := decodeAdmissionPolicyParamRef(&detail, source.Spec.ParamRef)
		if err != nil {
			return domain.KubernetesValidatingAdmissionPolicyBindingDetail{}, err
		}
		nestedEntries += paramEntries
	}
	if nestedEntries > maxAdmissionPolicyNestedEntries {
		return domain.KubernetesValidatingAdmissionPolicyBindingDetail{}, fmt.Errorf("Kubernetes admission policy binding exceeded safe nested entry limit: %w", domain.ErrUpstream)
	}
	return detail, nil
}

func admissionPolicySpec(kind domain.KubernetesAdmissionPolicyResourceKind) admissionPolicyResourceSpec {
	if kind == domain.AdmissionPolicyResourceBinding {
		return admissionPolicyResourceSpec{
			kind: kind, objectKind: "ValidatingAdmissionPolicyBinding", collectionPath: validatingAdmissionPolicyBindingCollectionPath,
		}
	}
	return admissionPolicyResourceSpec{
		kind:       domain.AdmissionPolicyResourcePolicy,
		objectKind: "ValidatingAdmissionPolicy", collectionPath: validatingAdmissionPolicyCollectionPath,
	}
}

func validateAdmissionPolicyIdentity(
	apiVersion, kind string,
	metadata admissionPolicyMetadataSource,
	spec admissionPolicyResourceSpec,
	expectedName string,
) error {
	if apiVersion != "admissionregistration.k8s.io/v1" || kind != spec.objectKind || metadata.Name != expectedName ||
		metadata.Namespace != "" || metadata.Generation < 1 || metadata.CreationTimestamp.IsZero() ||
		domain.ValidateAdmissionPolicyResourceName(metadata.Name) != nil {
		return fmt.Errorf("invalid Kubernetes admission policy resource identity: %w", domain.ErrUpstream)
	}
	return nil
}

func summarizeAdmissionPolicyMatch(
	source *admissionPolicyMatchResourcesSource,
	required bool,
) (domain.KubernetesAdmissionMatchSummary, int, error) {
	if source == nil {
		if required {
			return domain.KubernetesAdmissionMatchSummary{}, 0, fmt.Errorf("missing Kubernetes admission policy match constraints: %w", domain.ErrUpstream)
		}
		return domain.KubernetesAdmissionMatchSummary{}, 0, nil
	}
	if len(source.ResourceRules) > maxAdmissionPolicyRules || len(source.ExcludeResourceRules) > maxAdmissionPolicyRules ||
		(required && len(source.ResourceRules) == 0) {
		return domain.KubernetesAdmissionMatchSummary{}, 0, fmt.Errorf("Kubernetes admission policy match rules exceeded safe limit: %w", domain.ErrUpstream)
	}
	summary := domain.KubernetesAdmissionMatchSummary{
		Configured: true, MatchPolicy: "Equivalent", MatchPolicyDefaulted: source.MatchPolicy == nil,
		ResourceRuleCount: len(source.ResourceRules), ExcludeResourceRuleCount: len(source.ExcludeResourceRules),
	}
	if source.MatchPolicy != nil {
		summary.MatchPolicy = *source.MatchPolicy
	}
	if summary.MatchPolicy != "Equivalent" && summary.MatchPolicy != "Exact" {
		return domain.KubernetesAdmissionMatchSummary{}, 0, fmt.Errorf("invalid Kubernetes admission policy match policy: %w", domain.ErrUpstream)
	}
	nestedEntries := len(source.ResourceRules) + len(source.ExcludeResourceRules)
	if source.NamespaceSelector != nil {
		if err := summarizeAdmissionPolicySelector(
			source.NamespaceSelector, &summary.NamespaceSelectorLabelCount, &summary.NamespaceSelectorExpressionCount,
		); err != nil {
			return domain.KubernetesAdmissionMatchSummary{}, 0, err
		}
		nestedEntries += summary.NamespaceSelectorLabelCount + summary.NamespaceSelectorExpressionCount
	}
	if source.ObjectSelector != nil {
		if err := summarizeAdmissionPolicySelector(
			source.ObjectSelector, &summary.ObjectSelectorLabelCount, &summary.ObjectSelectorExpressionCount,
		); err != nil {
			return domain.KubernetesAdmissionMatchSummary{}, 0, err
		}
		nestedEntries += summary.ObjectSelectorLabelCount + summary.ObjectSelectorExpressionCount
	}
	for _, rules := range [][]admissionPolicyRuleSource{source.ResourceRules, source.ExcludeResourceRules} {
		for _, rule := range rules {
			entries, err := summarizeAdmissionPolicyRule(&summary, rule)
			if err != nil {
				return domain.KubernetesAdmissionMatchSummary{}, 0, err
			}
			nestedEntries += entries
			if nestedEntries > maxAdmissionPolicyNestedEntries {
				return domain.KubernetesAdmissionMatchSummary{}, 0, fmt.Errorf("Kubernetes admission policy match exceeded safe nested entry limit: %w", domain.ErrUpstream)
			}
		}
	}
	return summary, nestedEntries, nil
}

func summarizeAdmissionPolicySelector(source *admissionPolicySelectorSource, labels, expressions *int) error {
	if len(source.MatchLabels) > maxAdmissionPolicySelectorEntries ||
		len(source.MatchExpressions) > maxAdmissionPolicySelectorEntries {
		return fmt.Errorf("Kubernetes admission policy selector exceeded safe entry limit: %w", domain.ErrUpstream)
	}
	*labels = len(source.MatchLabels)
	*expressions = len(source.MatchExpressions)
	return nil
}

func summarizeAdmissionPolicyRule(summary *domain.KubernetesAdmissionMatchSummary, rule admissionPolicyRuleSource) (int, error) {
	if len(rule.Operations) == 0 || len(rule.APIGroups) == 0 || len(rule.APIVersions) == 0 || len(rule.Resources) == 0 ||
		len(rule.Operations) > maxAdmissionPolicyRuleValues || len(rule.APIGroups) > maxAdmissionPolicyRuleValues ||
		len(rule.APIVersions) > maxAdmissionPolicyRuleValues || len(rule.Resources) > maxAdmissionPolicyRuleValues ||
		len(rule.ResourceNames) > maxAdmissionPolicyRuleValues {
		return 0, fmt.Errorf("invalid Kubernetes admission policy resource rule: %w", domain.ErrUpstream)
	}
	if err := validateAdmissionWebhookOperations(rule.Operations); err != nil {
		return 0, fmt.Errorf("invalid Kubernetes admission policy operation: %w", domain.ErrUpstream)
	}
	for index, values := range [][]string{rule.APIGroups, rule.APIVersions, rule.Resources} {
		if !validAdmissionWebhookRuleValues(values) || (index > 0 && containsEmptyString(values)) ||
			(containsString(values, "*") && len(values) != 1) {
			return 0, fmt.Errorf("invalid Kubernetes admission policy rule value: %w", domain.ErrUpstream)
		}
	}
	if rule.Scope != nil && *rule.Scope != "Cluster" && *rule.Scope != "Namespaced" && *rule.Scope != "*" {
		return 0, fmt.Errorf("invalid Kubernetes admission policy rule scope: %w", domain.ErrUpstream)
	}
	summary.OperationCount += len(rule.Operations)
	summary.APIGroupCount += len(rule.APIGroups)
	summary.APIVersionCount += len(rule.APIVersions)
	summary.ResourceCount += len(rule.Resources)
	return len(rule.Operations) + len(rule.APIGroups) + len(rule.APIVersions) + len(rule.Resources) + len(rule.ResourceNames), nil
}

func canonicalAdmissionPolicyActions(actions []string) ([]string, error) {
	if len(actions) == 0 || len(actions) > 3 {
		return nil, fmt.Errorf("invalid Kubernetes admission policy binding actions: %w", domain.ErrUpstream)
	}
	seen := make(map[string]struct{}, len(actions))
	for _, action := range actions {
		if action != "Deny" && action != "Warn" && action != "Audit" {
			return nil, fmt.Errorf("invalid Kubernetes admission policy binding action: %w", domain.ErrUpstream)
		}
		if _, duplicate := seen[action]; duplicate {
			return nil, fmt.Errorf("duplicate Kubernetes admission policy binding action: %w", domain.ErrUpstream)
		}
		seen[action] = struct{}{}
	}
	if _, deny := seen["Deny"]; deny {
		if _, warn := seen["Warn"]; warn {
			return nil, fmt.Errorf("conflicting Kubernetes admission policy binding actions: %w", domain.ErrUpstream)
		}
	}
	ordered := make([]string, 0, len(actions))
	for _, action := range []string{"Deny", "Warn", "Audit"} {
		if _, exists := seen[action]; exists {
			ordered = append(ordered, action)
		}
	}
	return ordered, nil
}

func decodeAdmissionPolicyParamRef(
	detail *domain.KubernetesValidatingAdmissionPolicyBindingDetail,
	source *admissionPolicyParamRefSource,
) (int, error) {
	hasName := source.Name != ""
	hasSelector := source.Selector != nil
	if hasName == hasSelector || (hasName && domain.ValidateAdmissionPolicyResourceName(source.Name) != nil) ||
		(source.Namespace != "" && domain.ValidateNamespace(source.Namespace) != nil) || source.ParameterNotFoundAction == nil ||
		(*source.ParameterNotFoundAction != "Allow" && *source.ParameterNotFoundAction != "Deny") {
		return 0, fmt.Errorf("invalid Kubernetes admission policy binding parameter reference: %w", domain.ErrUpstream)
	}
	detail.ParamRefConfigured = true
	detail.ParamNamespace = source.Namespace
	detail.ParameterNotFoundAction = *source.ParameterNotFoundAction
	if hasName {
		detail.ParamRefMode = "name"
		return 0, nil
	}
	detail.ParamRefMode = "selector"
	if err := summarizeAdmissionPolicySelector(
		source.Selector, &detail.ParamSelectorLabelCount, &detail.ParamSelectorExpressionCount,
	); err != nil {
		return 0, err
	}
	return detail.ParamSelectorLabelCount + detail.ParamSelectorExpressionCount, nil
}

func validAdmissionPolicyAPIVersion(value string) bool {
	if value == "" || len(value) > maxAdmissionPolicyTextBytes || value != strings.TrimSpace(value) ||
		strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return false
	}
	group, version, hasGroup := strings.Cut(value, "/")
	if !hasGroup {
		return validAdmissionWebhookVersion(value)
	}
	return group != "" && version != "" && !strings.Contains(version, "/") &&
		domain.ValidateAPIServiceName(version+"."+group) == nil
}

func validAdmissionPolicyKind(value string) bool {
	if value == "" || len(value) > 63 {
		return false
	}
	for index, character := range value {
		if index == 0 {
			if (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') {
				return false
			}
			continue
		}
		if (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func validAdmissionPolicyContinue(value string) bool {
	return value != "" && len(value) <= admissionPolicyMaxContinueBytes && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

func containsEmptyString(values []string) bool {
	return containsString(values, "")
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
