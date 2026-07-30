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

func TestClientListsRuntimeClassesWithMetadataOnlyPagination(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requests := make([]string, 0, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != runtimeClassCollectionPath {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Accept"); got != kubernetesPartialMetadataListAccept {
			t.Errorf("Accept = %q, want %q", got, kubernetesPartialMetadataListAccept)
		}
		if got := r.URL.Query().Get("limit"); got != runtimeClassListPageSize {
			t.Errorf("limit = %q, want %q", got, runtimeClassListPageSize)
		}
		mu.Lock()
		requests = append(requests, r.URL.RequestURI())
		mu.Unlock()

		if r.URL.Query().Get("continue") == "page-two" {
			writeTestJSON(t, w, accessMetadataList("", []any{
				accessMetadata("runc", "", "2026-07-29T09:00:00Z"),
			}))
			return
		}
		writeTestJSON(t, w, accessMetadataList("page-two", []any{
			accessMetadata("kata-containers", "", "2026-07-30T09:00:00Z"),
		}))
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	items, err := client.RuntimeClasses(context.Background())
	if err != nil {
		t.Fatalf("RuntimeClasses() error = %v", err)
	}
	if len(items) != 2 || items[0].Name != "kata-containers" || items[1].Name != "runc" ||
		items[0].CreatedAt.IsZero() || items[1].CreatedAt.IsZero() {
		t.Fatalf("RuntimeClasses() = %#v", items)
	}
	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		runtimeClassCollectionPath + "?limit=250",
		runtimeClassCollectionPath + "?continue=page-two&limit=250",
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

func TestClientReadsRedactedRuntimeClassDetail(t *testing.T) {
	t.Parallel()

	const name = "kata-containers"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != runtimeClassCollectionPath+"/"+name {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		payload := runtimeClassPayload(name, "kata-fc")
		payload["overhead"] = map[string]any{"podFixed": map[string]any{
			"cpu": "250m", "memory": "120Mi", "vendor.example.com/private-device": "1",
		}}
		payload["scheduling"] = map[string]any{
			"nodeSelector": map[string]any{"private.example.com/runtime": "kata", "topology.kubernetes.io/zone": "zone-a"},
			"tolerations": []any{
				map[string]any{"key": "private-taint", "operator": "Exists", "effect": "NoSchedule"},
				map[string]any{"key": "other-taint", "value": "private-value", "effect": "NoExecute"},
			},
		}
		metadata := payload["metadata"].(map[string]any)
		metadata["uid"] = "private-uid"
		metadata["labels"] = map[string]any{"private-label": "private-value"}
		metadata["annotations"] = map[string]any{"private-annotation": "private-value"}
		metadata["managedFields"] = []any{map[string]any{"manager": "private-manager"}}
		writeTestJSON(t, w, payload)
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	detail, err := client.RuntimeClass(context.Background(), name)
	if err != nil {
		t.Fatalf("RuntimeClass() error = %v", err)
	}
	if detail.Name != name || detail.Handler != "kata-fc" || !detail.OverheadConfigured ||
		detail.PodOverheadCPU == nil || *detail.PodOverheadCPU != "250m" ||
		detail.PodOverheadMemory == nil || *detail.PodOverheadMemory != "120Mi" ||
		detail.OverheadResourceCount != 3 || !detail.SchedulingConfigured ||
		detail.NodeSelectorCount != 2 || detail.TolerationCount != 2 || detail.CreatedAt.IsZero() {
		t.Fatalf("RuntimeClass() = %#v", detail)
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal RuntimeClass detail: %v", err)
	}
	for _, forbidden := range []string{
		"vendor.example.com/private-device", "private.example.com/runtime", "zone-a", "private-taint",
		"other-taint", "private-value", "private-uid", "private-label", "private-annotation", "private-manager",
		"nodeSelector", "tolerations", "managedFields",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("RuntimeClass detail leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestDecodeRuntimeClassDefaultsOptionalConfiguration(t *testing.T) {
	t.Parallel()

	detail, err := decodeRuntimeClass(mustRuntimeClassJSON(t, runtimeClassPayload("runc", "runc")), "runc")
	if err != nil {
		t.Fatalf("decodeRuntimeClass() error = %v", err)
	}
	if detail.Handler != "runc" || detail.OverheadConfigured || detail.PodOverheadCPU != nil ||
		detail.PodOverheadMemory != nil || detail.OverheadResourceCount != 0 || detail.SchedulingConfigured ||
		detail.NodeSelectorCount != 0 || detail.TolerationCount != 0 {
		t.Fatalf("decodeRuntimeClass() = %#v", detail)
	}
}

func TestDecodeRuntimeClassRejectsInvalidUpstreamObjects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "api version", mutate: func(payload map[string]any) { payload["apiVersion"] = "node.k8s.io/v1beta1" }},
		{name: "kind", mutate: func(payload map[string]any) { payload["kind"] = "Pod" }},
		{name: "name", mutate: func(payload map[string]any) { payload["metadata"].(map[string]any)["name"] = "other" }},
		{name: "namespace", mutate: func(payload map[string]any) { payload["metadata"].(map[string]any)["namespace"] = "default" }},
		{name: "creation time", mutate: func(payload map[string]any) { payload["metadata"].(map[string]any)["creationTimestamp"] = nil }},
		{name: "empty handler", mutate: func(payload map[string]any) { payload["handler"] = "" }},
		{name: "invalid handler", mutate: func(payload map[string]any) { payload["handler"] = "Kata.Runtime" }},
		{name: "non-string quantity", mutate: func(payload map[string]any) {
			payload["overhead"] = map[string]any{"podFixed": map[string]any{"cpu": 250}}
		}},
		{name: "unsafe quantity", mutate: func(payload map[string]any) {
			payload["overhead"] = map[string]any{"podFixed": map[string]any{"cpu": "250m\nprivate"}}
		}},
		{name: "too many overhead resources", mutate: func(payload map[string]any) {
			resources := make(map[string]any, runtimeClassMaxOverheadResources+1)
			for index := 0; index <= runtimeClassMaxOverheadResources; index++ {
				resources["resource-"+string(rune('a'+index))] = "1"
			}
			payload["overhead"] = map[string]any{"podFixed": resources}
		}},
		{name: "non-string node selector", mutate: func(payload map[string]any) {
			payload["scheduling"] = map[string]any{"nodeSelector": map[string]any{"runtime": true}}
		}},
		{name: "too many node selectors", mutate: func(payload map[string]any) {
			selectors := make(map[string]any, runtimeClassMaxNodeSelectors+1)
			for index := 0; index <= runtimeClassMaxNodeSelectors; index++ {
				selectors["selector-"+string(rune(0x1000+index))] = "value"
			}
			payload["scheduling"] = map[string]any{"nodeSelector": selectors}
		}},
		{name: "too many tolerations", mutate: func(payload map[string]any) {
			tolerations := make([]any, runtimeClassMaxTolerations+1)
			for index := range tolerations {
				tolerations[index] = map[string]any{"key": "runtime"}
			}
			payload["scheduling"] = map[string]any{"tolerations": tolerations}
		}},
		{name: "non-object toleration", mutate: func(payload map[string]any) {
			payload["scheduling"] = map[string]any{"tolerations": []any{"runtime"}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			payload := runtimeClassPayload("kata-containers", "kata-fc")
			tt.mutate(payload)
			_, err := decodeRuntimeClass(mustRuntimeClassJSON(t, payload), "kata-containers")
			if !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("decodeRuntimeClass() error = %v, want upstream error", err)
			}
		})
	}
}

func TestClientRejectsInvalidRuntimeClassNameBeforeRequest(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	_, err := client.RuntimeClass(context.Background(), "../runtimeclasses")
	var validationErr *domain.ValidationError
	if !errors.As(err, &validationErr) || validationErr.Field != "name" {
		t.Fatalf("RuntimeClass() error = %v, want name validation error", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid name made %d upstream requests", calls.Load())
	}
}

func TestClientRejectsUnsafeRuntimeClassMetadataLists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload any
	}{
		{name: "wrong list type", payload: map[string]any{"apiVersion": "v1", "kind": "List", "items": []any{}}},
		{name: "namespaced item", payload: accessMetadataList("", []any{accessMetadata("runc", "default", "2026-07-30T09:00:00Z")})},
		{name: "invalid name", payload: accessMetadataList("", []any{accessMetadata("Kata", "", "2026-07-30T09:00:00Z")})},
		{name: "duplicate name", payload: accessMetadataList("", []any{
			accessMetadata("runc", "", "2026-07-30T09:00:00Z"),
			accessMetadata("runc", "", "2026-07-30T09:00:00Z"),
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
			if _, err := client.RuntimeClasses(context.Background()); !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("RuntimeClasses() error = %v, want upstream error", err)
			}
		})
	}
}

func TestClientRejectsRepeatedRuntimeClassContinuation(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, accessMetadataList("same-token", []any{}))
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)
	if _, err := client.RuntimeClasses(context.Background()); !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("RuntimeClasses() error = %v, want repeated continuation error", err)
	}
}

