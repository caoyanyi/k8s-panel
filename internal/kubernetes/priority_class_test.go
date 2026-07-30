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

func TestClientListsPriorityClassesWithMetadataOnlyPagination(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requests := make([]string, 0, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != priorityClassCollectionPath {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Accept"); got != kubernetesPartialMetadataListAccept {
			t.Errorf("Accept = %q, want %q", got, kubernetesPartialMetadataListAccept)
		}
		if got := r.URL.Query().Get("limit"); got != priorityClassListPageSize {
			t.Errorf("limit = %q, want %q", got, priorityClassListPageSize)
		}
		mu.Lock()
		requests = append(requests, r.URL.RequestURI())
		mu.Unlock()

		if r.URL.Query().Get("continue") == "page-two" {
			writeTestJSON(t, w, accessMetadataList("", []any{
				accessMetadata("system-cluster-critical", "", "2026-07-29T09:00:00Z"),
			}))
			return
		}
		writeTestJSON(t, w, accessMetadataList("page-two", []any{
			accessMetadata("workload-high", "", "2026-07-30T09:00:00Z"),
		}))
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	items, err := client.PriorityClasses(context.Background())
	if err != nil {
		t.Fatalf("PriorityClasses() error = %v", err)
	}
	if len(items) != 2 || items[0].Name != "system-cluster-critical" || items[1].Name != "workload-high" ||
		items[0].CreatedAt.IsZero() || items[1].CreatedAt.IsZero() {
		t.Fatalf("PriorityClasses() = %#v", items)
	}
	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		priorityClassCollectionPath + "?limit=250",
		priorityClassCollectionPath + "?continue=page-two&limit=250",
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

func TestClientReadsRedactedPriorityClassDetail(t *testing.T) {
	t.Parallel()

	const name = "system-cluster-critical"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != priorityClassCollectionPath+"/"+name {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		payload := priorityClassPayload(name, int64(2_000_000_000), true, "Never")
		payload["description"] = "private scheduling guidance"
		metadata := payload["metadata"].(map[string]any)
		metadata["uid"] = "private-uid"
		metadata["resourceVersion"] = "private-resource-version"
		metadata["labels"] = map[string]any{"private-label": "private-value"}
		metadata["annotations"] = map[string]any{"private-annotation": "private-value"}
		metadata["managedFields"] = []any{map[string]any{"manager": "private-manager"}}
		writeTestJSON(t, w, payload)
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	detail, err := client.PriorityClass(context.Background(), name)
	if err != nil {
		t.Fatalf("PriorityClass() error = %v", err)
	}
	if detail.Name != name || detail.Value != 2_000_000_000 || !detail.GlobalDefault ||
		detail.PreemptionPolicy != domain.PriorityClassPreemptNever || detail.PreemptionPolicyDefaulted ||
		detail.CreatedAt.IsZero() {
		t.Fatalf("PriorityClass() = %#v", detail)
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal PriorityClass detail: %v", err)
	}
	for _, forbidden := range []string{
		"private scheduling guidance", "private-uid", "private-resource-version", "private-label",
		"private-value", "private-annotation", "private-manager", "description", "managedFields",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("PriorityClass detail leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestDecodePriorityClassDefaultsPreemptionPolicy(t *testing.T) {
	t.Parallel()

	detail, err := decodePriorityClass(mustPriorityClassJSON(t, priorityClassPayload("workload-default", 0, false, nil)), "workload-default")
	if err != nil {
		t.Fatalf("decodePriorityClass() error = %v", err)
	}
	if detail.Value != 0 || detail.GlobalDefault || detail.PreemptionPolicy != domain.PriorityClassPreemptLower ||
		!detail.PreemptionPolicyDefaulted {
		t.Fatalf("decodePriorityClass() = %#v", detail)
	}
}

func TestDecodePriorityClassRejectsInvalidUpstreamObjects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "api version", mutate: func(payload map[string]any) { payload["apiVersion"] = "scheduling.k8s.io/v1beta1" }},
		{name: "kind", mutate: func(payload map[string]any) { payload["kind"] = "Pod" }},
		{name: "name", mutate: func(payload map[string]any) { payload["metadata"].(map[string]any)["name"] = "other" }},
		{name: "namespace", mutate: func(payload map[string]any) { payload["metadata"].(map[string]any)["namespace"] = "default" }},
		{name: "creation time", mutate: func(payload map[string]any) { payload["metadata"].(map[string]any)["creationTimestamp"] = nil }},
		{name: "missing value", mutate: func(payload map[string]any) { delete(payload, "value") }},
		{name: "string value", mutate: func(payload map[string]any) { payload["value"] = "1000" }},
		{name: "overflow value", mutate: func(payload map[string]any) { payload["value"] = int64(2_147_483_648) }},
		{name: "preemption policy", mutate: func(payload map[string]any) { payload["preemptionPolicy"] = "Always" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			payload := priorityClassPayload("workload-high", 1000, false, "PreemptLowerPriority")
			tt.mutate(payload)
			_, err := decodePriorityClass(mustPriorityClassJSON(t, payload), "workload-high")
			if !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("decodePriorityClass() error = %v, want upstream error", err)
			}
		})
	}
}

func TestClientRejectsInvalidPriorityClassNameBeforeRequest(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	_, err := client.PriorityClass(context.Background(), "../priorityclasses")
	var validationErr *domain.ValidationError
	if !errors.As(err, &validationErr) || validationErr.Field != "name" {
		t.Fatalf("PriorityClass() error = %v, want name validation error", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid name made %d upstream requests", calls.Load())
	}
}

func TestClientRejectsUnsafePriorityClassMetadataLists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload any
	}{
		{name: "wrong list type", payload: map[string]any{"apiVersion": "v1", "kind": "List", "items": []any{}}},
		{name: "namespaced item", payload: accessMetadataList("", []any{accessMetadata("workload-high", "default", "2026-07-30T09:00:00Z")})},
		{name: "invalid name", payload: accessMetadataList("", []any{accessMetadata("Workload-High", "", "2026-07-30T09:00:00Z")})},
		{name: "duplicate name", payload: accessMetadataList("", []any{
			accessMetadata("workload-high", "", "2026-07-30T09:00:00Z"),
			accessMetadata("workload-high", "", "2026-07-30T09:00:00Z"),
		})},
		{name: "unsafe continuation", payload: accessMetadataList("next\npage", []any{})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeTestJSON(t, w, tt.payload)
			}))
			t.Cleanup(server.Close)
			client := newNetworkTestClient(t, server)
			if _, err := client.PriorityClasses(context.Background()); !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("PriorityClasses() error = %v, want upstream error", err)
			}
		})
	}
}

