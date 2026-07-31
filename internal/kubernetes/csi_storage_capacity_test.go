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

func TestClientListsBoundedCSIStorageCapacitiesFromTable(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requests := make([]string, 0, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != csiStorageCapacityCollectionPath {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Accept"); got != kubernetesTableAccept {
			t.Errorf("Accept = %q, want %q", got, kubernetesTableAccept)
		}
		if got := r.URL.Query().Get("includeObject"); got != "Metadata" {
			t.Errorf("includeObject = %q, want Metadata", got)
		}
		if got := r.URL.Query().Get("limit"); got != csiStorageCapacityListPageSize {
			t.Errorf("limit = %q, want %q", got, csiStorageCapacityListPageSize)
		}
		mu.Lock()
		requests = append(requests, r.URL.RequestURI())
		mu.Unlock()

		if r.URL.Query().Get("continue") == "page-two" {
			row := csiStorageCapacityTableRow(
				[]any{"capacity-c", "standard", "0"}, "payments", "capacity-c", "2026-07-31T08:02:00Z",
			)
			metadata := row["object"].(map[string]any)["metadata"].(map[string]any)
			metadata["labels"] = map[string]any{"topology.example.com/zone": "private-zone"}
			metadata["annotations"] = map[string]any{"private-storage-account": "private-value"}
			writeTestJSON(t, w, tableResponse(csiStorageCapacityTestColumns(), []any{row}))
			return
		}
		response := tableResponse(csiStorageCapacityTestColumns(), []any{
			csiStorageCapacityTableRow(
				[]any{"capacity-b", "fast", "120Gi"}, "storage-system", "capacity-b", "2026-07-31T08:01:00Z",
			),
			csiStorageCapacityTableRow(
				[]any{"capacity-a", "archive", "<unset>"}, "storage-system", "capacity-a", "2026-07-31T08:00:00Z",
			),
		})
		response["metadata"] = map[string]any{"continue": "page-two"}
		writeTestJSON(t, w, response)
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	items, err := client.CSIStorageCapacities(context.Background(), "")
	if err != nil {
		t.Fatalf("CSIStorageCapacities() error = %v", err)
	}
	if len(items) != 3 || items[0].Name != "capacity-c" || items[1].Name != "capacity-a" || items[2].Name != "capacity-b" {
		t.Fatalf("CSIStorageCapacities() = %#v", items)
	}
	if items[0].Namespace != "payments" || items[0].StorageClass != "standard" || items[0].Capacity != "0" || items[0].CreatedAt.IsZero() {
		t.Fatalf("zero capacity item = %#v", items[0])
	}
	if items[1].Capacity != "" || items[1].StorageClass != "archive" {
		t.Fatalf("unset capacity item = %#v", items[1])
	}
	if items[2].Capacity != "120Gi" || items[2].StorageClass != "fast" {
		t.Fatalf("reported capacity item = %#v", items[2])
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal CSIStorageCapacities: %v", err)
	}
	for _, forbidden := range []string{"private-zone", "private-storage-account", "private-value", "nodeTopology", "maximumVolumeSize"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("CSIStorageCapacities() leaked %q: %s", forbidden, encoded)
		}
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		csiStorageCapacityCollectionPath + "?includeObject=Metadata&limit=250",
		csiStorageCapacityCollectionPath + "?continue=page-two&includeObject=Metadata&limit=250",
	}
	if fmt.Sprint(gotRequests) != fmt.Sprint(wantRequests) {
		t.Fatalf("request URIs = %#v, want %#v", gotRequests, wantRequests)
	}
}

func TestClientScopesCSIStorageCapacitiesToValidatedNamespace(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		wantPath := "/apis/storage.k8s.io/v1/namespaces/storage-system/csistoragecapacities"
		if r.URL.Path != wantPath {
			t.Errorf("path = %q, want %q", r.URL.Path, wantPath)
		}
		writeTestJSON(t, w, tableResponse(csiStorageCapacityTestColumns(), []any{
			csiStorageCapacityTableRow(
				[]any{"capacity-a", "standard", "80Gi"}, "storage-system", "capacity-a", "2026-07-31T08:00:00Z",
			),
		}))
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	items, err := client.CSIStorageCapacities(context.Background(), "storage-system")
	if err != nil || len(items) != 1 || items[0].Namespace != "storage-system" {
		t.Fatalf("CSIStorageCapacities() = %#v, %v", items, err)
	}
	if _, err := client.CSIStorageCapacities(context.Background(), "bad/namespace"); err == nil {
		t.Fatal("CSIStorageCapacities() accepted an invalid namespace")
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream calls = %d, want 1", calls.Load())
	}
}

