package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

func TestClientListsAdmissionPoliciesAndBindingsWithMetadataOnlyPagination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		collectionPath string
		load           func(*Client) ([]domain.KubernetesAdmissionPolicyResource, error)
		wantKind       domain.KubernetesAdmissionPolicyResourceKind
	}{
		{
			name:           "policies",
			collectionPath: validatingAdmissionPolicyCollectionPath,
			load: func(client *Client) ([]domain.KubernetesAdmissionPolicyResource, error) {
				return client.ValidatingAdmissionPolicies(context.Background())
			},
			wantKind: domain.AdmissionPolicyResourcePolicy,
		},
		{
			name:           "bindings",
			collectionPath: validatingAdmissionPolicyBindingCollectionPath,
			load: func(client *Client) ([]domain.KubernetesAdmissionPolicyResource, error) {
				return client.ValidatingAdmissionPolicyBindings(context.Background())
			},
			wantKind: domain.AdmissionPolicyResourceBinding,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			requests := make([]string, 0, 2)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Accept"); got != kubernetesPartialMetadataListAccept {
					t.Errorf("Accept = %q, want %q", got, kubernetesPartialMetadataListAccept)
				}
				if got := r.URL.Query().Get("limit"); got != admissionPolicyListPageSize {
					t.Errorf("limit = %q, want %q", got, admissionPolicyListPageSize)
				}
				mu.Lock()
				requests = append(requests, r.URL.RequestURI())
				mu.Unlock()
				if r.URL.Path != tt.collectionPath {
					http.NotFound(w, r)
					return
				}
				if r.URL.Query().Get("continue") == "page-two" {
					writeTestJSON(t, w, accessMetadataList("", []any{
						accessMetadata("alpha.policy.example.com", "", "2026-07-29T08:00:00Z"),
					}))
					return
				}
				writeTestJSON(t, w, accessMetadataList("page-two", []any{
					accessMetadata("zeta.policy.example.com", "", "2026-07-29T09:00:00Z"),
				}))
			}))
			t.Cleanup(server.Close)

			items, err := tt.load(newNetworkTestClient(t, server))
			if err != nil {
				t.Fatalf("load metadata: %v", err)
			}
			if len(items) != 2 || items[0].Name != "alpha.policy.example.com" || items[0].Kind != tt.wantKind ||
				items[0].CreatedAt.IsZero() || items[1].Name != "zeta.policy.example.com" {
				t.Fatalf("items = %#v", items)
			}

			mu.Lock()
			gotRequests := append([]string(nil), requests...)
			mu.Unlock()
			wantRequests := []string{
				tt.collectionPath + "?limit=250",
				tt.collectionPath + "?continue=page-two&limit=250",
			}
			if len(gotRequests) != len(wantRequests) {
				t.Fatalf("request URIs = %#v, want %#v", gotRequests, wantRequests)
			}
			for index := range wantRequests {
				if gotRequests[index] != wantRequests[index] {
					t.Fatalf("request URIs = %#v, want %#v", gotRequests, wantRequests)
				}
			}
		})
	}
}

func TestClientReadsRedactedValidatingAdmissionPolicyDetail(t *testing.T) {
	t.Parallel()

	const name = "replica-policy.example.com"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != validatingAdmissionPolicyCollectionPath+"/"+name {
			http.NotFound(w, r)
			return
		}
		writeTestJSON(t, w, admissionPolicyResponse(name))
	}))
	t.Cleanup(server.Close)

	detail, err := newNetworkTestClient(t, server).ValidatingAdmissionPolicy(context.Background(), name)
	if err != nil {
		t.Fatalf("ValidatingAdmissionPolicy() error = %v", err)
	}
	if detail.Kind != domain.AdmissionPolicyResourcePolicy || detail.Name != name || detail.Generation != 4 ||
		detail.FailurePolicy != "Ignore" || detail.FailurePolicyDefaulted || !detail.ParamKindConfigured ||
		detail.ParamAPIVersion != "rules.example.com/v1" || detail.ParamKind != "ReplicaLimit" ||
		detail.ValidationCount != 2 || detail.AuditAnnotationCount != 1 || detail.MatchConditionCount != 1 ||
		detail.VariableCount != 1 || detail.ObservedGeneration != 4 || !detail.TypeCheckingObserved ||
		detail.ExpressionWarningCount != 1 || detail.ConditionCount != 1 {
		t.Fatalf("policy detail = %#v", detail)
	}
	match := detail.Match
	if !match.Configured || match.MatchPolicy != "Exact" || match.MatchPolicyDefaulted ||
		match.ResourceRuleCount != 1 || match.ExcludeResourceRuleCount != 1 || match.OperationCount != 3 ||
		match.APIGroupCount != 2 || match.APIVersionCount != 2 || match.ResourceCount != 3 ||
		match.NamespaceSelectorLabelCount != 1 || match.NamespaceSelectorExpressionCount != 1 ||
		match.ObjectSelectorLabelCount != 1 || match.ObjectSelectorExpressionCount != 1 {
		t.Fatalf("policy match summary = %#v", match)
	}

	assertAdmissionPolicyDetailRedacted(t, detail)
}

