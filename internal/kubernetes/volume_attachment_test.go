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

func TestClientListsBoundedVolumeAttachmentsFromTable(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requests := make([]string, 0, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != volumeAttachmentCollectionPath {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Accept"); got != kubernetesTableAccept {
			t.Errorf("Accept = %q, want %q", got, kubernetesTableAccept)
		}
		if got := r.URL.Query().Get("includeObject"); got != "Metadata" {
			t.Errorf("includeObject = %q, want Metadata", got)
		}
		if got := r.URL.Query().Get("limit"); got != volumeAttachmentListPageSize {
			t.Errorf("limit = %q, want %q", got, volumeAttachmentListPageSize)
		}
		mu.Lock()
		requests = append(requests, r.URL.RequestURI())
		mu.Unlock()

		if r.URL.Query().Get("continue") == "page-two" {
			row := volumeAttachmentTableRow(
				[]any{"attach-c", "kubernetes.io/csi-migrated", "", "worker-03", true},
				"attach-c", "2026-07-31T08:02:00Z", "2026-07-31T08:03:00Z",
			)
			metadata := row["object"].(map[string]any)["metadata"].(map[string]any)
			metadata["labels"] = map[string]any{"private-storage-account": "private-value"}
			metadata["annotations"] = map[string]any{"private-node-id": "private-value"}
			writeTestJSON(t, w, tableResponse(volumeAttachmentTestColumns(), []any{row}))
			return
		}
		response := tableResponse(volumeAttachmentTestColumns(), []any{
			volumeAttachmentTableRow(
				[]any{"attach-b", "ebs.csi.example.com", "pv-data-b", "worker-02", false},
				"attach-b", "2026-07-31T08:01:00Z", "",
			),
			volumeAttachmentTableRow(
				[]any{"attach-a", "ebs.csi.example.com", "pv-data-a", "worker-01", true},
				"attach-a", "2026-07-31T08:00:00Z", "",
			),
		})
		response["metadata"] = map[string]any{"continue": "page-two"}
		writeTestJSON(t, w, response)
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	items, err := client.VolumeAttachments(context.Background())
	if err != nil {
		t.Fatalf("VolumeAttachments() error = %v", err)
	}
	if len(items) != 3 || items[0].Name != "attach-a" || items[1].Name != "attach-b" || items[2].Name != "attach-c" {
		t.Fatalf("VolumeAttachments() = %#v", items)
	}
	if items[0].Status != domain.VolumeAttachmentAttached || items[0].PersistentVolume != "pv-data-a" ||
		items[0].Node != "worker-01" || items[0].Attacher != "ebs.csi.example.com" || items[0].CreatedAt.IsZero() {
		t.Fatalf("attached item = %#v", items[0])
	}
	if items[1].Status != domain.VolumeAttachmentAttaching {
		t.Fatalf("attaching item = %#v", items[1])
	}
	if items[2].Status != domain.VolumeAttachmentDetaching || items[2].PersistentVolume != "" {
		t.Fatalf("detaching inline item = %#v", items[2])
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal VolumeAttachments: %v", err)
	}
	for _, forbidden := range []string{"private-storage-account", "private-node-id", "private-value", "deletionTimestamp"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("VolumeAttachments() leaked %q: %s", forbidden, encoded)
		}
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		volumeAttachmentCollectionPath + "?includeObject=Metadata&limit=250",
		volumeAttachmentCollectionPath + "?continue=page-two&includeObject=Metadata&limit=250",
	}
	if fmt.Sprint(gotRequests) != fmt.Sprint(wantRequests) {
		t.Fatalf("request URIs = %#v, want %#v", gotRequests, wantRequests)
	}
}