func TestClientRejectsUnsafeCSIStorageCapacityTables(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "wrong api version", mutate: func(payload map[string]any) { payload["apiVersion"] = "storage.k8s.io/v1" }},
		{name: "wrong kind", mutate: func(payload map[string]any) { payload["kind"] = "CSIStorageCapacityList" }},
		{name: "missing column", mutate: func(payload map[string]any) {
			payload["columnDefinitions"] = csiStorageCapacityTestColumns()[:2]
		}},
		{name: "duplicate column", mutate: func(payload map[string]any) {
			payload["columnDefinitions"] = append(csiStorageCapacityTestColumns(), map[string]any{"name": "Capacity", "type": "string"})
		}},
		{name: "wrong capacity type", mutate: func(payload map[string]any) {
			payload["columnDefinitions"].([]any)[2].(map[string]any)["type"] = "integer"
		}},
		{name: "name mismatch", mutate: func(payload map[string]any) {
			csiStorageCapacityFirstRow(payload)["cells"].([]any)[0] = "other-capacity"
		}},
		{name: "invalid object name", mutate: func(payload map[string]any) {
			csiStorageCapacityFirstMetadata(payload)["name"] = "Invalid_Name"
			csiStorageCapacityFirstRow(payload)["cells"].([]any)[0] = "Invalid_Name"
		}},
		{name: "missing namespace", mutate: func(payload map[string]any) {
			delete(csiStorageCapacityFirstMetadata(payload), "namespace")
		}},
		{name: "invalid storage class", mutate: func(payload map[string]any) {
			csiStorageCapacityFirstRow(payload)["cells"].([]any)[1] = "../private-class"
		}},
		{name: "invalid quantity", mutate: func(payload map[string]any) {
			csiStorageCapacityFirstRow(payload)["cells"].([]any)[2] = "private capacity"
		}},
		{name: "negative quantity", mutate: func(payload map[string]any) {
			csiStorageCapacityFirstRow(payload)["cells"].([]any)[2] = "-1Gi"
		}},
		{name: "oversized quantity", mutate: func(payload map[string]any) {
			csiStorageCapacityFirstRow(payload)["cells"].([]any)[2] = strings.Repeat("1", csiStorageCapacityMaxQuantityBytes+1)
		}},
		{name: "unsafe full object", mutate: func(payload map[string]any) {
			csiStorageCapacityFirstRow(payload)["object"] = map[string]any{
				"apiVersion": "storage.k8s.io/v1", "kind": "CSIStorageCapacity",
				"metadata":     map[string]any{"namespace": "storage-system", "name": "capacity-a"},
				"nodeTopology": map[string]any{"matchLabels": map[string]any{"zone": "private-zone"}},
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			payload := singleCSIStorageCapacityTable()
			tt.mutate(payload)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeTestJSON(t, w, payload)
			}))
			t.Cleanup(server.Close)
			client := newNetworkTestClient(t, server)

			_, err := client.CSIStorageCapacities(context.Background(), "")
			if !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("CSIStorageCapacities() error = %v, want upstream error", err)
			}
			if strings.Contains(fmt.Sprint(err), "private") {
				t.Fatalf("CSIStorageCapacities() error leaked upstream content: %v", err)
			}
		})
	}
}

func TestClientRejectsCSIStorageCapacityNamespaceEscapeAndDuplicates(t *testing.T) {
	t.Parallel()

	t.Run("namespace escape", func(t *testing.T) {
		t.Parallel()
		payload := singleCSIStorageCapacityTable()
		csiStorageCapacityFirstMetadata(payload)["namespace"] = "other-system"
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeTestJSON(t, w, payload) }))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.CSIStorageCapacities(context.Background(), "storage-system"); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CSIStorageCapacities() error = %v, want namespace error", err)
		}
	})

	t.Run("duplicate identity", func(t *testing.T) {
		t.Parallel()
		row := csiStorageCapacityTableRow(
			[]any{"capacity-a", "standard", "80Gi"}, "storage-system", "capacity-a", "2026-07-31T08:00:00Z",
		)
		payload := tableResponse(csiStorageCapacityTestColumns(), []any{row, row})
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeTestJSON(t, w, payload) }))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.CSIStorageCapacities(context.Background(), ""); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CSIStorageCapacities() error = %v, want duplicate error", err)
		}
	})
}

