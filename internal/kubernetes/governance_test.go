package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

func TestClientListsNamespaceGovernanceResources(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != governanceListPageSize {
			t.Errorf("limit = %q", r.URL.Query().Get("limit"))
		}
		switch r.URL.Path {
		case "/api/v1/namespaces/payments/resourcequotas":
			writeTestJSON(t, w, map[string]any{
				"apiVersion": "v1", "kind": "ResourceQuotaList",
				"metadata": map[string]any{},
				"items": []any{map[string]any{
					"apiVersion": "v1", "kind": "ResourceQuota",
					"metadata": map[string]any{
						"name": "compute-quota", "namespace": "payments",
						"creationTimestamp": "2026-07-28T02:00:00Z",
						"labels":            map[string]string{"private": "not-projected"},
					},
					"spec": map[string]any{
						"hard":   map[string]string{"requests.memory": "8Gi", "requests.cpu": "4"},
						"scopes": []string{"NotTerminating"},
						"scopeSelector": map[string]any{"matchExpressions": []any{
							map[string]any{"scopeName": "PriorityClass", "operator": "In", "values": []string{"high"}},
						}},
					},
					"status": map[string]any{
						"hard": map[string]string{"requests.memory": "8Gi", "requests.cpu": "4"},
						"used": map[string]string{"requests.memory": "6Gi", "requests.cpu": "2"},
					},
				}},
			})
		case "/api/v1/namespaces/payments/limitranges":
			writeTestJSON(t, w, map[string]any{
				"apiVersion": "v1", "kind": "LimitRangeList",
				"metadata": map[string]any{},
				"items": []any{map[string]any{
					"apiVersion": "v1", "kind": "LimitRange",
					"metadata": map[string]any{
						"name": "namespace-defaults", "namespace": "payments",
						"creationTimestamp": "2026-07-28T02:05:00Z",
					},
					"spec": map[string]any{"limits": []any{
						map[string]any{
							"type":                 "Container",
							"default":              map[string]string{"memory": "512Mi", "cpu": "500m"},
							"defaultRequest":       map[string]string{"memory": "256Mi", "cpu": "250m"},
							"min":                  map[string]string{"cpu": "100m"},
							"max":                  map[string]string{"cpu": "2"},
							"maxLimitRequestRatio": map[string]string{"cpu": "4"},
						},
						map[string]any{
							"type": "PersistentVolumeClaim",
							"min":  map[string]string{"storage": "1Gi"},
							"max":  map[string]string{"storage": "100Gi"},
						},
					}},
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := newBatchTestClient(t, server)
	quotas, err := client.ResourceQuotas(context.Background(), "payments")
	if err != nil {
		t.Fatalf("ResourceQuotas() error = %v", err)
	}
	if len(quotas) != 1 || quotas[0].Name != "compute-quota" || quotas[0].Namespace != "payments" ||
		quotas[0].ResourceCount != 2 || quotas[0].ResourcesTruncated || quotas[0].ScopeSelectorCount != 1 {
		t.Fatalf("quotas = %#v", quotas)
	}
	if len(quotas[0].Scopes) != 1 || quotas[0].Scopes[0] != "NotTerminating" || quotas[0].ScopeCount != 1 {
		t.Errorf("quota scopes = %#v", quotas[0])
	}
	if got := quotas[0].Resources; len(got) != 2 || got[0].Name != "requests.cpu" || got[0].Hard != "4" ||
		got[0].Used != "2" || !got[0].Observed || got[1].Name != "requests.memory" {
		t.Errorf("quota resources = %#v", got)
	}

	limitRanges, err := client.LimitRanges(context.Background(), "payments")
	if err != nil {
		t.Fatalf("LimitRanges() error = %v", err)
	}
	if len(limitRanges) != 1 || limitRanges[0].Name != "namespace-defaults" || limitRanges[0].ConstraintCount != 3 ||
		limitRanges[0].ConstraintsTruncated {
		t.Fatalf("limitRanges = %#v", limitRanges)
	}
	constraints := limitRanges[0].Constraints
	if len(constraints) != 3 || constraints[0].Type != "Container" || constraints[0].Resource != "cpu" ||
		constraints[0].DefaultRequest != "250m" || constraints[0].Default != "500m" ||
		constraints[0].Min != "100m" || constraints[0].Max != "2" || constraints[0].MaxLimitRequestRatio != "4" ||
		constraints[1].Resource != "memory" || constraints[2].Type != "PersistentVolumeClaim" || constraints[2].Resource != "storage" {
		t.Errorf("limit range constraints = %#v", constraints)
	}
}

func TestClientBoundsNamespaceGovernanceProjection(t *testing.T) {
	t.Parallel()

	quotaHard := make(map[string]string, maxQuotaResourcesPerObject+1)
	limitMax := make(map[string]string, maxLimitConstraintsPerObject+1)
	for index := 0; index < maxQuotaResourcesPerObject+1; index++ {
		quotaHard[fmt.Sprintf("requests.example.com/resource-%03d", index)] = "1"
	}
	for index := 0; index < maxLimitConstraintsPerObject+1; index++ {
		limitMax[fmt.Sprintf("example.com/resource-%03d", index)] = "1"
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metadata := map[string]any{"name": "bounded", "namespace": "payments", "creationTimestamp": "2026-07-28T02:00:00Z"}
		switch r.URL.Path {
		case "/api/v1/namespaces/payments/resourcequotas":
			writeTestJSON(t, w, map[string]any{
				"apiVersion": "v1", "kind": "ResourceQuotaList", "metadata": map[string]any{},
				"items": []any{map[string]any{
					"apiVersion": "v1", "kind": "ResourceQuota", "metadata": metadata,
					"spec": map[string]any{"hard": quotaHard, "scopes": append(
						make([]string, 0, maxQuotaScopesPerObject+1), repeatedGovernanceValues("Scope", maxQuotaScopesPerObject+1)...,
					)},
				}},
			})
		case "/api/v1/namespaces/payments/limitranges":
			writeTestJSON(t, w, map[string]any{
				"apiVersion": "v1", "kind": "LimitRangeList", "metadata": map[string]any{},
				"items": []any{map[string]any{
					"apiVersion": "v1", "kind": "LimitRange", "metadata": metadata,
					"spec": map[string]any{"limits": []any{map[string]any{"type": "Container", "max": limitMax}}},
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := newBatchTestClient(t, server)
	quotas, err := client.ResourceQuotas(context.Background(), "payments")
	if err != nil {
		t.Fatalf("ResourceQuotas() error = %v", err)
	}
	if len(quotas) != 1 || len(quotas[0].Resources) != maxQuotaResourcesPerObject || !quotas[0].ResourcesTruncated ||
		quotas[0].ResourceCount != maxQuotaResourcesPerObject+1 || len(quotas[0].Scopes) != maxQuotaScopesPerObject ||
		!quotas[0].ScopesTruncated || quotas[0].ScopeCount != maxQuotaScopesPerObject+1 {
		t.Fatalf("bounded quota = %#v", quotas)
	}
	if !sort.SliceIsSorted(quotas[0].Resources, func(i, j int) bool {
		return quotas[0].Resources[i].Name < quotas[0].Resources[j].Name
	}) {
		t.Errorf("quota resources are not sorted: %#v", quotas[0].Resources)
	}

	limitRanges, err := client.LimitRanges(context.Background(), "payments")
	if err != nil {
		t.Fatalf("LimitRanges() error = %v", err)
	}
	if len(limitRanges) != 1 || len(limitRanges[0].Constraints) != maxLimitConstraintsPerObject ||
		!limitRanges[0].ConstraintsTruncated || limitRanges[0].ConstraintCount != maxLimitConstraintsPerObject+1 {
		t.Fatalf("bounded limit range = %#v", limitRanges)
	}
}

func TestClientBoundsNamespaceGovernanceRequestProjection(t *testing.T) {
	t.Parallel()

	quotaHard := make(map[string]string, maxQuotaResourcesPerObject)
	limitMax := make(map[string]string, maxLimitConstraintsPerObject)
	for index := 0; index < maxQuotaResourcesPerObject; index++ {
		quotaHard[fmt.Sprintf("requests.example.com/resource-%03d", index)] = "1"
	}
	for index := 0; index < maxLimitConstraintsPerObject; index++ {
		limitMax[fmt.Sprintf("example.com/resource-%03d", index)] = "1"
	}
	quotaItems := make([]any, maxGovernanceProjectedEntries/maxQuotaResourcesPerObject+1)
	for index := range quotaItems {
		quotaItems[index] = map[string]any{
			"apiVersion": "v1", "kind": "ResourceQuota",
			"metadata": map[string]any{
				"name": fmt.Sprintf("quota-%03d", index), "namespace": "payments",
				"creationTimestamp": "2026-07-28T02:00:00Z",
			},
			"spec": map[string]any{
				"hard": quotaHard, "scopes": repeatedGovernanceValues("Scope", maxQuotaScopesPerObject),
			},
		}
	}
	limitItems := make([]any, maxGovernanceProjectedEntries/maxLimitConstraintsPerObject+1)
	for index := range limitItems {
		limitItems[index] = map[string]any{
			"apiVersion": "v1", "kind": "LimitRange",
			"metadata": map[string]any{
				"name": fmt.Sprintf("limits-%03d", index), "namespace": "payments",
				"creationTimestamp": "2026-07-28T02:00:00Z",
			},
			"spec": map[string]any{"limits": []any{map[string]any{"type": "Container", "max": limitMax}}},
		}
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/namespaces/payments/resourcequotas":
			writeTestJSON(t, w, map[string]any{
				"apiVersion": "v1", "kind": "ResourceQuotaList", "metadata": map[string]any{}, "items": quotaItems,
			})
		case "/api/v1/namespaces/payments/limitranges":
			writeTestJSON(t, w, map[string]any{
				"apiVersion": "v1", "kind": "LimitRangeList", "metadata": map[string]any{}, "items": limitItems,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := newBatchTestClient(t, server)
	quotas, err := client.ResourceQuotas(context.Background(), "payments")
	if err != nil {
		t.Fatalf("ResourceQuotas() error = %v", err)
	}
	var projectedResources, projectedScopes int
	for _, quota := range quotas {
		projectedResources += len(quota.Resources)
		projectedScopes += len(quota.Scopes)
	}
	if projectedResources != maxGovernanceProjectedEntries || projectedScopes != maxGovernanceProjectedScopes {
		t.Fatalf("quota projection = %d resources, %d scopes", projectedResources, projectedScopes)
	}
	lastQuota := quotas[len(quotas)-1]
	if len(lastQuota.Resources) != 0 || !lastQuota.ResourcesTruncated || len(lastQuota.Scopes) != 0 || !lastQuota.ScopesTruncated {
		t.Fatalf("last quota = %#v", lastQuota)
	}

	limitRanges, err := client.LimitRanges(context.Background(), "payments")
	if err != nil {
		t.Fatalf("LimitRanges() error = %v", err)
	}
	var projectedConstraints int
	for _, limitRange := range limitRanges {
		projectedConstraints += len(limitRange.Constraints)
	}
	if projectedConstraints != maxGovernanceProjectedEntries {
		t.Fatalf("limit range projection = %d", projectedConstraints)
	}
	lastLimitRange := limitRanges[len(limitRanges)-1]
	if len(lastLimitRange.Constraints) != 0 || !lastLimitRange.ConstraintsTruncated {
		t.Fatalf("last limit range = %#v", lastLimitRange)
	}
}

func TestClientRejectsInvalidNamespaceGovernanceResponses(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeTestJSON(t, w, map[string]any{
			"apiVersion": "v1", "kind": "ResourceQuotaList", "metadata": map[string]any{},
			"items": []any{map[string]any{
				"apiVersion": "v1", "kind": "ResourceQuota",
				"metadata": map[string]any{
					"name": "escaped", "namespace": "other", "creationTimestamp": "2026-07-28T02:00:00Z",
				},
			}},
		})
	}))
	t.Cleanup(server.Close)
	client := newBatchTestClient(t, server)

	for _, namespace := range []string{"", "bad/namespace"} {
		if _, err := client.ResourceQuotas(context.Background(), namespace); err == nil {
			t.Errorf("ResourceQuotas(%q) succeeded", namespace)
		}
		if _, err := client.LimitRanges(context.Background(), namespace); err == nil {
			t.Errorf("LimitRanges(%q) succeeded", namespace)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid namespace reached Kubernetes: %d calls", calls.Load())
	}
	if _, err := client.ResourceQuotas(context.Background(), "payments"); !errors.Is(err, domain.ErrUpstream) ||
		!strings.Contains(err.Error(), "namespace scope") {
		t.Fatalf("ResourceQuotas() error = %v", err)
	}
}

func TestClientStopsNamespaceGovernancePaginationAtSafeLimit(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeTestJSON(t, w, map[string]any{
			"apiVersion": "v1", "kind": "ResourceQuotaList",
			"metadata": map[string]any{"continue": fmt.Sprintf("page-%d", calls.Load()+1)},
			"items":    []any{},
		})
	}))
	t.Cleanup(server.Close)

	client := newBatchTestClient(t, server)
	_, err := client.ResourceQuotas(context.Background(), "payments")
	if !errors.Is(err, domain.ErrUpstream) || !strings.Contains(err.Error(), "safe page limit") {
		t.Fatalf("ResourceQuotas() error = %v", err)
	}
	if calls.Load() != maxGovernanceListPages {
		t.Fatalf("pagination calls = %d, want %d", calls.Load(), maxGovernanceListPages)
	}
}

func repeatedGovernanceValues(prefix string, count int) []string {
	values := make([]string, count)
	for index := range values {
		values[index] = fmt.Sprintf("%s-%02d", prefix, index)
	}
	return values
}
