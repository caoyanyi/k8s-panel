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

func TestClientListsBoundedVolumeAttributesClassesFromTable(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requests := make([]string, 0, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != volumeAttributesClassCollectionPath {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Accept"); got != kubernetesTableAccept {
			t.Errorf("Accept = %q, want %q", got, kubernetesTableAccept)
		}
		if got := r.URL.Query().Get("includeObject"); got != "Metadata" {
			t.Errorf("includeObject = %q, want Metadata", got)
		}
		if got := r.URL.Query().Get("limit"); got != volumeAttributesClassListPageSize {
			t.Errorf("limit = %q, want %q", got, volumeAttributesClassListPageSize)
		}
		mu.Lock()
		requests = append(requests, r.URL.RequestURI())
		mu.Unlock()

		if r.URL.Query().Get("continue") == "page-two" {
			row := volumeAttributesClassTableRow(
				[]any{"archive", "archive.csi.example.com", "2d"},
				"archive", "2026-07-31T08:02:00Z",
			)
			metadata := row["object"].(map[string]any)["metadata"].(map[string]any)
			metadata["labels"] = map[string]any{"private-storage-tier": "private-value"}
			metadata["annotations"] = map[string]any{"private-storage-account": "private-value"}
			writeTestJSON(t, w, tableResponse(volumeAttributesClassTestColumns(), []any{row}))
			return
		}
		response := tableResponse(volumeAttributesClassTestColumns(), []any{
			volumeAttributesClassTableRow(
				[]any{"gold", "ebs.csi.example.com", "1d"}, "gold", "2026-07-31T08:01:00Z",
			),
			volumeAttributesClassTableRow(
				[]any{"balanced", "pd.csi.example.com", "3d"}, "balanced", "2026-07-31T08:00:00Z",
			),
		})
		response["metadata"] = map[string]any{"continue": "page-two"}
		writeTestJSON(t, w, response)
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	items, err := client.VolumeAttributesClasses(context.Background())
	if err != nil {
		t.Fatalf("VolumeAttributesClasses() error = %v", err)
	}
	if len(items) != 3 || items[0].Name != "archive" || items[1].Name != "balanced" || items[2].Name != "gold" {
		t.Fatalf("VolumeAttributesClasses() = %#v", items)
	}
	if items[0].DriverName != "archive.csi.example.com" || items[0].CreatedAt.IsZero() ||
		items[2].DriverName != "ebs.csi.example.com" {
		t.Fatalf("VolumeAttributesClasses() values = %#v", items)
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal VolumeAttributesClasses: %v", err)
	}
	for _, forbidden := range []string{
		"private-storage-tier", "private-storage-account", "private-value", "parameters", "deletionTimestamp",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("VolumeAttributesClasses() leaked %q: %s", forbidden, encoded)
		}
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		volumeAttributesClassCollectionPath + "?includeObject=Metadata&limit=250",
		volumeAttributesClassCollectionPath + "?continue=page-two&includeObject=Metadata&limit=250",
	}
	if fmt.Sprint(gotRequests) != fmt.Sprint(wantRequests) {
		t.Fatalf("request URIs = %#v, want %#v", gotRequests, wantRequests)
	}
}

func TestClientRejectsUnsafeVolumeAttributesClassTables(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "wrong api version", mutate: func(payload map[string]any) { payload["apiVersion"] = "storage.k8s.io/v1" }},
		{name: "wrong kind", mutate: func(payload map[string]any) { payload["kind"] = "VolumeAttributesClassList" }},
		{name: "missing column", mutate: func(payload map[string]any) {
			payload["columnDefinitions"] = volumeAttributesClassTestColumns()[:1]
		}},
		{name: "duplicate column", mutate: func(payload map[string]any) {
			payload["columnDefinitions"] = append(
				volumeAttributesClassTestColumns(), map[string]any{"name": "DriverName", "type": "string"},
			)
		}},
		{name: "wrong driver type", mutate: func(payload map[string]any) {
			payload["columnDefinitions"].([]any)[1].(map[string]any)["type"] = "integer"
		}},
		{name: "name mismatch", mutate: func(payload map[string]any) {
			volumeAttributesClassFirstRow(payload)["cells"].([]any)[0] = "silver"
		}},
		{name: "namespaced metadata", mutate: func(payload map[string]any) {
			volumeAttributesClassFirstMetadata(payload)["namespace"] = "default"
		}},
		{name: "invalid class name", mutate: func(payload map[string]any) {
			volumeAttributesClassFirstMetadata(payload)["name"] = "Invalid_Name"
			volumeAttributesClassFirstRow(payload)["cells"].([]any)[0] = "Invalid_Name"
		}},
		{name: "empty driver", mutate: func(payload map[string]any) {
			volumeAttributesClassFirstRow(payload)["cells"].([]any)[1] = ""
		}},
		{name: "invalid driver", mutate: func(payload map[string]any) {
			volumeAttributesClassFirstRow(payload)["cells"].([]any)[1] = "private/driver"
		}},
		{name: "invalid creation timestamp", mutate: func(payload map[string]any) {
			volumeAttributesClassFirstMetadata(payload)["creationTimestamp"] = "private-invalid-time"
		}},
		{name: "unsafe full object", mutate: func(payload map[string]any) {
			volumeAttributesClassFirstRow(payload)["object"] = map[string]any{
				"apiVersion": "storage.k8s.io/v1", "kind": "VolumeAttributesClass",
				"metadata":   map[string]any{"name": "gold"},
				"driverName": "ebs.csi.example.com",
				"parameters": map[string]any{"private-iops": "private-value"},
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			payload := singleVolumeAttributesClassTable()
			tt.mutate(payload)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeTestJSON(t, w, payload)
			}))
			t.Cleanup(server.Close)
			client := newNetworkTestClient(t, server)

			_, err := client.VolumeAttributesClasses(context.Background())
			if !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("VolumeAttributesClasses() error = %v, want upstream error", err)
			}
			if strings.Contains(fmt.Sprint(err), "private") {
				t.Fatalf("VolumeAttributesClasses() error leaked upstream content: %v", err)
			}
		})
	}
}

