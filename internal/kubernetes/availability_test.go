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

func TestClientListsNamespaceAvailabilityPolicies(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != governanceListPageSize {
			t.Errorf("limit = %q", r.URL.Query().Get("limit"))
		}
		switch r.URL.Path {
		case "/apis/autoscaling/v2/namespaces/payments/horizontalpodautoscalers":
			writeTestJSON(t, w, map[string]any{
				"apiVersion": "autoscaling/v2", "kind": "HorizontalPodAutoscalerList", "metadata": map[string]any{},
				"items": []any{map[string]any{
					"apiVersion": "autoscaling/v2", "kind": "HorizontalPodAutoscaler",
					"metadata": map[string]any{
						"name": "gateway-autoscaler", "namespace": "payments", "generation": 4,
						"creationTimestamp": "2026-07-28T03:00:00Z",
					},
					"spec": map[string]any{
						"scaleTargetRef": map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "name": "gateway"},
						"maxReplicas":    10,
						"metrics":        []any{map[string]any{"type": "Resource"}, map[string]any{"type": "External"}},
					},
					"status": map[string]any{
						"observedGeneration": 4, "currentReplicas": 3, "desiredReplicas": 5,
						"currentMetrics": []any{map[string]any{"type": "Resource"}},
						"lastScaleTime":  "2026-07-28T03:05:00Z",
						"conditions": []any{
							map[string]any{"type": "ScalingLimited", "status": "False", "reason": "DesiredWithinRange", "message": "not projected"},
							map[string]any{"type": "ScalingActive", "status": "True", "reason": "ValidMetricFound", "message": "not projected"},
						},
					},
				}},
			})
		case "/apis/policy/v1/namespaces/payments/poddisruptionbudgets":
			writeTestJSON(t, w, map[string]any{
				"apiVersion": "policy/v1", "kind": "PodDisruptionBudgetList", "metadata": map[string]any{},
				"items": []any{
					map[string]any{
						"apiVersion": "policy/v1", "kind": "PodDisruptionBudget",
						"metadata": map[string]any{
							"name": "none-budget", "namespace": "payments", "generation": 2,
							"creationTimestamp": "2026-07-28T03:10:00Z",
						},
						"spec": map[string]any{"maxUnavailable": 1, "unhealthyPodEvictionPolicy": "AlwaysAllow"},
						"status": map[string]any{
							"observedGeneration": 1, "currentHealthy": 0, "desiredHealthy": 0,
							"disruptionsAllowed": 0, "expectedPods": 0,
						},
					},
					map[string]any{
						"apiVersion": "policy/v1", "kind": "PodDisruptionBudget",
						"metadata": map[string]any{
							"name": "all-budget", "namespace": "payments", "generation": 1,
							"creationTimestamp": "2026-07-28T03:11:00Z",
						},
						"spec": map[string]any{"selector": map[string]any{}, "maxUnavailable": "25%"},
						"status": map[string]any{
							"observedGeneration": 1, "currentHealthy": 4, "desiredHealthy": 3,
							"disruptionsAllowed": 1, "expectedPods": 4,
						},
					},
					map[string]any{
						"apiVersion": "policy/v1", "kind": "PodDisruptionBudget",
						"metadata": map[string]any{
							"name": "gateway-budget", "namespace": "payments", "generation": 7,
							"creationTimestamp": "2026-07-28T03:12:00Z",
						},
						"spec": map[string]any{
							"selector": map[string]any{
								"matchLabels":      map[string]string{"private-app": "not-projected"},
								"matchExpressions": []any{map[string]any{"key": "tier", "operator": "In", "values": []string{"api"}}},
							},
							"minAvailable": "75%",
						},
						"status": map[string]any{
							"observedGeneration": 7, "currentHealthy": 3, "desiredHealthy": 3,
							"disruptionsAllowed": 1, "expectedPods": 4,
							"disruptedPods": map[string]any{"gateway-private": "2026-07-28T03:13:00Z"},
							"conditions": []any{map[string]any{
								"type": "DisruptionAllowed", "status": "True", "reason": "SufficientPods", "message": "not projected",
							}},
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := newBatchTestClient(t, server)
	autoscalers, err := client.HorizontalPodAutoscalers(context.Background(), "payments")
	if err != nil {
		t.Fatalf("HorizontalPodAutoscalers() error = %v", err)
	}
	if len(autoscalers) != 1 {
		t.Fatalf("autoscalers = %#v", autoscalers)
	}
	hpa := autoscalers[0]
	if hpa.Name != "gateway-autoscaler" || hpa.Namespace != "payments" || hpa.TargetAPIVersion != "apps/v1" ||
		hpa.TargetKind != "Deployment" || hpa.TargetName != "gateway" || hpa.MinReplicas != 1 ||
		!hpa.MinReplicasDefaulted || hpa.MaxReplicas != 10 || hpa.CurrentReplicas != 3 || hpa.DesiredReplicas != 5 ||
		hpa.MetricCount != 2 || hpa.CurrentMetricCount != 1 || !hpa.Observed || hpa.LastScaleTime == nil {
		t.Fatalf("autoscaler = %#v", hpa)
	}
	if len(hpa.Conditions) != 2 || hpa.ConditionCount != 2 || hpa.ConditionsTruncated ||
		hpa.Conditions[0].Type != "ScalingActive" || hpa.Conditions[0].Status != "True" ||
		hpa.Conditions[0].Reason != "ValidMetricFound" {
		t.Fatalf("autoscaler conditions = %#v", hpa.Conditions)
	}

	budgets, err := client.PodDisruptionBudgets(context.Background(), "payments")
	if err != nil {
		t.Fatalf("PodDisruptionBudgets() error = %v", err)
	}
	if len(budgets) != 3 || budgets[0].Name != "all-budget" || budgets[1].Name != "gateway-budget" || budgets[2].Name != "none-budget" {
		t.Fatalf("budgets = %#v", budgets)
	}
	if budgets[0].SelectorMode != domain.KubernetesSelectorAll || budgets[0].MaxUnavailable != "25%" || !budgets[0].Observed {
		t.Fatalf("all selector budget = %#v", budgets[0])
	}
	filtered := budgets[1]
	if filtered.SelectorMode != domain.KubernetesSelectorFiltered || filtered.SelectorLabelCount != 1 ||
		filtered.SelectorExpressionCount != 1 || filtered.MinAvailable != "75%" || filtered.MaxUnavailable != "" ||
		filtered.CurrentHealthy != 3 || filtered.DesiredHealthy != 3 || filtered.DisruptionsAllowed != 1 ||
		filtered.ExpectedPods != 4 || !filtered.Observed || filtered.UnhealthyPodEvictionPolicy != "IfHealthyBudget" ||
		!filtered.UnhealthyPodEvictionPolicyDefaulted || len(filtered.Conditions) != 1 ||
		filtered.Conditions[0].Reason != "SufficientPods" {
		t.Fatalf("filtered budget = %#v", filtered)
	}
	if budgets[2].SelectorMode != domain.KubernetesSelectorNone || budgets[2].MaxUnavailable != "1" || budgets[2].Observed ||
		budgets[2].UnhealthyPodEvictionPolicy != "AlwaysAllow" || budgets[2].UnhealthyPodEvictionPolicyDefaulted {
		t.Fatalf("none selector budget = %#v", budgets[2])
	}
}

func TestClientListsClusterDisruptionBudgetEvidence(t *testing.T) {
	t.Parallel()

	item := func(namespace, name string, generation int64, observedGeneration *int64, expectedPods, disruptionsAllowed int32) map[string]any {
		status := map[string]any{
			"currentHealthy": 3, "desiredHealthy": 3,
			"expectedPods": expectedPods, "disruptionsAllowed": disruptionsAllowed,
		}
		if observedGeneration != nil {
			status["observedGeneration"] = *observedGeneration
		}
		return map[string]any{
			"apiVersion": "policy/v1", "kind": "PodDisruptionBudget",
			"metadata": map[string]any{
				"namespace": namespace, "name": name, "generation": generation,
				"creationTimestamp": "2026-07-30T03:00:00Z",
			},
			"spec": map[string]any{
				"selector":       map[string]any{"matchLabels": map[string]string{"private-app": "not-projected"}},
				"maxUnavailable": 1,
			},
			"status": status,
		}
	}
	observed := int64(2)
	stale := int64(1)
	var requests []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RequestURI())
		if r.URL.Path != "/apis/policy/v1/poddisruptionbudgets" || r.URL.Query().Get("limit") != governanceListPageSize {
			t.Errorf("request URI = %q", r.URL.RequestURI())
		}
		response := map[string]any{
			"apiVersion": "policy/v1", "kind": "PodDisruptionBudgetList", "metadata": map[string]any{},
			"items": []any{
				item("beta", "inactive", 2, &observed, 0, 0),
				item("alpha", "z-available", 2, &observed, 4, 1),
			},
		}
		if r.URL.Query().Get("continue") == "page-two" {
			response["items"] = []any{
				item("gamma", "unobserved", 2, &stale, 4, 7),
				item("alpha", "a-blocked", 2, &observed, 4, 0),
			}
		} else {
			response["metadata"] = map[string]any{"continue": "page-two"}
		}
		writeTestJSON(t, w, response)
	}))
	t.Cleanup(server.Close)

	budgets, err := newBatchTestClient(t, server).DisruptionBudgets(context.Background())
	if err != nil {
		t.Fatalf("DisruptionBudgets() error = %v", err)
	}
	if len(budgets) != 4 {
		t.Fatalf("budgets = %#v", budgets)
	}
	want := []struct {
		namespace string
		name      string
		status    domain.KubernetesDisruptionBudgetStatus
	}{
		{namespace: "alpha", name: "a-blocked", status: domain.DisruptionBudgetBlocked},
		{namespace: "alpha", name: "z-available", status: domain.DisruptionBudgetAvailable},
		{namespace: "beta", name: "inactive", status: domain.DisruptionBudgetInactive},
		{namespace: "gamma", name: "unobserved", status: domain.DisruptionBudgetUnobserved},
	}
	for index, expected := range want {
		budget := budgets[index]
		if budget.Namespace != expected.namespace || budget.Name != expected.name || budget.DisruptionStatus != expected.status {
			t.Errorf("budget[%d] = %#v", index, budget)
		}
	}
	if budgets[3].Observed || budgets[3].DisruptionStatus != domain.DisruptionBudgetUnobserved {
		t.Fatalf("stale budget = %#v", budgets[3])
	}
	if len(requests) != 2 || requests[0] != "/apis/policy/v1/poddisruptionbudgets?limit=250" ||
		requests[1] != "/apis/policy/v1/poddisruptionbudgets?continue=page-two&limit=250" {
		t.Fatalf("requests = %#v", requests)
	}
}

func TestClientRejectsClusterDisruptionBudgetWithoutNamespace(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, map[string]any{
			"apiVersion": "policy/v1", "kind": "PodDisruptionBudgetList", "metadata": map[string]any{},
			"items": []any{map[string]any{
				"apiVersion": "policy/v1", "kind": "PodDisruptionBudget",
				"metadata": map[string]any{
					"name": "unscoped", "generation": 1, "creationTimestamp": "2026-07-30T03:00:00Z",
				},
				"spec":   map[string]any{"maxUnavailable": 1},
				"status": map[string]any{"observedGeneration": 1},
			}},
		})
	}))
	t.Cleanup(server.Close)

	_, err := newBatchTestClient(t, server).DisruptionBudgets(context.Background())
	if !errors.Is(err, domain.ErrUpstream) || !strings.Contains(err.Error(), "namespace scope") {
		t.Fatalf("DisruptionBudgets() error = %v", err)
	}
}