func TestClientReadsRedactedValidatingAdmissionPolicyBindingDetail(t *testing.T) {
	t.Parallel()

	const name = "replica-binding.example.com"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != validatingAdmissionPolicyBindingCollectionPath+"/"+name {
			http.NotFound(w, r)
			return
		}
		writeTestJSON(t, w, admissionPolicyBindingResponse(name, map[string]any{
			"name":                    "private-param-name",
			"namespace":               "policy-system",
			"parameterNotFoundAction": "Deny",
		}, []string{"Audit", "Deny"}))
	}))
	t.Cleanup(server.Close)

	detail, err := newNetworkTestClient(t, server).ValidatingAdmissionPolicyBinding(context.Background(), name)
	if err != nil {
		t.Fatalf("ValidatingAdmissionPolicyBinding() error = %v", err)
	}
	if detail.Kind != domain.AdmissionPolicyResourceBinding || detail.Name != name || detail.Generation != 3 ||
		detail.PolicyName != "replica-policy.example.com" || !detail.ParamRefConfigured || detail.ParamRefMode != "name" ||
		detail.ParamNamespace != "policy-system" || detail.ParameterNotFoundAction != "Deny" ||
		len(detail.ValidationActions) != 2 || detail.ValidationActions[0] != "Deny" || detail.ValidationActions[1] != "Audit" {
		t.Fatalf("binding detail = %#v", detail)
	}
	if !detail.Match.Configured || detail.Match.MatchPolicy != "Equivalent" || !detail.Match.MatchPolicyDefaulted ||
		detail.Match.ResourceRuleCount != 1 || detail.Match.ResourceCount != 1 {
		t.Fatalf("binding match summary = %#v", detail.Match)
	}

	assertAdmissionPolicyDetailRedacted(t, detail)
}

func TestClientAppliesAdmissionPolicyDefaultsAndSummarizesSelectorParameters(t *testing.T) {
	t.Parallel()

	policy := admissionPolicyResponse("default-policy.example.com")
	policySpec := policy["spec"].(map[string]any)
	delete(policySpec, "failurePolicy")
	delete(policySpec, "paramKind")
	policySpec["matchConstraints"].(map[string]any)["matchPolicy"] = nil
	policy["status"] = map[string]any{"observedGeneration": 0}

	policyDetail, err := decodeValidatingAdmissionPolicy(mustMarshalAdmissionPolicyTest(t, policy), "default-policy.example.com")
	if err != nil {
		t.Fatalf("decodeValidatingAdmissionPolicy() error = %v", err)
	}
	if policyDetail.FailurePolicy != "Fail" || !policyDetail.FailurePolicyDefaulted ||
		policyDetail.ParamKindConfigured || policyDetail.Match.MatchPolicy != "Equivalent" ||
		!policyDetail.Match.MatchPolicyDefaulted || policyDetail.TypeCheckingObserved || policyDetail.ObservedGeneration != 0 {
		t.Fatalf("defaulted policy detail = %#v", policyDetail)
	}

	binding := admissionPolicyBindingResponse("selector-binding.example.com", map[string]any{
		"selector": map[string]any{
			"matchLabels": map[string]any{"private-selector-label": "private-selector-value"},
			"matchExpressions": []any{
				map[string]any{"key": "private-selector-expression", "operator": "Exists"},
			},
		},
		"parameterNotFoundAction": "Allow",
	}, []string{"Audit", "Warn"})
	delete(binding["spec"].(map[string]any), "matchResources")

	bindingDetail, err := decodeValidatingAdmissionPolicyBinding(
		mustMarshalAdmissionPolicyTest(t, binding), "selector-binding.example.com",
	)
	if err != nil {
		t.Fatalf("decodeValidatingAdmissionPolicyBinding() error = %v", err)
	}
	if !bindingDetail.ParamRefConfigured || bindingDetail.ParamRefMode != "selector" ||
		bindingDetail.ParamSelectorLabelCount != 1 || bindingDetail.ParamSelectorExpressionCount != 1 ||
		bindingDetail.ParameterNotFoundAction != "Allow" || bindingDetail.Match.Configured ||
		len(bindingDetail.ValidationActions) != 2 || bindingDetail.ValidationActions[0] != "Warn" ||
		bindingDetail.ValidationActions[1] != "Audit" {
		t.Fatalf("selector binding detail = %#v", bindingDetail)
	}
	assertAdmissionPolicyDetailRedacted(t, bindingDetail)
}

