package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

func TestClientListsCSIDriversWithMetadataOnlyPagination(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requests := make([]string, 0, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != csiDriverCollectionPath {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Accept"); got != kubernetesPartialMetadataListAccept {
			t.Errorf("Accept = %q, want %q", got, kubernetesPartialMetadataListAccept)
		}
		if got := r.URL.Query().Get("limit"); got != csiDriverListPageSize {
			t.Errorf("limit = %q, want %q", got, csiDriverListPageSize)
		}
		mu.Lock()
		requests = append(requests, r.URL.RequestURI())
		mu.Unlock()

		if r.URL.Query().Get("continue") == "page-two" {
			writeTestJSON(t, w, accessMetadataList("", []any{
				accessMetadata("zfs.csi.example.com", "", "2026-07-29T09:00:00Z"),
			}))
			return
		}
		writeTestJSON(t, w, accessMetadataList("page-two", []any{
			accessMetadata("ebs.csi.example.com", "", "2026-07-30T09:00:00Z"),
		}))
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	items, err := client.CSIDrivers(context.Background())
	if err != nil {
		t.Fatalf("CSIDrivers() error = %v", err)
	}
	if len(items) != 2 || items[0].Name != "ebs.csi.example.com" || items[1].Name != "zfs.csi.example.com" ||
		items[0].CreatedAt.IsZero() || items[1].CreatedAt.IsZero() {
		t.Fatalf("CSIDrivers() = %#v", items)
	}
	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		csiDriverCollectionPath + "?limit=250",
		csiDriverCollectionPath + "?continue=page-two&limit=250",
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

func TestClientReadsRedactedCSIDriverDetail(t *testing.T) {
	t.Parallel()

	const name = "ebs.csi.example.com"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != csiDriverCollectionPath+"/"+name {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		payload := csiDriverPayload(name)
		spec := payload["spec"].(map[string]any)
		spec["attachRequired"] = false
		spec["podInfoOnMount"] = true
		spec["storageCapacity"] = true
		spec["requiresRepublish"] = true
		spec["seLinuxMount"] = true
		spec["fsGroupPolicy"] = "File"
		spec["volumeLifecycleModes"] = []any{"Ephemeral", "Persistent"}
		spec["tokenRequests"] = []any{
			map[string]any{"audience": "private-storage-api", "expirationSeconds": 3600},
			map[string]any{"audience": "private-backup-api", "expirationSeconds": 7200},
		}
		spec["nodeAllocatableUpdatePeriodSeconds"] = 10
		metadata := payload["metadata"].(map[string]any)
		metadata["uid"] = "private-uid"
		metadata["labels"] = map[string]any{"private-label": "private-value"}
		metadata["annotations"] = map[string]any{"private-annotation": "private-value"}
		metadata["managedFields"] = []any{map[string]any{"manager": "private-manager"}}
		writeTestJSON(t, w, payload)
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	detail, err := client.CSIDriver(context.Background(), name)
	if err != nil {
		t.Fatalf("CSIDriver() error = %v", err)
	}
	if detail.Name != name || detail.AttachRequired || !detail.PodInfoOnMount || !detail.StorageCapacity ||
		!detail.RequiresRepublish || !detail.SELinuxMount || detail.FSGroupPolicy != domain.CSIFSGroupPolicyFile ||
		len(detail.VolumeLifecycleModes) != 2 ||
		detail.VolumeLifecycleModes[0] != domain.CSIVolumeLifecyclePersistent ||
		detail.VolumeLifecycleModes[1] != domain.CSIVolumeLifecycleEphemeral ||
		detail.TokenRequestCount != 2 || detail.CreatedAt.IsZero() {
		t.Fatalf("CSIDriver() = %#v", detail)
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal CSIDriver detail: %v", err)
	}
	for _, forbidden := range []string{
		"private-storage-api", "private-backup-api", "expirationSeconds", "nodeAllocatableUpdatePeriodSeconds",
		"private-uid", "private-label", "private-annotation", "private-value", "private-manager", "managedFields",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("CSIDriver detail leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestDecodeCSIDriverDefaultsOptionalSELinuxMount(t *testing.T) {
	t.Parallel()

	detail, err := decodeCSIDriver(mustCSIDriverJSON(t, csiDriverPayload("local.csi.example.com")), "local.csi.example.com")
	if err != nil {
		t.Fatalf("decodeCSIDriver() error = %v", err)
	}
	if !detail.AttachRequired || detail.PodInfoOnMount || detail.StorageCapacity || detail.RequiresRepublish ||
		detail.SELinuxMount || detail.FSGroupPolicy != domain.CSIFSGroupPolicyReadWriteOnceWithFSType ||
		len(detail.VolumeLifecycleModes) != 1 || detail.VolumeLifecycleModes[0] != domain.CSIVolumeLifecyclePersistent ||
		detail.TokenRequestCount != 0 {
		t.Fatalf("decodeCSIDriver() = %#v", detail)
	}
}

func TestDecodeCSIDriverRejectsInvalidUpstreamObjects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "api version", mutate: func(payload map[string]any) { payload["apiVersion"] = "storage.k8s.io/v1beta1" }},
		{name: "kind", mutate: func(payload map[string]any) { payload["kind"] = "StorageClass" }},
		{name: "name", mutate: func(payload map[string]any) { payload["metadata"].(map[string]any)["name"] = "other.csi.example.com" }},
		{name: "namespace", mutate: func(payload map[string]any) { payload["metadata"].(map[string]any)["namespace"] = "default" }},
		{name: "creation time", mutate: func(payload map[string]any) { payload["metadata"].(map[string]any)["creationTimestamp"] = nil }},
		{name: "invalid name", mutate: func(payload map[string]any) { payload["metadata"].(map[string]any)["name"] = "CSI.example.com" }},
		{name: "missing attach required", mutate: func(payload map[string]any) { delete(payload["spec"].(map[string]any), "attachRequired") }},
		{name: "missing pod info", mutate: func(payload map[string]any) { delete(payload["spec"].(map[string]any), "podInfoOnMount") }},
		{name: "missing storage capacity", mutate: func(payload map[string]any) { delete(payload["spec"].(map[string]any), "storageCapacity") }},
		{name: "missing requires republish", mutate: func(payload map[string]any) { delete(payload["spec"].(map[string]any), "requiresRepublish") }},
		{name: "missing fs group policy", mutate: func(payload map[string]any) { delete(payload["spec"].(map[string]any), "fsGroupPolicy") }},
		{name: "invalid fs group policy", mutate: func(payload map[string]any) { payload["spec"].(map[string]any)["fsGroupPolicy"] = "Recursive" }},
		{name: "missing lifecycle modes", mutate: func(payload map[string]any) { delete(payload["spec"].(map[string]any), "volumeLifecycleModes") }},
		{name: "duplicate lifecycle mode", mutate: func(payload map[string]any) {
			payload["spec"].(map[string]any)["volumeLifecycleModes"] = []any{"Persistent", "Persistent"}
		}},
		{name: "unsupported lifecycle mode", mutate: func(payload map[string]any) {
			payload["spec"].(map[string]any)["volumeLifecycleModes"] = []any{"Snapshot"}
		}},
		{name: "too many lifecycle modes", mutate: func(payload map[string]any) {
			payload["spec"].(map[string]any)["volumeLifecycleModes"] = []any{"Persistent", "Ephemeral", "Persistent"}
		}},
		{name: "too many token requests", mutate: func(payload map[string]any) {
			requests := make([]any, csiDriverMaxTokenRequests+1)
			for index := range requests {
				requests[index] = map[string]any{"audience": fmt.Sprintf("private-%d", index)}
			}
			payload["spec"].(map[string]any)["tokenRequests"] = requests
		}},
		{name: "non-object token request", mutate: func(payload map[string]any) {
			payload["spec"].(map[string]any)["tokenRequests"] = []any{"private-audience"}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			payload := csiDriverPayload("ebs.csi.example.com")
			tt.mutate(payload)
			_, err := decodeCSIDriver(mustCSIDriverJSON(t, payload), "ebs.csi.example.com")
			if !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("decodeCSIDriver() error = %v, want upstream error", err)
			}
		})
	}

	if _, err := decodeCSIDriver([]byte("{"), "ebs.csi.example.com"); !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("decodeCSIDriver() malformed JSON error = %v, want upstream error", err)
	}
}

