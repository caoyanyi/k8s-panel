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

func TestClientListsBoundedAPIServicesWithoutSensitiveFields(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requests := make([]string, 0, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != apiServiceCollectionPath {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		if got := r.URL.Query().Get("limit"); got != apiServiceListPageSize {
			t.Errorf("limit = %q, want %q", got, apiServiceListPageSize)
		}
		mu.Lock()
		requests = append(requests, r.URL.RequestURI())
		mu.Unlock()

		if r.URL.Query().Get("continue") == "page-two" {
			writeTestJSON(t, w, apiServiceListResponse("", []any{
				apiServiceResponse("v1.", "", "v1", nil, []any{
					apiServiceCondition("Available", "True", "Local", "2026-07-26T08:01:00Z"),
				}),
			}))
			return
		}
		remote := apiServiceResponse(
			"v1beta1.metrics.k8s.io", "metrics.k8s.io", "v1beta1",
			map[string]any{"namespace": "kube-system", "name": "metrics-server"},
			[]any{apiServiceCondition("Available", "False", "FailedDiscoveryCheck", "2026-07-26T08:02:00Z")},
		)
		remote["metadata"].(map[string]any)["labels"] = map[string]string{"private-label": "must-not-be-projected"}
		spec := remote["spec"].(map[string]any)
		spec["caBundle"] = "private-ca-bundle"
		spec["insecureSkipTLSVerify"] = true
		condition := remote["status"].(map[string]any)["conditions"].([]any)[0].(map[string]any)
		condition["message"] = "private discovery endpoint detail"
		writeTestJSON(t, w, apiServiceListResponse("page-two", []any{remote}))
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	items, err := client.APIServices(context.Background())
	if err != nil {
		t.Fatalf("APIServices() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("APIServices() returned %d items", len(items))
	}
	local, remote := items[0], items[1]
	if local.Name != "v1." || local.Group != "" || local.Version != "v1" || !local.Local ||
		!local.AvailabilityObserved || local.AvailabilityStatus != "True" || local.AvailabilityReason != "Local" {
		t.Fatalf("local APIService = %#v", local)
	}
	if remote.Name != "v1beta1.metrics.k8s.io" || remote.Group != "metrics.k8s.io" || remote.Version != "v1beta1" ||
		remote.Local || remote.ServiceNamespace != "kube-system" || remote.ServiceName != "metrics-server" ||
		remote.ServicePort != 443 || !remote.ServicePortDefaulted || !remote.InsecureSkipTLSVerify ||
		!remote.AvailabilityObserved || remote.AvailabilityStatus != "False" ||
		remote.AvailabilityReason != "FailedDiscoveryCheck" || remote.AvailabilityTransitionTime == nil ||
		remote.ConditionCount != 1 || remote.GroupPriorityMinimum != 100 || remote.VersionPriority != 100 ||
		remote.CreatedAt.IsZero() {
		t.Fatalf("remote APIService = %#v", remote)
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal APIService list: %v", err)
	}
	for _, forbidden := range []string{"private-ca-bundle", "private-label", "private discovery endpoint detail", "caBundle", "message"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("APIService list leaked %q: %s", forbidden, encoded)
		}
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		apiServiceCollectionPath + "?limit=250",
		apiServiceCollectionPath + "?continue=page-two&limit=250",
	}
	if len(gotRequests) != len(wantRequests) {
		t.Fatalf("request URIs = %#v, want %#v", gotRequests, wantRequests)
	}
	for index := range wantRequests {
		if gotRequests[index] != wantRequests[index] {
			t.Fatalf("request URIs = %#v, want %#v", gotRequests, wantRequests)
		}
	}
}

func TestClientAPIServicesReportsMissingAvailabilityCondition(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, apiServiceListResponse("", []any{
			apiServiceResponse(
				"v1alpha1.catalog.example.com", "catalog.example.com", "v1alpha1",
				map[string]any{"namespace": "catalog-system", "name": "catalog-api", "port": 7443}, nil,
			),
		}))
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	items, err := client.APIServices(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("APIServices() = %#v, %v", items, err)
	}
	item := items[0]
	if item.AvailabilityObserved || item.AvailabilityStatus != "" || item.AvailabilityTransitionTime != nil ||
		item.ServicePort != 7443 || item.ServicePortDefaulted {
		t.Fatalf("APIService without availability = %#v", item)
	}
}

func TestClientBoundsAPIServices(t *testing.T) {
	t.Parallel()

	t.Run("page limit", func(t *testing.T) {
		var requests atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			writeTestJSON(t, w, apiServiceListResponse("more", []any{}))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)

		if _, err := client.APIServices(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("APIServices() error = %v, want upstream error", err)
		}
		if got := requests.Load(); got != apiServiceMaxListPages {
			t.Fatalf("requests = %d, want %d", got, apiServiceMaxListPages)
		}
	})

	t.Run("response bytes", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", int(apiServiceMaxListBytes)+1)))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)

		if _, err := client.APIServices(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("APIServices() error = %v, want upstream error", err)
		}
	})

	t.Run("request condition budget", func(t *testing.T) {
		items := make([]any, 0, apiServiceMaxTotalConditions/maxAPIServiceConditions+1)
		for itemIndex := 0; itemIndex < apiServiceMaxTotalConditions/maxAPIServiceConditions+1; itemIndex++ {
			conditions := make([]any, maxAPIServiceConditions)
			for conditionIndex := range conditions {
				conditions[conditionIndex] = apiServiceCondition(
					"Condition"+versionName(conditionIndex), "True", "Observed", "2026-07-26T08:01:00Z",
				)
			}
			version := versionName(itemIndex)
			items = append(items, apiServiceResponse(version+".metrics.k8s.io", "metrics.k8s.io", version, nil, conditions))
		}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, apiServiceListResponse("", items))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)

		if _, err := client.APIServices(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("APIServices() error = %v, want upstream error", err)
		}
	})
}

func TestClientRejectsInvalidAPIServiceResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response func() map[string]any
	}{
		{
			name: "unexpected list identity",
			response: func() map[string]any {
				return map[string]any{"apiVersion": "v1", "kind": "List", "items": []any{}}
			},
		},
		{
			name: "unsafe continuation token",
			response: func() map[string]any {
				return apiServiceListResponse(strings.Repeat("x", apiServiceMaxContinueBytes+1), []any{})
			},
		},
		{
			name: "identity mismatch",
			response: func() map[string]any {
				return apiServiceListResponse("", []any{
					apiServiceResponse("v1beta1.metrics.k8s.io", "custom.metrics.k8s.io", "v1beta1", nil, nil),
				})
			},
		},
		{
			name: "namespaced object",
			response: func() map[string]any {
				item := apiServiceResponse("v1beta1.metrics.k8s.io", "metrics.k8s.io", "v1beta1", nil, nil)
				item["metadata"].(map[string]any)["namespace"] = "kube-system"
				return apiServiceListResponse("", []any{item})
			},
		},
		{
			name: "invalid service reference",
			response: func() map[string]any {
				return apiServiceListResponse("", []any{apiServiceResponse(
					"v1beta1.metrics.k8s.io", "metrics.k8s.io", "v1beta1",
					map[string]any{"namespace": "../kube-system", "name": "metrics-server"}, nil,
				)})
			},
		},
		{
			name: "invalid explicit service port",
			response: func() map[string]any {
				return apiServiceListResponse("", []any{apiServiceResponse(
					"v1beta1.metrics.k8s.io", "metrics.k8s.io", "v1beta1",
					map[string]any{"namespace": "kube-system", "name": "metrics-server", "port": 0}, nil,
				)})
			},
		},
		{
			name: "invalid priority",
			response: func() map[string]any {
				item := apiServiceResponse("v1beta1.metrics.k8s.io", "metrics.k8s.io", "v1beta1", nil, nil)
				item["spec"].(map[string]any)["versionPriority"] = 0
				return apiServiceListResponse("", []any{item})
			},
		},
		{
			name: "duplicate condition",
			response: func() map[string]any {
				condition := apiServiceCondition("Available", "True", "Local", "2026-07-26T08:01:00Z")
				return apiServiceListResponse("", []any{
					apiServiceResponse("v1.metrics.k8s.io", "metrics.k8s.io", "v1", nil, []any{condition, condition}),
				})
			},
		},
		{
			name: "invalid condition status",
			response: func() map[string]any {
				return apiServiceListResponse("", []any{apiServiceResponse(
					"v1.metrics.k8s.io", "metrics.k8s.io", "v1", nil,
					[]any{apiServiceCondition("Available", "Maybe", "Unknown", "2026-07-26T08:01:00Z")},
				)})
			},
		},
		{
			name: "condition item limit",
			response: func() map[string]any {
				conditions := make([]any, maxAPIServiceConditions+1)
				for index := range conditions {
					conditions[index] = apiServiceCondition(
						"Condition"+versionName(index), "True", "Observed", "2026-07-26T08:01:00Z",
					)
				}
				return apiServiceListResponse("", []any{
					apiServiceResponse("v1.metrics.k8s.io", "metrics.k8s.io", "v1", nil, conditions),
				})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeTestJSON(t, w, tt.response())
			}))
			t.Cleanup(server.Close)
			client := newNetworkTestClient(t, server)

			if _, err := client.APIServices(context.Background()); !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("APIServices() error = %v, want upstream error", err)
			}
		})
	}
}

func apiServiceListResponse(continuation string, items []any) map[string]any {
	return map[string]any{
		"apiVersion": "apiregistration.k8s.io/v1",
		"kind":       "APIServiceList",
		"metadata":   map[string]any{"continue": continuation},
		"items":      items,
	}
}

func apiServiceResponse(name, group, version string, service any, conditions []any) map[string]any {
	return map[string]any{
		"apiVersion": "apiregistration.k8s.io/v1",
		"kind":       "APIService",
		"metadata": map[string]any{
			"name": name, "creationTimestamp": "2026-07-26T08:00:00Z",
		},
		"spec": map[string]any{
			"group": group, "version": version, "groupPriorityMinimum": 100, "versionPriority": 100,
			"service": service,
		},
		"status": map[string]any{"conditions": conditions},
	}
}

func apiServiceCondition(conditionType, status, reason, transitionTime string) map[string]any {
	return map[string]any{
		"type": conditionType, "status": status, "reason": reason, "lastTransitionTime": transitionTime,
	}
}
