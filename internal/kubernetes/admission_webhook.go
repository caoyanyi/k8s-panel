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
	validatingAdmissionWebhookCollectionPath       = "/apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations"
	mutatingAdmissionWebhookCollectionPath         = "/apis/admissionregistration.k8s.io/v1/mutatingwebhookconfigurations"
	admissionWebhookListPageSize                   = "250"
	admissionWebhookMaxListPages                   = 4
	admissionWebhookMaxListItems                   = 1000
	admissionWebhookMaxListBytes             int64 = 4 * 1024 * 1024
	admissionWebhookMaxDetailBytes           int64 = 2 * 1024 * 1024
	admissionWebhookMaxContinueBytes               = 16 * 1024
	maxAdmissionWebhooks                           = 64
	maxAdmissionWebhookRules                       = 128
	maxAdmissionWebhookRuleValues                  = 128
	maxAdmissionWebhookMatchConditions             = 64
	maxAdmissionWebhookReviewVersions              = 32
	maxAdmissionWebhookSelectorEntries             = 128
	maxAdmissionWebhookNestedEntries               = 8192
	maxAdmissionWebhookTextBytes                   = 1024
	defaultAdmissionWebhookServicePort       int32 = 443
	defaultAdmissionWebhookTimeout           int32 = 10
)

type admissionWebhookResourceSpec struct {
	objectKind     string
	collectionPath string
}

type admissionWebhookRuleSource struct {
	Operations  []string `json:"operations"`
	APIGroups   []string `json:"apiGroups"`
	APIVersions []string `json:"apiVersions"`
	Resources   []string `json:"resources"`
	Scope       *string  `json:"scope"`
}

type admissionWebhookSelectorSource struct {
	MatchLabels      map[string]json.RawMessage `json:"matchLabels"`
	MatchExpressions []struct{}                 `json:"matchExpressions"`
}

type admissionWebhookSource struct {
	Name         string `json:"name"`
	ClientConfig struct {
		Service *struct {
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
			Path      string `json:"path"`
			Port      *int32 `json:"port"`
		} `json:"service"`
		URL      *string `json:"url"`
		CABundle []byte  `json:"caBundle"`
	} `json:"clientConfig"`
	Rules                   []admissionWebhookRuleSource   `json:"rules"`
	FailurePolicy           *string                        `json:"failurePolicy"`
	MatchPolicy             *string                        `json:"matchPolicy"`
	NamespaceSelector       admissionWebhookSelectorSource `json:"namespaceSelector"`
	ObjectSelector          admissionWebhookSelectorSource `json:"objectSelector"`
	SideEffects             *string                        `json:"sideEffects"`
	TimeoutSeconds          *int32                         `json:"timeoutSeconds"`
	AdmissionReviewVersions []string                       `json:"admissionReviewVersions"`
	MatchConditions         []struct{}                     `json:"matchConditions"`
	ReinvocationPolicy      *string                        `json:"reinvocationPolicy"`
}

type admissionWebhookConfigurationSource struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name              string    `json:"name"`
		Namespace         string    `json:"namespace"`
		Generation        int64     `json:"generation"`
		CreationTimestamp time.Time `json:"creationTimestamp"`
	} `json:"metadata"`
	Webhooks []admissionWebhookSource `json:"webhooks"`
}