func TestClientRejectsUnsafeVolumeAttachmentTables(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "wrong api version", mutate: func(payload map[string]any) { payload["apiVersion"] = "storage.k8s.io/v1" }},
		{name: "wrong kind", mutate: func(payload map[string]any) { payload["kind"] = "VolumeAttachmentList" }},
		{name: "missing column", mutate: func(payload map[string]any) {
			payload["columnDefinitions"] = volumeAttachmentTestColumns()[:4]
		}},
		{name: "duplicate column", mutate: func(payload map[string]any) {
			payload["columnDefinitions"] = append(volumeAttachmentTestColumns(), map[string]any{"name": "Name", "type": "string"})
		}},
		{name: "wrong attached type", mutate: func(payload map[string]any) {
			payload["columnDefinitions"].([]any)[4].(map[string]any)["type"] = "string"
		}},
		{name: "name mismatch", mutate: func(payload map[string]any) {
			volumeAttachmentFirstRow(payload)["cells"].([]any)[0] = "other-attachment"
		}},
		{name: "namespaced metadata", mutate: func(payload map[string]any) {
			volumeAttachmentFirstMetadata(payload)["namespace"] = "default"
		}},
		{name: "invalid attachment name", mutate: func(payload map[string]any) {
			volumeAttachmentFirstMetadata(payload)["name"] = "Invalid_Name"
			volumeAttachmentFirstRow(payload)["cells"].([]any)[0] = "Invalid_Name"
		}},
		{name: "empty attacher", mutate: func(payload map[string]any) {
			volumeAttachmentFirstRow(payload)["cells"].([]any)[1] = ""
		}},
		{name: "unsafe attacher", mutate: func(payload map[string]any) {
			volumeAttachmentFirstRow(payload)["cells"].([]any)[1] = "private\nattacher"
		}},
		{name: "invalid persistent volume", mutate: func(payload map[string]any) {
			volumeAttachmentFirstRow(payload)["cells"].([]any)[2] = "../private-volume"
		}},
		{name: "invalid node", mutate: func(payload map[string]any) {
			volumeAttachmentFirstRow(payload)["cells"].([]any)[3] = "worker/01"
		}},
		{name: "non boolean attached", mutate: func(payload map[string]any) {
			volumeAttachmentFirstRow(payload)["cells"].([]any)[4] = "true"
		}},
		{name: "invalid deletion timestamp", mutate: func(payload map[string]any) {
			volumeAttachmentFirstMetadata(payload)["deletionTimestamp"] = "private-invalid-time"
		}},
		{name: "unsafe full object", mutate: func(payload map[string]any) {
			volumeAttachmentFirstRow(payload)["object"] = map[string]any{
				"apiVersion": "storage.k8s.io/v1", "kind": "VolumeAttachment",
				"metadata": map[string]any{"name": "attach-a"},
				"status":   map[string]any{"attachError": map[string]any{"message": "private-storage-error"}},
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			payload := singleVolumeAttachmentTable()
			tt.mutate(payload)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeTestJSON(t, w, payload)
			}))
			t.Cleanup(server.Close)
			client := newNetworkTestClient(t, server)

			_, err := client.VolumeAttachments(context.Background())
			if !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("VolumeAttachments() error = %v, want upstream error", err)
			}
			if strings.Contains(fmt.Sprint(err), "private") {
				t.Fatalf("VolumeAttachments() error leaked upstream content: %v", err)
			}
		})
	}
}