func TestClientBoundsPriorityClassPagesItemsAndBodies(t *testing.T) {
	t.Run("pages", func(t *testing.T) {
		var calls atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			call := calls.Add(1)
			writeTestJSON(t, w, accessMetadataList("page-"+string(rune('a'+call)), []any{}))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.PriorityClasses(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("PriorityClasses() error = %v, want upstream page error", err)
		}
		if calls.Load() != priorityClassMaxListPages {
			t.Fatalf("upstream calls = %d, want %d", calls.Load(), priorityClassMaxListPages)
		}
	})

	t.Run("items", func(t *testing.T) {
		items := make([]any, priorityClassMaxListItems+1)
		for index := range items {
			items[index] = accessMetadata("class-"+strings.Repeat("a", 8)+"-"+string(rune(0x1000+index)), "", "2026-07-30T09:00:00Z")
		}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, accessMetadataList("", items))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.PriorityClasses(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("PriorityClasses() error = %v, want upstream item error", err)
		}
	})

	t.Run("list bytes", func(t *testing.T) {
		metadata := accessMetadata("workload-high", "", "2026-07-30T09:00:00Z")
		metadata["metadata"].(map[string]any)["annotations"] = map[string]any{
			"private.example.com/padding": strings.Repeat("x", int(priorityClassMaxListBytes)),
		}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, accessMetadataList("", []any{metadata}))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.PriorityClasses(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("PriorityClasses() error = %v, want upstream byte error", err)
		}
	})

	t.Run("detail bytes", func(t *testing.T) {
		payload := priorityClassPayload("workload-high", 1000, false, nil)
		payload["description"] = strings.Repeat("x", int(priorityClassMaxDetailBytes))
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, payload)
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.PriorityClass(context.Background(), "workload-high"); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("PriorityClass() error = %v, want upstream byte error", err)
		}
	})
}

func priorityClassPayload(name string, value any, globalDefault bool, preemptionPolicy any) map[string]any {
	payload := map[string]any{
		"apiVersion": "scheduling.k8s.io/v1",
		"kind":       "PriorityClass",
		"metadata": map[string]any{
			"name": name, "creationTimestamp": "2026-07-30T09:00:00Z",
		},
		"value":         value,
		"globalDefault": globalDefault,
	}
	if preemptionPolicy != nil {
		payload["preemptionPolicy"] = preemptionPolicy
	}
	return payload
}

func mustPriorityClassJSON(t *testing.T, payload any) []byte {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal PriorityClass fixture: %v", err)
	}
	return encoded
}