func TestClientRejectsDuplicateVolumeAttributesClassesAndContinuationTokens(t *testing.T) {
	t.Parallel()

	t.Run("duplicate class", func(t *testing.T) {
		t.Parallel()
		payload := tableResponse(volumeAttributesClassTestColumns(), []any{
			volumeAttributesClassTableRow([]any{"gold", "csi.example.com", "1d"}, "gold", "2026-07-31T08:00:00Z"),
			volumeAttributesClassTableRow([]any{"gold", "csi.example.com", "1d"}, "gold", "2026-07-31T08:00:00Z"),
		})
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, payload)
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.VolumeAttributesClasses(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("VolumeAttributesClasses() error = %v, want duplicate error", err)
		}
	})

	t.Run("repeated continuation", func(t *testing.T) {
		t.Parallel()
		var calls atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			payload := tableResponse(volumeAttributesClassTestColumns(), []any{})
			payload["metadata"] = map[string]any{"continue": "same-token"}
			writeTestJSON(t, w, payload)
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.VolumeAttributesClasses(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("VolumeAttributesClasses() error = %v, want continuation error", err)
		}
		if calls.Load() != 2 {
			t.Fatalf("upstream calls = %d, want 2", calls.Load())
		}
	})

	t.Run("unsafe continuation", func(t *testing.T) {
		t.Parallel()
		payload := tableResponse(volumeAttributesClassTestColumns(), []any{})
		payload["metadata"] = map[string]any{"continue": "next\nprivate"}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, payload)
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.VolumeAttributesClasses(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("VolumeAttributesClasses() error = %v, want continuation error", err)
		}
	})
}

func TestClientBoundsVolumeAttributesClassPagesItemsAndBodies(t *testing.T) {
	t.Run("pages", func(t *testing.T) {
		var calls atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			call := calls.Add(1)
			payload := tableResponse(volumeAttributesClassTestColumns(), []any{})
			payload["metadata"] = map[string]any{"continue": fmt.Sprintf("page-%d", call)}
			writeTestJSON(t, w, payload)
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.VolumeAttributesClasses(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("VolumeAttributesClasses() error = %v, want page error", err)
		}
		if calls.Load() != volumeAttributesClassMaxListPages {
			t.Fatalf("upstream calls = %d, want %d", calls.Load(), volumeAttributesClassMaxListPages)
		}
	})

	t.Run("items", func(t *testing.T) {
		rows := make([]any, volumeAttributesClassMaxListItems+1)
		for index := range rows {
			name := fmt.Sprintf("attributes-%04d", index)
			rows[index] = volumeAttributesClassTableRow(
				[]any{name, "csi.example.com", "1d"}, name, "2026-07-31T08:00:00Z",
			)
		}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, tableResponse(volumeAttributesClassTestColumns(), rows))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.VolumeAttributesClasses(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("VolumeAttributesClasses() error = %v, want item error", err)
		}
	})

	t.Run("body", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", int(volumeAttributesClassMaxPageBytes)+1)))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.VolumeAttributesClasses(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("VolumeAttributesClasses() error = %v, want body error", err)
		}
	})
}

func volumeAttributesClassTestColumns() []any {
	return []any{
		map[string]any{"name": "Name", "type": "string"},
		map[string]any{"name": "DriverName", "type": "string"},
		map[string]any{"name": "Age", "type": "string"},
	}
}

func volumeAttributesClassTableRow(cells []any, name, createdAt string) map[string]any {
	return tableRow(cells, name, "", createdAt)
}

func singleVolumeAttributesClassTable() map[string]any {
	return tableResponse(volumeAttributesClassTestColumns(), []any{
		volumeAttributesClassTableRow(
			[]any{"gold", "ebs.csi.example.com", "1d"}, "gold", "2026-07-31T08:00:00Z",
		),
	})
}

func volumeAttributesClassFirstRow(payload map[string]any) map[string]any {
	return payload["rows"].([]any)[0].(map[string]any)
}

func volumeAttributesClassFirstMetadata(payload map[string]any) map[string]any {
	return volumeAttributesClassFirstRow(payload)["object"].(map[string]any)["metadata"].(map[string]any)
}