func TestClientBoundsAvailabilityConditionProjection(t *testing.T) {
	conditions := make([]any, maxAvailabilityConditionsPerObject)
	for index := range conditions {
		conditions[index] = map[string]any{
			"type": fmt.Sprintf("Condition-%02d", index), "status": "True", "reason": "WithinBounds",
		}
	}
	items := make([]any, maxGovernanceProjectedEntries/maxAvailabilityConditionsPerObject+1)
	for index := range items {
		items[index] = map[string]any{
			"apiVersion": "autoscaling/v2", "kind": "HorizontalPodAutoscaler",
			"metadata": map[string]any{
				"name": fmt.Sprintf("autoscaler-%03d", index), "namespace": "payments", "generation": 1,
				"creationTimestamp": "2026-07-28T03:00:00Z",
			},
			"spec": map[string]any{
				"scaleTargetRef": map[string]any{"kind": "Deployment", "name": "gateway"}, "minReplicas": 1, "maxReplicas": 10,
			},
			"status": map[string]any{"observedGeneration": 1, "conditions": conditions},
		}
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, map[string]any{
			"apiVersion": "autoscaling/v2", "kind": "HorizontalPodAutoscalerList", "metadata": map[string]any{}, "items": items,
		})
	}))
	t.Cleanup(server.Close)

	autoscalers, err := newBatchTestClient(t, server).HorizontalPodAutoscalers(context.Background(), "payments")
	if err != nil {
		t.Fatalf("HorizontalPodAutoscalers() error = %v", err)
	}
	var projected int
	for _, autoscaler := range autoscalers {
		projected += len(autoscaler.Conditions)
	}
	if projected != maxGovernanceProjectedEntries {
		t.Fatalf("projected conditions = %d", projected)
	}
	last := autoscalers[len(autoscalers)-1]
	if len(last.Conditions) != 0 || !last.ConditionsTruncated || last.ConditionCount != maxAvailabilityConditionsPerObject {
		t.Fatalf("last autoscaler = %#v", last)
	}
}