func TestClientRejectsUnsafeCSIStorageCapacityContinuationTokens(t *testing.T) {
	t.Parallel()

	t.Run("repeated continuation", func(t *testing.T) {
		t.Parallel()
		var calls atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			payload := tableResponse(csiStorageCapacityTestColumns(), []any{})
			payload["metadata"] = map[string]any{"continue": "same-token"}
			writeTestJSON(t, w, payload)
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.CSIStorageCapacities(context.Background(), ""); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CSIStorageCapacities() error = %v, want continuation error", err)
		}
		if calls.Load() != 2 {
			t.Fatalf("upstream calls = %d, want 2", calls.Load())
		}
	})

	t.Run("control character", func(t *testing.T) {
		t.Parallel()
		payload := tableResponse(csiStorageCapacityTestColumns(), []any{})
		payload["metadata"] = map[string]any{"continue": "next\nprivate"}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeTestJSON(t, w, payload) }))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.CSIStorageCapacities(context.Background(), ""); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CSIStorageCapacities() error = %v, want continuation error", err)
		}
	})
}

func TestClientBoundsCSIStorageCapacityPagesItemsAndBodies(t *testing.T) {
	t.Run("pages", func(t *testing.T) {
		var calls atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			call := calls.Add(1)
			payload := tableResponse(csiStorageCapacityTestColumns(), []any{})
			payload["metadata"] = map[string]any{"continue": fmt.Sprintf("page-%d", call)}
			writeTestJSON(t, w, payload)
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.CSIStorageCapacities(context.Background(), ""); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CSIStorageCapacities() error = %v, want page error", err)
		}
		if calls.Load() != csiStorageCapacityMaxListPages {
			t.Fatalf("upstream calls = %d, want %d", calls.Load(), csiStorageCapacityMaxListPages)
		}
	})

	t.Run("items", func(t *testing.T) {
		rows := make([]any, csiStorageCapacityMaxListItems+1)
		for index := range rows {
			name := fmt.Sprintf("capacity-%04d", index)
			rows[index] = csiStorageCapacityTableRow(
				[]any{name, "standard", "1Gi"}, "storage-system", name, "2026-07-31T08:00:00Z",
			)
		}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, tableResponse(csiStorageCapacityTestColumns(), rows))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.CSIStorageCapacities(context.Background(), ""); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CSIStorageCapacities() error = %v, want item error", err)
		}
	})

	t.Run("body", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", int(csiStorageCapacityMaxPageBytes)+1)))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.CSIStorageCapacities(context.Background(), ""); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CSIStorageCapacities() error = %v, want body error", err)
		}
	})
}

func csiStorageCapacityTestColumns() []any {
	return []any{
		map[string]any{"name": "Name", "type": "string"},
		map[string]any{"name": "StorageClassName", "type": "string"},
		map[string]any{"name": "Capacity", "type": "string"},
	}
}

func csiStorageCapacityTableRow(cells []any, namespace, name, createdAt string) map[string]any {
	return tableRow(cells, name, namespace, createdAt)
}

func singleCSIStorageCapacityTable() map[string]any {
	return tableResponse(csiStorageCapacityTestColumns(), []any{
		csiStorageCapacityTableRow(
			[]any{"capacity-a", "standard", "80Gi"}, "storage-system", "capacity-a", "2026-07-31T08:00:00Z",
		),
	})
}

func csiStorageCapacityFirstRow(payload map[string]any) map[string]any {
	return payload["rows"].([]any)[0].(map[string]any)
}

func csiStorageCapacityFirstMetadata(payload map[string]any) map[string]any {
	return csiStorageCapacityFirstRow(payload)["object"].(map[string]any)["metadata"].(map[string]any)
}
