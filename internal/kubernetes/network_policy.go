package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

const (
	maxNetworkPolicySelectorEntries   = 128
	maxNetworkPolicyRulesPerDirection = 128
	maxNetworkPolicyRulePeers         = 128
	maxNetworkPolicyRulePorts         = 128
	maxNetworkPolicyNestedEntries     = 16 * 1024
)

type networkPolicyIngressRuleSource struct {
	From  []json.RawMessage `json:"from"`
	Ports []json.RawMessage `json:"ports"`
}

type networkPolicyEgressRuleSource struct {
	To    []json.RawMessage `json:"to"`
	Ports []json.RawMessage `json:"ports"`
}

func (c *Client) NetworkPolicies(ctx context.Context, namespace string) ([]domain.KubernetesNetworkPolicy, error) {
	path := "/apis/networking.k8s.io/v1/networkpolicies"
	if namespace != "" {
		if err := domain.ValidateNamespace(namespace); err != nil {
			return nil, err
		}
		path = "/apis/networking.k8s.io/v1/namespaces/" + namespace + "/networkpolicies"
	}
	items, err := c.listGovernanceRaw(ctx, path, "networking.k8s.io/v1", "NetworkPolicyList")
	if err != nil {
		return nil, err
	}
	remaining := maxNetworkPolicyNestedEntries
	policies := make([]domain.KubernetesNetworkPolicy, 0, len(items))
	for _, item := range items {
		policy, err := decodeNetworkPolicy(item, namespace, &remaining)
		if err != nil {
			return nil, err
		}
		policies = append(policies, policy)
	}
	sort.Slice(policies, func(i, j int) bool {
		if policies[i].Namespace != policies[j].Namespace {
			return policies[i].Namespace < policies[j].Namespace
		}
		return policies[i].Name < policies[j].Name
	})
	return policies, nil
}