func TestClientRejectsInvalidAvailabilityPolicyInputAndResponses(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writeTestJSON(t, w, map[string]any{
			"apiVersion": "autoscaling/v2", "kind": "HorizontalPodAutoscalerList", "metadata": map[string]any{},
			"items": []any{map[string]any{
				"apiVersion": "autoscaling/v2", "kind": "HorizontalPodAutoscaler",
				"metadata": map[string]any{
					"name": "escaped", "namespace": "other", "generation": 1, "creationTimestamp": "2026-07-28T03:00:00Z",
				},
			}},
		})
	}))
	t.Cleanup(server.Close)
	client := newBatchTestClient(t, server)
	for _, namespace := range []string{"", "bad/namespace"} {
		if _, err := client.HorizontalPodAutoscalers(context.Background(), namespace); err == nil {
			t.Errorf("HorizontalPodAutoscalers(%q) succeeded", namespace)
		}
		if _, err := client.PodDisruptionBudgets(context.Background(), namespace); err == nil {
			t.Errorf("PodDisruptionBudgets(%q) succeeded", namespace)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid namespace reached Kubernetes: %d calls", calls.Load())
	}
	if _, err := client.HorizontalPodAutoscalers(context.Background(), "payments"); !errors.Is(err, domain.ErrUpstream) ||
		!strings.Contains(err.Error(), "namespace scope") {
		t.Fatalf("HorizontalPodAutoscalers() error = %v", err)
	}

	tests := []struct {
		name   string
		spec   map[string]any
		status map[string]any
		want   string
	}{
		{
			name:   "mutually exclusive availability fields",
			spec:   map[string]any{"selector": map[string]any{}, "minAvailable": 1, "maxUnavailable": 1},
			status: map[string]any{},
			want:   "mutually exclusive",
		},
		{
			name:   "negative status count",
			spec:   map[string]any{"selector": map[string]any{}, "minAvailable": 1},
			status: map[string]any{"disruptionsAllowed": -1},
			want:   "status count",
		},
		{
			name:   "unsafe condition",
			spec:   map[string]any{"selector": map[string]any{}, "minAvailable": 1},
			status: map[string]any{"conditions": []any{map[string]any{"type": "DisruptionAllowed", "status": "True", "reason": "bad\nreason"}}},
			want:   "condition",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeTestJSON(t, w, map[string]any{
					"apiVersion": "policy/v1", "kind": "PodDisruptionBudgetList", "metadata": map[string]any{},
					"items": []any{map[string]any{
						"apiVersion": "policy/v1", "kind": "PodDisruptionBudget",
						"metadata": map[string]any{
							"name": "invalid", "namespace": "payments", "generation": 1,
							"creationTimestamp": "2026-07-28T03:00:00Z",
						},
						"spec": test.spec, "status": test.status,
					}},
				})
			}))
			t.Cleanup(server.Close)
			_, err := newBatchTestClient(t, server).PodDisruptionBudgets(context.Background(), "payments")
			if !errors.Is(err, domain.ErrUpstream) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("PodDisruptionBudgets() error = %v", err)
			}
		})
	}
}