func (c *Client) AdmissionWebhookConfigurations(
	ctx context.Context,
	kind domain.KubernetesAdmissionWebhookConfigurationKind,
) ([]domain.KubernetesAdmissionWebhookConfiguration, error) {
	if err := domain.ValidateAdmissionWebhookConfigurationKind(kind); err != nil {
		return nil, err
	}
	spec, err := admissionWebhookSpec(kind)
	if err != nil {
		return nil, err
	}
	query := url.Values{"limit": {admissionWebhookListPageSize}}
	items := make([]domain.KubernetesAdmissionWebhookConfiguration, 0)
	seen := make(map[string]struct{})
	var totalBytes int64
	for page := 0; page < admissionWebhookMaxListPages; page++ {
		remainingBytes := admissionWebhookMaxListBytes - totalBytes
		payload, _, err := c.getPayload(
			ctx, spec.collectionPath, query, kubernetesPartialMetadataListAccept, remainingBytes, false,
		)
		if err != nil {
			return nil, err
		}
		totalBytes += int64(len(payload))

		var response partialObjectMetadataList
		if err := json.Unmarshal(payload, &response); err != nil {
			return nil, fmt.Errorf("decode Kubernetes admission webhook metadata list: %w", domain.ErrUpstream)
		}
		if response.APIVersion != "meta.k8s.io/v1" || response.Kind != "PartialObjectMetadataList" {
			return nil, fmt.Errorf("unsupported Kubernetes admission webhook metadata list: %w", domain.ErrUpstream)
		}
		if len(response.Items) > admissionWebhookMaxListItems-len(items) {
			return nil, fmt.Errorf("Kubernetes admission webhook list exceeded safe item limit: %w", domain.ErrUpstream)
		}
		for _, raw := range response.Items {
			metadata, err := decodePartialObjectMetadataForScope(raw, false)
			if err != nil {
				return nil, err
			}
			if err := domain.ValidateAdmissionWebhookConfigurationName(metadata.Name); err != nil {
				return nil, fmt.Errorf("invalid Kubernetes admission webhook metadata identity: %w", domain.ErrUpstream)
			}
			if _, exists := seen[metadata.Name]; exists {
				return nil, fmt.Errorf("duplicate Kubernetes admission webhook metadata: %w", domain.ErrUpstream)
			}
			seen[metadata.Name] = struct{}{}
			items = append(items, domain.KubernetesAdmissionWebhookConfiguration{
				Kind: kind, Name: metadata.Name, CreatedAt: metadata.CreationTimestamp,
			})
		}
		if response.Metadata.Continue == "" {
			sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
			return items, nil
		}
		if !validAdmissionWebhookContinue(response.Metadata.Continue) {
			return nil, fmt.Errorf("invalid Kubernetes admission webhook continuation token: %w", domain.ErrUpstream)
		}
		query.Set("continue", response.Metadata.Continue)
	}
	return nil, fmt.Errorf("Kubernetes admission webhook list exceeded safe page limit: %w", domain.ErrUpstream)
}

func (c *Client) AdmissionWebhookConfiguration(
	ctx context.Context,
	kind domain.KubernetesAdmissionWebhookConfigurationKind,
	name string,
) (domain.KubernetesAdmissionWebhookConfigurationDetail, error) {
	if err := domain.ValidateAdmissionWebhookConfigurationKind(kind); err != nil {
		return domain.KubernetesAdmissionWebhookConfigurationDetail{}, err
	}
	if err := domain.ValidateAdmissionWebhookConfigurationName(name); err != nil {
		return domain.KubernetesAdmissionWebhookConfigurationDetail{}, err
	}
	spec, err := admissionWebhookSpec(kind)
	if err != nil {
		return domain.KubernetesAdmissionWebhookConfigurationDetail{}, err
	}
	payload, _, err := c.getPayload(
		ctx, spec.collectionPath+"/"+name, nil, "application/json", admissionWebhookMaxDetailBytes, false,
	)
	if err != nil {
		return domain.KubernetesAdmissionWebhookConfigurationDetail{}, err
	}
	return decodeAdmissionWebhookConfiguration(payload, kind, name)
}

func admissionWebhookSpec(
	kind domain.KubernetesAdmissionWebhookConfigurationKind,
) (admissionWebhookResourceSpec, error) {
	switch kind {
	case domain.AdmissionWebhookConfigurationValidating:
		return admissionWebhookResourceSpec{
			objectKind: "ValidatingWebhookConfiguration", collectionPath: validatingAdmissionWebhookCollectionPath,
		}, nil
	case domain.AdmissionWebhookConfigurationMutating:
		return admissionWebhookResourceSpec{
			objectKind: "MutatingWebhookConfiguration", collectionPath: mutatingAdmissionWebhookCollectionPath,
		}, nil
	default:
		return admissionWebhookResourceSpec{}, domain.Invalid("kind", "must be validating or mutating")
	}
}