func TestClientBoundsAdmissionPolicyReads(t *testing.T) {
	t.Parallel()

	t.Run("list page limit", func(t *testing.T) {
		var requests atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			writeTestJSON(t, w, accessMetadataList("more", []any{}))
		}))
		t.Cleanup(server.Close)

		if _, err := newNetworkTestClient(t, server).ValidatingAdmissionPolicies(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("ValidatingAdmissionPolicies() error = %v, want upstream error", err)
		}
		if got := requests.Load(); got != admissionPolicyMaxListPages {
			t.Fatalf("requests = %d, want %d", got, admissionPolicyMaxListPages)
		}
	})

	t.Run("detail response bytes", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", int(admissionPolicyMaxDetailBytes)+1)))
		}))
		t.Cleanup(server.Close)

		if _, err := newNetworkTestClient(t, server).ValidatingAdmissionPolicy(context.Background(), "policy.example.com"); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("ValidatingAdmissionPolicy() error = %v, want upstream error", err)
		}
	})

	t.Run("unsafe continuation", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, accessMetadataList(strings.Repeat("x", admissionPolicyMaxContinueBytes+1), []any{}))
		}))
		t.Cleanup(server.Close)

		if _, err := newNetworkTestClient(t, server).ValidatingAdmissionPolicyBindings(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("ValidatingAdmissionPolicyBindings() error = %v, want upstream error", err)
		}
	})

	t.Run("duplicate metadata", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, accessMetadataList("", []any{
				accessMetadata("duplicate.example.com", "", "2026-07-29T09:00:00Z"),
				accessMetadata("duplicate.example.com", "", "2026-07-29T09:00:00Z"),
			}))
		}))
		t.Cleanup(server.Close)

		if _, err := newNetworkTestClient(t, server).ValidatingAdmissionPolicies(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("ValidatingAdmissionPolicies() error = %v, want upstream error", err)
		}
	})
}

func TestClientRejectsInvalidAdmissionPolicyDetails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		binding  bool
		response map[string]any
	}{
		{
			name: "policy identity mismatch",
			response: func() map[string]any {
				item := admissionPolicyResponse("other.example.com")
				return item
			}(),
		},
		{
			name:     "conflicting binding actions",
			binding:  true,
			response: admissionPolicyBindingResponse("binding.example.com", nil, []string{"Deny", "Warn"}),
		},
		{
			name:    "ambiguous binding parameter",
			binding: true,
			response: admissionPolicyBindingResponse("binding.example.com", map[string]any{
				"name": "private-param", "selector": map[string]any{}, "parameterNotFoundAction": "Allow",
			}, []string{"Audit"}),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeTestJSON(t, w, tt.response)
			}))
			t.Cleanup(server.Close)
			client := newNetworkTestClient(t, server)
			var err error
			if tt.binding {
				_, err = client.ValidatingAdmissionPolicyBinding(context.Background(), "binding.example.com")
			} else {
				_, err = client.ValidatingAdmissionPolicy(context.Background(), "policy.example.com")
			}
			if !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("detail error = %v, want upstream error", err)
			}
		})
	}
}

func TestAdmissionPolicyRejectsInvalidRequestNamesAndFieldValues(t *testing.T) {
	t.Parallel()

	client := &Client{}
	if _, err := client.ValidatingAdmissionPolicy(context.Background(), "../policies"); err == nil {
		t.Fatal("ValidatingAdmissionPolicy() accepted an invalid request name")
	}
	if _, err := client.ValidatingAdmissionPolicyBinding(context.Background(), "../bindings"); err == nil {
		t.Fatal("ValidatingAdmissionPolicyBinding() accepted an invalid request name")
	}

	for _, value := range []string{"v1", "apps/v1", "rules.example.com/v1alpha1"} {
		if !validAdmissionPolicyAPIVersion(value) {
			t.Errorf("validAdmissionPolicyAPIVersion(%q) = false", value)
		}
	}
	for _, value := range []string{"", "apps/", "apps/v1/extra", " apps/v1", "UPPER_GROUP/v1"} {
		if validAdmissionPolicyAPIVersion(value) {
			t.Errorf("validAdmissionPolicyAPIVersion(%q) = true", value)
		}
	}
	for _, value := range []string{"ConfigMap", "ReplicaLimit", "Kind2"} {
		if !validAdmissionPolicyKind(value) {
			t.Errorf("validAdmissionPolicyKind(%q) = false", value)
		}
	}
	for _, value := range []string{"", "2Kind", "Invalid-Kind", strings.Repeat("K", 64)} {
		if validAdmissionPolicyKind(value) {
			t.Errorf("validAdmissionPolicyKind(%q) = true", value)
		}
	}

	for _, actions := range [][]string{{}, {"Patch"}, {"Audit", "Audit"}} {
		if _, err := canonicalAdmissionPolicyActions(actions); !errors.Is(err, domain.ErrUpstream) {
			t.Errorf("canonicalAdmissionPolicyActions(%v) error = %v, want upstream error", actions, err)
		}
	}
}

