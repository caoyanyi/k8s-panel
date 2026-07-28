package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

func TestClientReadsBoundedNetworkPolicySummaries(t *testing.T) {
	t.Parallel()

	var requested string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = r.URL.RequestURI()
		writeTestJSON(t, w, map[string]any{
			"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicyList", "metadata": map[string]any{},
			"items": []any{
				networkPolicyObject("payments", "selected", map[string]any{
					"podSelector": map[string]any{
						"matchLabels": map[string]string{"app": "private-value"},
						"matchExpressions": []any{map[string]any{
							"key": "tier", "operator": "In", "values": []string{"private-tier"},
						}},
					},
					"policyTypes": []string{"Egress", "Ingress"},
					"ingress": []any{
						map[string]any{"from": []any{map[string]any{"ipBlock": map[string]any{"cidr": "10.0.0.0/8"}}, map[string]any{"podSelector": map[string]any{}}}, "ports": []any{map[string]any{"protocol": "TCP", "port": 443}}},
						map[string]any{},
					},
					"egress": []any{map[string]any{
						"to":    []any{map[string]any{"namespaceSelector": map[string]any{}}},
						"ports": []any{map[string]any{"protocol": "UDP", "port": 53}, map[string]any{"protocol": "TCP", "port": 53}},
					}},
				}),
				networkPolicyObject("default", "all-pods", map[string]any{}),
				networkPolicyObject("payments", "default-both", map[string]any{
					"podSelector": map[string]any{},
					"egress":      []any{map[string]any{}},
				}),
			},
		})
	}))
	t.Cleanup(server.Close)

	client := newNetworkTestClient(t, server)
	policies, err := client.NetworkPolicies(context.Background(), "")
	if err != nil {
		t.Fatalf("NetworkPolicies() error = %v", err)
	}
	if requested != "/apis/networking.k8s.io/v1/networkpolicies?limit=250" {
		t.Fatalf("request URI = %q", requested)
	}
	if len(policies) != 3 || policies[0].Namespace != "default" || policies[0].Name != "all-pods" ||
		policies[1].Name != "default-both" || policies[2].Name != "selected" {
		t.Fatalf("NetworkPolicies() = %#v", policies)
	}
	allPods := policies[0]
	if allPods.PodSelectorMode != domain.KubernetesSelectorAll || allPods.PodSelectorLabelCount != 0 ||
		allPods.PodSelectorExpressionCount != 0 || !allPods.PolicyTypesDefaulted ||
		len(allPods.PolicyTypes) != 1 || allPods.PolicyTypes[0] != "Ingress" {
		t.Errorf("all-pods summary = %#v", allPods)
	}
	defaultBoth := policies[1]
	if !defaultBoth.PolicyTypesDefaulted || len(defaultBoth.PolicyTypes) != 2 ||
		defaultBoth.PolicyTypes[0] != "Ingress" || defaultBoth.PolicyTypes[1] != "Egress" ||
		defaultBoth.EgressRuleCount != 1 {
		t.Errorf("default-both summary = %#v", defaultBoth)
	}
	selected := policies[2]
	if selected.PodSelectorMode != domain.KubernetesSelectorFiltered || selected.PodSelectorLabelCount != 1 ||
		selected.PodSelectorExpressionCount != 1 || selected.PolicyTypesDefaulted ||
		len(selected.PolicyTypes) != 2 || selected.PolicyTypes[0] != "Ingress" || selected.PolicyTypes[1] != "Egress" ||
		selected.IngressRuleCount != 2 || selected.IngressPeerCount != 2 || selected.IngressPortCount != 1 ||
		selected.EgressRuleCount != 1 || selected.EgressPeerCount != 1 || selected.EgressPortCount != 2 {
		t.Errorf("selected summary = %#v", selected)
	}
	encoded, err := json.Marshal(selected)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	for _, secret := range []string{"private-value", "private-tier", "10.0.0.0/8", "443"} {
		if strings.Contains(string(encoded), secret) {
			t.Errorf("summary leaked %q: %s", secret, encoded)
		}
	}
}