func decodeAdmissionWebhookConfiguration(
	payload []byte,
	kind domain.KubernetesAdmissionWebhookConfigurationKind,
	expectedName string,
) (domain.KubernetesAdmissionWebhookConfigurationDetail, error) {
	spec, err := admissionWebhookSpec(kind)
	if err != nil {
		return domain.KubernetesAdmissionWebhookConfigurationDetail{}, err
	}
	var source admissionWebhookConfigurationSource
	if err := json.Unmarshal(payload, &source); err != nil {
		return domain.KubernetesAdmissionWebhookConfigurationDetail{}, fmt.Errorf("decode Kubernetes admission webhook detail: %w", domain.ErrUpstream)
	}
	if source.APIVersion != "admissionregistration.k8s.io/v1" || source.Kind != spec.objectKind ||
		source.Metadata.Name != expectedName || source.Metadata.Namespace != "" || source.Metadata.Generation < 1 ||
		source.Metadata.CreationTimestamp.IsZero() || domain.ValidateAdmissionWebhookConfigurationName(source.Metadata.Name) != nil {
		return domain.KubernetesAdmissionWebhookConfigurationDetail{}, fmt.Errorf("invalid Kubernetes admission webhook detail identity: %w", domain.ErrUpstream)
	}
	if len(source.Webhooks) == 0 || len(source.Webhooks) > maxAdmissionWebhooks {
		return domain.KubernetesAdmissionWebhookConfigurationDetail{}, fmt.Errorf("Kubernetes admission webhook detail exceeded safe webhook limit: %w", domain.ErrUpstream)
	}
	if err := validateAdmissionWebhookNestedEntries(source.Webhooks); err != nil {
		return domain.KubernetesAdmissionWebhookConfigurationDetail{}, err
	}

	webhooks := make([]domain.KubernetesAdmissionWebhook, 0, len(source.Webhooks))
	seen := make(map[string]struct{}, len(source.Webhooks))
	for _, webhookSource := range source.Webhooks {
		if _, exists := seen[webhookSource.Name]; exists {
			return domain.KubernetesAdmissionWebhookConfigurationDetail{}, fmt.Errorf("duplicate Kubernetes admission webhook: %w", domain.ErrUpstream)
		}
		webhook, err := decodeAdmissionWebhook(webhookSource, kind)
		if err != nil {
			return domain.KubernetesAdmissionWebhookConfigurationDetail{}, err
		}
		seen[webhook.Name] = struct{}{}
		webhooks = append(webhooks, webhook)
	}
	return domain.KubernetesAdmissionWebhookConfigurationDetail{
		KubernetesAdmissionWebhookConfiguration: domain.KubernetesAdmissionWebhookConfiguration{
			Kind: kind, Name: source.Metadata.Name, CreatedAt: source.Metadata.CreationTimestamp,
		},
		Generation: source.Metadata.Generation, Webhooks: webhooks, WebhookCount: len(source.Webhooks),
	}, nil
}

func validateAdmissionWebhookNestedEntries(webhooks []admissionWebhookSource) error {
	total := len(webhooks)
	for _, webhook := range webhooks {
		if len(webhook.Rules) > maxAdmissionWebhookRules ||
			len(webhook.MatchConditions) > maxAdmissionWebhookMatchConditions ||
			len(webhook.AdmissionReviewVersions) > maxAdmissionWebhookReviewVersions ||
			len(webhook.NamespaceSelector.MatchLabels) > maxAdmissionWebhookSelectorEntries ||
			len(webhook.NamespaceSelector.MatchExpressions) > maxAdmissionWebhookSelectorEntries ||
			len(webhook.ObjectSelector.MatchLabels) > maxAdmissionWebhookSelectorEntries ||
			len(webhook.ObjectSelector.MatchExpressions) > maxAdmissionWebhookSelectorEntries {
			return fmt.Errorf("Kubernetes admission webhook detail exceeded safe nested field limit: %w", domain.ErrUpstream)
		}
		total += len(webhook.Rules) + len(webhook.MatchConditions) + len(webhook.AdmissionReviewVersions) +
			len(webhook.NamespaceSelector.MatchLabels) + len(webhook.NamespaceSelector.MatchExpressions) +
			len(webhook.ObjectSelector.MatchLabels) + len(webhook.ObjectSelector.MatchExpressions)
		for _, rule := range webhook.Rules {
			if len(rule.Operations) > maxAdmissionWebhookRuleValues || len(rule.APIGroups) > maxAdmissionWebhookRuleValues ||
				len(rule.APIVersions) > maxAdmissionWebhookRuleValues || len(rule.Resources) > maxAdmissionWebhookRuleValues {
				return fmt.Errorf("Kubernetes admission webhook rule exceeded safe value limit: %w", domain.ErrUpstream)
			}
			total += len(rule.Operations) + len(rule.APIGroups) + len(rule.APIVersions) + len(rule.Resources)
		}
		if total > maxAdmissionWebhookNestedEntries {
			return fmt.Errorf("Kubernetes admission webhook detail exceeded safe nested entry limit: %w", domain.ErrUpstream)
		}
	}
	return nil
}