func TestClientRejectsDuplicateVolumeAttachmentsAndContinuationTokens(t *testing.T) {
	t.Parallel()

	t.Run("duplicate attachment", func(t *testing.T) {
		t.Parallel()
		payload := tableResponse(volumeAttachmentTestColumns(), []any{
			volumeAttachmentTableRow([]any{"attach-a", "csi.example.com", "pv-a", "worker-01", true}, "attach-a", "2026-07-31T08:00:00Z", ""),
			volumeAttachmentTableRow([]any{"attach-a", "csi.example.com", "pv-a", "worker-01", true}, "attach-a", "2026-07-31T08:00:00Z", ""),
		})
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeTestJSON(t, w, payload) }))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.VolumeAttachments(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("VolumeAttachments() error = %v, want duplicate error", err)
		}
	})

	t.Run("repeated continuation", func(t *testing.T) {
		t.Parallel()
		var calls atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			payload := tableResponse(volumeAttachmentTestColumns(), []any{})
			payload["metadata"] = map[string]any{"continue": "same-token"}
			writeTestJSON(t, w, payload)
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.VolumeAttachments(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("VolumeAttachments() error = %v, want continuation error", err)
		}
		if calls.Load() != 2 {
			t.Fatalf("upstream calls = %d, want 2", calls.Load())
		}
	})

	t.Run("unsafe continuation", func(t *testing.T) {
		t.Parallel()
		payload := tableResponse(volumeAttachmentTestColumns(), []any{})
		payload["metadata"] = map[string]any{"continue": "next\nprivate"}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeTestJSON(t, w, payload) }))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.VolumeAttachments(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("VolumeAttachments() error = %v, want continuation error", err)
		}
	})
}

func TestClientBoundsVolumeAttachmentPagesItemsAndBodies(t *testing.T) {
	t.Run("pages", func(t *testing.T) {
		var calls atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			call := calls.Add(1)
			payload := tableResponse(volumeAttachmentTestColumns(), []any{})
			payload["metadata"] = map[string]any{"continue": fmt.Sprintf("page-%d", call)}
			writeTestJSON(t, w, payload)
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.VolumeAttachments(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("VolumeAttachments() error = %v, want page error", err)
		}
		if calls.Load() != volumeAttachmentMaxListPages {
			t.Fatalf("upstream calls = %d, want %d", calls.Load(), volumeAttachmentMaxListPages)
		}
	})

	t.Run("items", func(t *testing.T) {
		rows := make([]any, volumeAttachmentMaxListItems+1)
		for index := range rows {
			name := fmt.Sprintf("attach-%04d", index)
			rows[index] = volumeAttachmentTableRow(
				[]any{name, "csi.example.com", fmt.Sprintf("pv-%04d", index), "worker-01", true},
				name, "2026-07-31T08:00:00Z", "",
			)
		}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, tableResponse(volumeAttachmentTestColumns(), rows))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.VolumeAttachments(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("VolumeAttachments() error = %v, want item error", err)
		}
	})

	t.Run("body", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", int(volumeAttachmentMaxPageBytes)+1)))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.VolumeAttachments(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("VolumeAttachments() error = %v, want body error", err)
		}
	})
}

func volumeAttachmentTestColumns() []any {
	return []any{
		map[string]any{"name": "Name", "type": "string"},
		map[string]any{"name": "Attacher", "type": "string"},
		map[string]any{"name": "PV", "type": "string"},
		map[string]any{"name": "Node", "type": "string"},
		map[string]any{"name": "Attached", "type": "boolean"},
		map[string]any{"name": "Age", "type": "string"},
	}
}

func volumeAttachmentTableRow(cells []any, name, createdAt, deletedAt string) map[string]any {
	row := tableRow(cells, name, "", createdAt)
	if deletedAt != "" {
		row["object"].(map[string]any)["metadata"].(map[string]any)["deletionTimestamp"] = deletedAt
	}
	return row
}

func singleVolumeAttachmentTable() map[string]any {
	return tableResponse(volumeAttachmentTestColumns(), []any{
		volumeAttachmentTableRow(
			[]any{"attach-a", "ebs.csi.example.com", "pv-data", "worker-01", true},
			"attach-a", "2026-07-31T08:00:00Z", "",
		),
	})
}

func volumeAttachmentFirstRow(payload map[string]any) map[string]any {
	return payload["rows"].([]any)[0].(map[string]any)
}

func volumeAttachmentFirstMetadata(payload map[string]any) map[string]any {
	return volumeAttachmentFirstRow(payload)["object"].(map[string]any)["metadata"].(map[string]any)
}