func TestDecodeHorizontalPodAutoscalerRejectsUnsafeProjection(t *testing.T) {
	t.Parallel()

	base := func() map[string]any {
		return map[string]any{
			"apiVersion": "autoscaling/v2", "kind": "HorizontalPodAutoscaler",
			"metadata": map[string]any{
				"name": "gateway-autoscaler", "namespace": "payments", "generation": 1,
				"creationTimestamp": "2026-07-28T03:00:00Z",
			},
			"spec": map[string]any{
				"scaleTargetRef": map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "name": "gateway"},
				"minReplicas":    1, "maxReplicas": 10,
			},
			"status": map[string]any{"observedGeneration": 1, "currentReplicas": 3, "desiredReplicas": 4},
		}
	}
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "api version", mutate: func(item map[string]any) { item["apiVersion"] = "autoscaling/v1" }},
		{name: "target api version", mutate: func(item map[string]any) {
			item["spec"].(map[string]any)["scaleTargetRef"].(map[string]any)["apiVersion"] = "apps/v1\n"
		}},
		{name: "target kind", mutate: func(item map[string]any) {
			item["spec"].(map[string]any)["scaleTargetRef"].(map[string]any)["kind"] = ""
		}},
		{name: "minimum replicas", mutate: func(item map[string]any) { item["spec"].(map[string]any)["minReplicas"] = -1 }},
		{name: "maximum replicas", mutate: func(item map[string]any) { item["spec"].(map[string]any)["maxReplicas"] = 0 }},
		{name: "replica ordering", mutate: func(item map[string]any) {
			item["spec"].(map[string]any)["minReplicas"] = 5
			item["spec"].(map[string]any)["maxReplicas"] = 4
		}},
		{name: "current replicas", mutate: func(item map[string]any) { item["status"].(map[string]any)["currentReplicas"] = -1 }},
		{name: "scale time", mutate: func(item map[string]any) {
			item["status"].(map[string]any)["lastScaleTime"] = "0001-01-01T00:00:00Z"
		}},
		{name: "condition status", mutate: func(item map[string]any) {
			item["status"].(map[string]any)["conditions"] = []any{map[string]any{
				"type": "ScalingActive", "status": "Maybe", "reason": "Unknown",
			}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			item := base()
			test.mutate(item)
			raw, err := json.Marshal(item)
			if err != nil {
				t.Fatalf("marshal autoscaler: %v", err)
			}
			remaining := maxGovernanceProjectedEntries
			if _, err := decodeHorizontalPodAutoscaler(raw, "payments", &remaining); !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("decodeHorizontalPodAutoscaler() error = %v", err)
			}
		})
	}
	remaining := maxGovernanceProjectedEntries
	if _, err := decodeHorizontalPodAutoscaler([]byte("{"), "payments", &remaining); !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("malformed decodeHorizontalPodAutoscaler() error = %v", err)
	}
}

func TestProjectAvailabilityValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "missing", raw: "", want: ""},
		{name: "null", raw: "null", want: ""},
		{name: "integer", raw: "2", want: "2"},
		{name: "percentage", raw: `"25%"`, want: "25%"},
		{name: "negative", raw: "-1", wantErr: true},
		{name: "fraction", raw: "1.5", wantErr: true},
		{name: "object", raw: "{}", wantErr: true},
		{name: "empty string", raw: `""`, wantErr: true},
		{name: "too large", raw: `"` + strings.Repeat("x", maxAvailabilityIntOrStringBytes+1) + `"`, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := projectAvailabilityValue(json.RawMessage(test.raw))
			if test.wantErr {
				if !errors.Is(err, domain.ErrUpstream) {
					t.Fatalf("projectAvailabilityValue() error = %v", err)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("projectAvailabilityValue() = %q, %v", got, err)
			}
		})
	}
}