func decodeAdmissionWebhook(
	source admissionWebhookSource,
	kind domain.KubernetesAdmissionWebhookConfigurationKind,
) (domain.KubernetesAdmissionWebhook, error) {
	if domain.ValidateAdmissionWebhookConfigurationName(source.Name) != nil {
		return domain.KubernetesAdmissionWebhook{}, fmt.Errorf("invalid Kubernetes admission webhook name: %w", domain.ErrUpstream)
	}
	webhook := domain.KubernetesAdmissionWebhook{
		Name: source.Name, CABundleConfigured: len(source.ClientConfig.CABundle) > 0,
		AdmissionReviewVersions: append([]string(nil), source.AdmissionReviewVersions...),
		RuleCount:               len(source.Rules), MatchConditionCount: len(source.MatchConditions),
		NamespaceSelectorLabelCount:      len(source.NamespaceSelector.MatchLabels),
		NamespaceSelectorExpressionCount: len(source.NamespaceSelector.MatchExpressions),
		ObjectSelectorLabelCount:         len(source.ObjectSelector.MatchLabels),
		ObjectSelectorExpressionCount:    len(source.ObjectSelector.MatchExpressions),
	}
	if err := decodeAdmissionWebhookTarget(&webhook, source); err != nil {
		return domain.KubernetesAdmissionWebhook{}, err
	}
	if err := decodeAdmissionWebhookPolicies(&webhook, source, kind); err != nil {
		return domain.KubernetesAdmissionWebhook{}, err
	}
	if err := validateAdmissionReviewVersions(webhook.AdmissionReviewVersions); err != nil {
		return domain.KubernetesAdmissionWebhook{}, err
	}
	if err := summarizeAdmissionWebhookRules(&webhook, source.Rules); err != nil {
		return domain.KubernetesAdmissionWebhook{}, err
	}
	return webhook, nil
}

func decodeAdmissionWebhookTarget(webhook *domain.KubernetesAdmissionWebhook, source admissionWebhookSource) error {
	hasService := source.ClientConfig.Service != nil
	hasURL := source.ClientConfig.URL != nil
	if hasService == hasURL {
		return fmt.Errorf("invalid Kubernetes admission webhook client target: %w", domain.ErrUpstream)
	}
	if hasURL {
		raw := *source.ClientConfig.URL
		parsed, err := url.ParseRequestURI(raw)
		if err != nil || len(raw) > maxAdmissionWebhookTextBytes || raw != strings.TrimSpace(raw) ||
			parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
			strings.IndexFunc(raw, unicode.IsControl) >= 0 {
			return fmt.Errorf("invalid Kubernetes admission webhook URL target: %w", domain.ErrUpstream)
		}
		webhook.TargetType = "url"
		return nil
	}

	service := source.ClientConfig.Service
	if domain.ValidateNamespace(service.Namespace) != nil || domain.ValidateNamespace(service.Name) != nil ||
		(service.Path != "" && (len(service.Path) > maxAdmissionWebhookTextBytes || !strings.HasPrefix(service.Path, "/") ||
			service.Path != strings.TrimSpace(service.Path) || strings.IndexFunc(service.Path, unicode.IsControl) >= 0)) {
		return fmt.Errorf("invalid Kubernetes admission webhook service target: %w", domain.ErrUpstream)
	}
	webhook.TargetType = "service"
	webhook.ServiceNamespace = service.Namespace
	webhook.ServiceName = service.Name
	webhook.ServicePort = defaultAdmissionWebhookServicePort
	webhook.ServicePortDefaulted = service.Port == nil
	if service.Port != nil {
		if *service.Port < 1 || *service.Port > 65535 {
			return fmt.Errorf("invalid Kubernetes admission webhook service port: %w", domain.ErrUpstream)
		}
		webhook.ServicePort = *service.Port
	}
	return nil
}