func TestClientUsesNamespacedNetworkPolicyPathAndRejectsEscapedScope(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.RequestURI() != "/apis/networking.k8s.io/v1/namespaces/payments/networkpolicies?limit=250" {
			t.Errorf("request URI = %q", r.URL.RequestURI())
		}
		writeTestJSON(t, w, map[string]any{
			"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicyList", "metadata": map[string]any{},
			"items": []any{networkPolicyObject("other", "escaped", map[string]any{})},
		})
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	if _, err := client.NetworkPolicies(context.Background(), "bad/namespace"); err == nil {
		t.Fatal("NetworkPolicies() accepted an invalid namespace")
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid namespace made %d requests", calls.Load())
	}
	if _, err := client.NetworkPolicies(context.Background(), "payments"); !errors.Is(err, domain.ErrUpstream) ||
		!strings.Contains(err.Error(), "namespace scope") {
		t.Fatalf("NetworkPolicies() escaped scope error = %v", err)
	}
}

func TestClientRejectsUntrustedNetworkPolicyObjects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		object map[string]any
	}{
		{name: "api version", object: networkPolicyObject("payments", "policy", map[string]any{"policyTypes": []string{"Ingress"}})},
		{name: "kind", object: networkPolicyObject("payments", "policy", map[string]any{"policyTypes": []string{"Ingress"}})},
		{name: "unknown policy type", object: networkPolicyObject("payments", "policy", map[string]any{"policyTypes": []string{"Unknown"}})},
		{name: "duplicate policy type", object: networkPolicyObject("payments", "policy", map[string]any{"policyTypes": []string{"Ingress", "Ingress"}})},
	}
	tests[0].object["apiVersion"] = "networking.k8s.io/v1beta1"
	tests[1].object["kind"] = "Ingress"

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(test.object)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			remaining := maxNetworkPolicyNestedEntries
			if _, err := decodeNetworkPolicy(raw, "payments", &remaining); !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("decodeNetworkPolicy() error = %v", err)
			}
		})
	}
}

func TestClientBoundsNetworkPolicyNestedEntries(t *testing.T) {
	t.Parallel()

	repeated := func(count int) []any {
		items := make([]any, count)
		for index := range items {
			items[index] = map[string]any{}
		}
		return items
	}
	tests := []struct {
		name string
		spec map[string]any
	}{
		{name: "selector", spec: map[string]any{"podSelector": map[string]any{"matchExpressions": repeated(maxNetworkPolicySelectorEntries + 1)}}},
		{name: "ingress rules", spec: map[string]any{"ingress": repeated(maxNetworkPolicyRulesPerDirection + 1)}},
		{name: "egress rules", spec: map[string]any{"egress": repeated(maxNetworkPolicyRulesPerDirection + 1)}},
		{name: "ingress peers", spec: map[string]any{"ingress": []any{map[string]any{"from": repeated(maxNetworkPolicyRulePeers + 1)}}}},
		{name: "egress peers", spec: map[string]any{"egress": []any{map[string]any{"to": repeated(maxNetworkPolicyRulePeers + 1)}}}},
		{name: "ports", spec: map[string]any{"ingress": []any{map[string]any{"ports": repeated(maxNetworkPolicyRulePorts + 1)}}}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(networkPolicyObject("payments", "bounded", test.spec))
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			remaining := maxNetworkPolicyNestedEntries
			if _, err := decodeNetworkPolicy(raw, "payments", &remaining); !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("decodeNetworkPolicy() error = %v", err)
			}
		})
	}
}

func TestClientStopsNetworkPolicyProjectionAtRequestBudget(t *testing.T) {
	t.Parallel()

	expressions := make([]any, maxNetworkPolicySelectorEntries)
	for index := range expressions {
		expressions[index] = map[string]any{}
	}
	items := make([]any, maxNetworkPolicyNestedEntries/maxNetworkPolicySelectorEntries+1)
	for index := range items {
		items[index] = networkPolicyObject("payments", fmt.Sprintf("policy-%03d", index), map[string]any{
			"podSelector": map[string]any{"matchExpressions": expressions},
		})
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, map[string]any{
			"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicyList", "metadata": map[string]any{}, "items": items,
		})
	}))
	t.Cleanup(server.Close)

	client := newNetworkTestClient(t, server)
	if _, err := client.NetworkPolicies(context.Background(), "payments"); !errors.Is(err, domain.ErrUpstream) ||
		!strings.Contains(err.Error(), "nested entry limit") {
		t.Fatalf("NetworkPolicies() error = %v", err)
	}
}

func networkPolicyObject(namespace, name string, spec map[string]any) map[string]any {
	return map[string]any{
		"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy",
		"metadata": map[string]any{
			"namespace": namespace, "name": name, "creationTimestamp": "2026-07-28T04:00:00Z",
		},
		"spec": spec,
	}
}