func TestClientRejectsInvalidCSIDriverNameBeforeRequest(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	_, err := client.CSIDriver(context.Background(), "../csidrivers")
	var validationErr *domain.ValidationError
	if !errors.As(err, &validationErr) || validationErr.Field != "name" {
		t.Fatalf("CSIDriver() error = %v, want name validation error", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid name made %d upstream requests", calls.Load())
	}
}

func TestClientRejectsUnsafeCSIDriverMetadataLists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload any
	}{
		{name: "wrong list type", payload: map[string]any{"apiVersion": "v1", "kind": "List", "items": []any{}}},
		{name: "namespaced item", payload: accessMetadataList("", []any{accessMetadata("csi.example.com", "default", "2026-07-30T09:00:00Z")})},
		{name: "invalid name", payload: accessMetadataList("", []any{accessMetadata("CSI.example.com", "", "2026-07-30T09:00:00Z")})},
		{name: "duplicate name", payload: accessMetadataList("", []any{
			accessMetadata("csi.example.com", "", "2026-07-30T09:00:00Z"),
			accessMetadata("csi.example.com", "", "2026-07-30T09:00:00Z"),
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
			if _, err := client.CSIDrivers(context.Background()); !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("CSIDrivers() error = %v, want upstream error", err)
			}
		})
	}
}