func decodeAdmissionWebhookPolicies(
	webhook *domain.KubernetesAdmissionWebhook,
	source admissionWebhookSource,
	kind domain.KubernetesAdmissionWebhookConfigurationKind,
) error {
	webhook.FailurePolicy, webhook.FailurePolicyDefaulted = "Fail", source.FailurePolicy == nil
	if source.FailurePolicy != nil {
		webhook.FailurePolicy = *source.FailurePolicy
	}
	if webhook.FailurePolicy != "Fail" && webhook.FailurePolicy != "Ignore" {
		return fmt.Errorf("invalid Kubernetes admission webhook failure policy: %w", domain.ErrUpstream)
	}

	webhook.MatchPolicy, webhook.MatchPolicyDefaulted = "Equivalent", source.MatchPolicy == nil
	if source.MatchPolicy != nil {
		webhook.MatchPolicy = *source.MatchPolicy
	}
	if webhook.MatchPolicy != "Equivalent" && webhook.MatchPolicy != "Exact" {
		return fmt.Errorf("invalid Kubernetes admission webhook match policy: %w", domain.ErrUpstream)
	}

	if source.SideEffects == nil {
		return fmt.Errorf("missing Kubernetes admission webhook side effects: %w", domain.ErrUpstream)
	}
	webhook.SideEffects = *source.SideEffects
	if webhook.SideEffects != "None" && webhook.SideEffects != "NoneOnDryRun" &&
		webhook.SideEffects != "Some" && webhook.SideEffects != "Unknown" {
		return fmt.Errorf("invalid Kubernetes admission webhook side effects: %w", domain.ErrUpstream)
	}

	webhook.TimeoutSeconds, webhook.TimeoutSecondsDefaulted = defaultAdmissionWebhookTimeout, source.TimeoutSeconds == nil
	if source.TimeoutSeconds != nil {
		webhook.TimeoutSeconds = *source.TimeoutSeconds
	}
	if webhook.TimeoutSeconds < 1 || webhook.TimeoutSeconds > 30 {
		return fmt.Errorf("invalid Kubernetes admission webhook timeout: %w", domain.ErrUpstream)
	}

	if kind == domain.AdmissionWebhookConfigurationValidating {
		if source.ReinvocationPolicy != nil {
			return fmt.Errorf("validating Kubernetes admission webhook contains a reinvocation policy: %w", domain.ErrUpstream)
		}
		return nil
	}
	webhook.ReinvocationPolicy, webhook.ReinvocationPolicyDefaulted = "Never", source.ReinvocationPolicy == nil
	if source.ReinvocationPolicy != nil {
		webhook.ReinvocationPolicy = *source.ReinvocationPolicy
	}
	if webhook.ReinvocationPolicy != "Never" && webhook.ReinvocationPolicy != "IfNeeded" {
		return fmt.Errorf("invalid Kubernetes admission webhook reinvocation policy: %w", domain.ErrUpstream)
	}
	return nil
}

func validateAdmissionReviewVersions(versions []string) error {
	if len(versions) == 0 || len(versions) > maxAdmissionWebhookReviewVersions {
		return fmt.Errorf("invalid Kubernetes admission webhook review versions: %w", domain.ErrUpstream)
	}
	seen := make(map[string]struct{}, len(versions))
	for _, version := range versions {
		if !validAdmissionWebhookVersion(version) {
			return fmt.Errorf("invalid Kubernetes admission webhook review version: %w", domain.ErrUpstream)
		}
		if _, exists := seen[version]; exists {
			return fmt.Errorf("duplicate Kubernetes admission webhook review version: %w", domain.ErrUpstream)
		}
		seen[version] = struct{}{}
	}
	return nil
}

func summarizeAdmissionWebhookRules(webhook *domain.KubernetesAdmissionWebhook, rules []admissionWebhookRuleSource) error {
	for _, rule := range rules {
		if len(rule.Operations) == 0 || len(rule.APIGroups) == 0 || len(rule.APIVersions) == 0 || len(rule.Resources) == 0 {
			return fmt.Errorf("incomplete Kubernetes admission webhook rule: %w", domain.ErrUpstream)
		}
		if rule.Scope != nil && *rule.Scope != "Cluster" && *rule.Scope != "Namespaced" && *rule.Scope != "*" {
			return fmt.Errorf("invalid Kubernetes admission webhook rule scope: %w", domain.ErrUpstream)
		}
		if err := validateAdmissionWebhookOperations(rule.Operations); err != nil {
			return err
		}
		for _, values := range [][]string{rule.APIGroups, rule.APIVersions, rule.Resources} {
			if !validAdmissionWebhookRuleValues(values) {
				return fmt.Errorf("invalid Kubernetes admission webhook rule value: %w", domain.ErrUpstream)
			}
		}
		webhook.OperationCount += len(rule.Operations)
		webhook.APIGroupCount += len(rule.APIGroups)
		webhook.APIVersionCount += len(rule.APIVersions)
		webhook.ResourceCount += len(rule.Resources)
	}
	return nil
}

func validateAdmissionWebhookOperations(operations []string) error {
	seen := make(map[string]struct{}, len(operations))
	for _, operation := range operations {
		switch operation {
		case "CREATE", "UPDATE", "DELETE", "CONNECT", "*":
		default:
			return fmt.Errorf("invalid Kubernetes admission webhook operation: %w", domain.ErrUpstream)
		}
		if _, exists := seen[operation]; exists {
			return fmt.Errorf("duplicate Kubernetes admission webhook operation: %w", domain.ErrUpstream)
		}
		seen[operation] = struct{}{}
	}
	return nil
}

func validAdmissionWebhookRuleValues(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if len(value) > maxAdmissionWebhookTextBytes || value != strings.TrimSpace(value) ||
			strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func validAdmissionWebhookVersion(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func validAdmissionWebhookContinue(value string) bool {
	return value != "" && len(value) <= admissionWebhookMaxContinueBytes && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}