func decodeNetworkPolicy(
	raw json.RawMessage,
	expectedNamespace string,
	remainingEntries *int,
) (domain.KubernetesNetworkPolicy, error) {
	var source struct {
		APIVersion string             `json:"apiVersion"`
		Kind       string             `json:"kind"`
		Metadata   governanceMetadata `json:"metadata"`
		Spec       struct {
			PodSelector *availabilitySelectorSource      `json:"podSelector"`
			PolicyTypes []string                         `json:"policyTypes"`
			Ingress     []networkPolicyIngressRuleSource `json:"ingress"`
			Egress      []networkPolicyEgressRuleSource  `json:"egress"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		return domain.KubernetesNetworkPolicy{}, fmt.Errorf("decode Kubernetes NetworkPolicy: %w", domain.ErrUpstream)
	}
	if err := validateNetworkPolicyIdentity(source.APIVersion, source.Kind, source.Metadata, expectedNamespace); err != nil {
		return domain.KubernetesNetworkPolicy{}, err
	}

	selectorLabelCount, selectorExpressionCount := 0, 0
	if source.Spec.PodSelector != nil {
		selectorLabelCount = len(source.Spec.PodSelector.MatchLabels)
		selectorExpressionCount = len(source.Spec.PodSelector.MatchExpressions)
	}
	if selectorLabelCount+selectorExpressionCount > maxNetworkPolicySelectorEntries {
		return domain.KubernetesNetworkPolicy{}, fmt.Errorf("Kubernetes NetworkPolicy selector exceeded safe entry limit: %w", domain.ErrUpstream)
	}
	if len(source.Spec.Ingress) > maxNetworkPolicyRulesPerDirection || len(source.Spec.Egress) > maxNetworkPolicyRulesPerDirection {
		return domain.KubernetesNetworkPolicy{}, fmt.Errorf("Kubernetes NetworkPolicy rules exceeded safe entry limit: %w", domain.ErrUpstream)
	}

	nestedEntries := selectorLabelCount + selectorExpressionCount + len(source.Spec.Ingress) + len(source.Spec.Egress)
	ingressPeers, ingressPorts, err := countNetworkPolicyIngressEntries(source.Spec.Ingress, &nestedEntries)
	if err != nil {
		return domain.KubernetesNetworkPolicy{}, err
	}
	egressPeers, egressPorts, err := countNetworkPolicyEgressEntries(source.Spec.Egress, &nestedEntries)
	if err != nil {
		return domain.KubernetesNetworkPolicy{}, err
	}
	if remainingEntries == nil || nestedEntries > *remainingEntries {
		return domain.KubernetesNetworkPolicy{}, fmt.Errorf("Kubernetes NetworkPolicy request exceeded safe nested entry limit: %w", domain.ErrUpstream)
	}
	*remainingEntries -= nestedEntries

	policyTypes, defaulted, err := networkPolicyTypes(source.Spec.PolicyTypes, len(source.Spec.Egress) > 0)
	if err != nil {
		return domain.KubernetesNetworkPolicy{}, err
	}
	selectorMode := domain.KubernetesSelectorAll
	if selectorLabelCount > 0 || selectorExpressionCount > 0 {
		selectorMode = domain.KubernetesSelectorFiltered
	}
	return domain.KubernetesNetworkPolicy{
		Namespace: source.Metadata.Namespace, Name: source.Metadata.Name,
		PodSelectorMode: selectorMode, PodSelectorLabelCount: selectorLabelCount,
		PodSelectorExpressionCount: selectorExpressionCount,
		PolicyTypes:                policyTypes, PolicyTypesDefaulted: defaulted,
		IngressRuleCount: len(source.Spec.Ingress), IngressPeerCount: ingressPeers, IngressPortCount: ingressPorts,
		EgressRuleCount: len(source.Spec.Egress), EgressPeerCount: egressPeers, EgressPortCount: egressPorts,
		CreatedAt: source.Metadata.CreationTimestamp,
	}, nil
}

func validateNetworkPolicyIdentity(apiVersion, kind string, metadata governanceMetadata, expectedNamespace string) error {
	if apiVersion != "networking.k8s.io/v1" || kind != "NetworkPolicy" ||
		!validKubernetesMetadataString(metadata.Name) || metadata.CreationTimestamp.IsZero() ||
		domain.ValidateNamespace(metadata.Namespace) != nil {
		return fmt.Errorf("invalid Kubernetes NetworkPolicy object identity: %w", domain.ErrUpstream)
	}
	if expectedNamespace != "" && metadata.Namespace != expectedNamespace {
		return fmt.Errorf("Kubernetes NetworkPolicy object exceeded namespace scope: %w", domain.ErrUpstream)
	}
	return nil
}

func countNetworkPolicyIngressEntries(rules []networkPolicyIngressRuleSource, total *int) (int, int, error) {
	peers, ports := 0, 0
	for _, rule := range rules {
		if len(rule.From) > maxNetworkPolicyRulePeers || len(rule.Ports) > maxNetworkPolicyRulePorts {
			return 0, 0, fmt.Errorf("Kubernetes NetworkPolicy ingress rule exceeded safe entry limit: %w", domain.ErrUpstream)
		}
		peers += len(rule.From)
		ports += len(rule.Ports)
		*total += len(rule.From) + len(rule.Ports)
	}
	return peers, ports, nil
}

func countNetworkPolicyEgressEntries(rules []networkPolicyEgressRuleSource, total *int) (int, int, error) {
	peers, ports := 0, 0
	for _, rule := range rules {
		if len(rule.To) > maxNetworkPolicyRulePeers || len(rule.Ports) > maxNetworkPolicyRulePorts {
			return 0, 0, fmt.Errorf("Kubernetes NetworkPolicy egress rule exceeded safe entry limit: %w", domain.ErrUpstream)
		}
		peers += len(rule.To)
		ports += len(rule.Ports)
		*total += len(rule.To) + len(rule.Ports)
	}
	return peers, ports, nil
}

func networkPolicyTypes(source []string, hasEgressRules bool) ([]string, bool, error) {
	if len(source) == 0 {
		types := []string{"Ingress"}
		if hasEgressRules {
			types = append(types, "Egress")
		}
		return types, true, nil
	}
	if len(source) > 2 {
		return nil, false, fmt.Errorf("invalid Kubernetes NetworkPolicy policy types: %w", domain.ErrUpstream)
	}
	ingress, egress := false, false
	for _, policyType := range source {
		switch policyType {
		case "Ingress":
			if ingress {
				return nil, false, fmt.Errorf("duplicate Kubernetes NetworkPolicy policy type: %w", domain.ErrUpstream)
			}
			ingress = true
		case "Egress":
			if egress {
				return nil, false, fmt.Errorf("duplicate Kubernetes NetworkPolicy policy type: %w", domain.ErrUpstream)
			}
			egress = true
		default:
			return nil, false, fmt.Errorf("unsupported Kubernetes NetworkPolicy policy type: %w", domain.ErrUpstream)
		}
	}
	types := make([]string, 0, len(source))
	if ingress {
		types = append(types, "Ingress")
	}
	if egress {
		types = append(types, "Egress")
	}
	return types, false, nil
}