func admissionPolicyResponse(name string) map[string]any {
	return map[string]any{
		"apiVersion": "admissionregistration.k8s.io/v1",
		"kind":       "ValidatingAdmissionPolicy",
		"metadata": map[string]any{
			"name": name, "generation": 4, "creationTimestamp": "2026-07-29T08:00:00Z",
			"labels": map[string]any{"private-label": "private-label-value"},
		},
		"spec": map[string]any{
			"failurePolicy": "Ignore",
			"paramKind":     map[string]any{"apiVersion": "rules.example.com/v1", "kind": "ReplicaLimit"},
			"matchConstraints": admissionPolicyMatchResources("Exact", []any{
				admissionPolicyRule([]string{"CREATE", "UPDATE"}, []string{"apps"}, []string{"v1"}, []string{"deployments", "statefulsets"}),
			}, []any{
				admissionPolicyRule([]string{"DELETE"}, []string{"batch"}, []string{"v1"}, []string{"jobs"}),
			}),
			"validations": []any{
				map[string]any{"expression": "private-cel-expression-1", "message": "private-message-1"},
				map[string]any{"expression": "private-cel-expression-2", "messageExpression": "private-message-expression"},
			},
			"auditAnnotations": []any{map[string]any{"key": "private-key", "valueExpression": "private-audit-expression"}},
			"matchConditions":  []any{map[string]any{"name": "private-condition-name", "expression": "private-match-expression"}},
			"variables":        []any{map[string]any{"name": "private-variable-name", "expression": "private-variable-expression"}},
		},
		"status": map[string]any{
			"observedGeneration": 4,
			"typeChecking": map[string]any{"expressionWarnings": []any{
				map[string]any{"fieldRef": "private-field-ref", "warning": "private-warning"},
			}},
			"conditions": []any{map[string]any{
				"type": "Ready", "status": "True", "reason": "private-reason", "message": "private-status-message",
				"lastTransitionTime": "2026-07-29T08:01:00Z",
			}},
		},
	}
}

func admissionPolicyBindingResponse(name string, paramRef map[string]any, actions []string) map[string]any {
	return map[string]any{
		"apiVersion": "admissionregistration.k8s.io/v1",
		"kind":       "ValidatingAdmissionPolicyBinding",
		"metadata": map[string]any{
			"name": name, "generation": 3, "creationTimestamp": "2026-07-29T09:00:00Z",
		},
		"spec": map[string]any{
			"policyName":        "replica-policy.example.com",
			"validationActions": actions,
			"paramRef":          paramRef,
			"matchResources": admissionPolicyMatchResources("", []any{
				admissionPolicyRule([]string{"CREATE"}, []string{"apps"}, []string{"v1"}, []string{"deployments"}),
			}, nil),
		},
	}
}

func admissionPolicyMatchResources(matchPolicy string, rules, excludeRules []any) map[string]any {
	result := map[string]any{
		"resourceRules":        rules,
		"excludeResourceRules": excludeRules,
		"namespaceSelector": map[string]any{
			"matchLabels":      map[string]any{"private-namespace-label": "private-value"},
			"matchExpressions": []any{map[string]any{"key": "private-namespace-expression", "operator": "Exists"}},
		},
		"objectSelector": map[string]any{
			"matchLabels":      map[string]any{"private-object-label": "private-value"},
			"matchExpressions": []any{map[string]any{"key": "private-object-expression", "operator": "Exists"}},
		},
	}
	if matchPolicy != "" {
		result["matchPolicy"] = matchPolicy
	}
	return result
}

func admissionPolicyRule(operations, groups, versions, resources []string) map[string]any {
	return map[string]any{
		"operations": operations, "apiGroups": groups, "apiVersions": versions, "resources": resources,
		"resourceNames": []string{"private-resource-name"}, "scope": "Namespaced",
	}
}

func assertAdmissionPolicyDetailRedacted(t *testing.T, detail any) {
	t.Helper()
	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal detail: %v", err)
	}
	for _, forbidden := range []string{
		"private-cel", "private-message", "private-label", "private-condition-name", "private-audit-expression",
		"private-variable", "private-warning", "private-field-ref", "private-reason", "private-resource-name", "private-param-name",
		"private-selector",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("detail leaked %q: %s", forbidden, encoded)
		}
	}
}

func mustMarshalAdmissionPolicyTest(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal admission policy test response: %v", err)
	}
	return encoded
}