func TestClientRejectsRepeatedCSIDriverContinuation(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, accessMetadataList("same-token", []any{}))
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)
	if _, err := client.CSIDrivers(context.Background()); !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("CSIDrivers() error = %v, want repeated continuation error", err)
	}
}

func TestClientBoundsCSIDriverPagesItemsAndBodies(t *testing.T) {
	t.Run("pages", func(t *testing.T) {
		var calls atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			call := calls.Add(1)
			writeTestJSON(t, w, accessMetadataList(fmt.Sprintf("page-%d", call), []any{}))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.CSIDrivers(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CSIDrivers() error = %v, want upstream page error", err)
		}
		if calls.Load() != csiDriverMaxListPages {
			t.Fatalf("upstream calls = %d, want %d", calls.Load(), csiDriverMaxListPages)
		}
	})

	t.Run("items", func(t *testing.T) {
		items := make([]any, csiDriverMaxListItems+1)
		for index := range items {
			items[index] = accessMetadata(fmt.Sprintf("driver-%04d.example.com", index), "", "2026-07-30T09:00:00Z")
		}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, accessMetadataList("", items))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.CSIDrivers(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CSIDrivers() error = %v, want upstream item error", err)
		}
	})

	t.Run("list bytes", func(t *testing.T) {
		metadata := accessMetadata("csi.example.com", "", "2026-07-30T09:00:00Z")
		metadata["metadata"].(map[string]any)["annotations"] = map[string]any{
			"private.example.com/padding": strings.Repeat("x", int(csiDriverMaxListBytes)),
		}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, accessMetadataList("", []any{metadata}))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.CSIDrivers(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CSIDrivers() error = %v, want upstream byte error", err)
		}
	})

	t.Run("detail bytes", func(t *testing.T) {
		payload := csiDriverPayload("csi.example.com")
		payload["private"] = strings.Repeat("x", int(csiDriverMaxDetailBytes))
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, payload)
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.CSIDriver(context.Background(), "csi.example.com"); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CSIDriver() error = %v, want upstream byte error", err)
		}
	})
}

func csiDriverPayload(name string) map[string]any {
	return map[string]any{
		"apiVersion": "storage.k8s.io/v1",
		"kind":       "CSIDriver",
		"metadata": map[string]any{
			"name": name, "creationTimestamp": "2026-07-30T09:00:00Z",
		},
		"spec": map[string]any{
			"attachRequired":       true,
			"podInfoOnMount":       false,
			"storageCapacity":      false,
			"requiresRepublish":    false,
			"fsGroupPolicy":        "ReadWriteOnceWithFSType",
			"volumeLifecycleModes": []any{"Persistent"},
		},
	}
}

func mustCSIDriverJSON(t *testing.T, payload any) []byte {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal CSIDriver fixture: %v", err)
	}
	return encoded
}