func TestClientBoundsRuntimeClassPagesItemsAndBodies(t *testing.T) {
	t.Run("pages", func(t *testing.T) {
		var calls atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			call := calls.Add(1)
			writeTestJSON(t, w, accessMetadataList("page-"+string(rune('a'+call)), []any{}))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.RuntimeClasses(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("RuntimeClasses() error = %v, want upstream page error", err)
		}
		if calls.Load() != runtimeClassMaxListPages {
			t.Fatalf("upstream calls = %d, want %d", calls.Load(), runtimeClassMaxListPages)
		}
	})

	t.Run("items", func(t *testing.T) {
		items := make([]any, runtimeClassMaxListItems+1)
		for index := range items {
			items[index] = accessMetadata("class-"+string(rune(0x1000+index)), "", "2026-07-30T09:00:00Z")
		}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, accessMetadataList("", items))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.RuntimeClasses(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("RuntimeClasses() error = %v, want upstream item error", err)
		}
	})

	t.Run("list bytes", func(t *testing.T) {
		metadata := accessMetadata("runc", "", "2026-07-30T09:00:00Z")
		metadata["metadata"].(map[string]any)["annotations"] = map[string]any{
			"private.example.com/padding": strings.Repeat("x", int(runtimeClassMaxListBytes)),
		}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, accessMetadataList("", []any{metadata}))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.RuntimeClasses(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("RuntimeClasses() error = %v, want upstream byte error", err)
		}
	})

	t.Run("detail bytes", func(t *testing.T) {
		payload := runtimeClassPayload("runc", "runc")
		payload["private"] = strings.Repeat("x", int(runtimeClassMaxDetailBytes))
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, payload)
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.RuntimeClass(context.Background(), "runc"); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("RuntimeClass() error = %v, want upstream byte error", err)
		}
	})
}

func runtimeClassPayload(name, handler string) map[string]any {
	return map[string]any{
		"apiVersion": "node.k8s.io/v1",
		"kind":       "RuntimeClass",
		"metadata": map[string]any{
			"name": name, "creationTimestamp": "2026-07-30T09:00:00Z",
		},
		"handler": handler,
	}
}

func mustRuntimeClassJSON(t *testing.T, payload any) []byte {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal RuntimeClass fixture: %v", err)
	}
	return encoded
}
