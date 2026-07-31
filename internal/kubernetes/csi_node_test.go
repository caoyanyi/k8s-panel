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

func TestClientListsBoundedCSINodesFromTable(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requests := make([]string, 0, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != csiNodeCollectionPath {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Accept"); got != kubernetesTableAccept {
			t.Errorf("Accept = %q, want %q", got, kubernetesTableAccept)
		}
		if got := r.URL.Query().Get("includeObject"); got != "Metadata" {
			t.Errorf("includeObject = %q, want Metadata", got)
		}
		if got := r.URL.Query().Get("limit"); got != csiNodeListPageSize {
			t.Errorf("limit = %q, want %q", got, csiNodeListPageSize)
		}
		mu.Lock()
		requests = append(requests, r.URL.RequestURI())
		mu.Unlock()

		if r.URL.Query().Get("continue") == "page-two" {
			row := csiNodeTableRow("worker-03", 0, "2026-07-31T08:02:00Z")
			metadata := row["object"].(map[string]any)["metadata"].(map[string]any)
			metadata["labels"] = map[string]any{"private-topology": "private-value"}
			metadata["annotations"] = map[string]any{"private-node-id": "storage-node-03"}
			writeTestJSON(t, w, tableResponse(csiNodeTestColumns(), []any{row}))
			return
		}
		response := tableResponse(csiNodeTestColumns(), []any{
			csiNodeTableRow("worker-02", 1, "2026-07-31T08:01:00Z"),
			csiNodeTableRow("worker-01", 2, "2026-07-31T08:00:00Z"),
		})
		response["metadata"] = map[string]any{"continue": "page-two"}
		writeTestJSON(t, w, response)
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	items, err := client.CSINodes(context.Background())
	if err != nil {
		t.Fatalf("CSINodes() error = %v", err)
	}
	if len(items) != 3 || items[0].Name != "worker-01" || items[0].DriverCount != 2 ||
		items[1].Name != "worker-02" || items[1].DriverCount != 1 ||
		items[2].Name != "worker-03" || items[2].DriverCount != 0 || items[0].CreatedAt.IsZero() {
		t.Fatalf("CSINodes() = %#v", items)
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal CSINodes: %v", err)
	}
	for _, forbidden := range []string{"private-topology", "private-node-id", "private-value", "storage-node-03"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("CSINodes() leaked %q: %s", forbidden, encoded)
		}
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		csiNodeCollectionPath + "?includeObject=Metadata&limit=250",
		csiNodeCollectionPath + "?continue=page-two&includeObject=Metadata&limit=250",
	}
	if fmt.Sprint(gotRequests) != fmt.Sprint(wantRequests) {
		t.Fatalf("request URIs = %#v, want %#v", gotRequests, wantRequests)
	}
}

func TestClientReadsRedactedCSINodeDetail(t *testing.T) {
	t.Parallel()

	const name = "worker-01"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != csiNodeCollectionPath+"/"+name {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		payload := csiNodePayload(name)
		metadata := payload["metadata"].(map[string]any)
		metadata["uid"] = "private-uid"
		metadata["labels"] = map[string]any{"private-label": "private-value"}
		metadata["annotations"] = map[string]any{"private-annotation": "private-value"}
		metadata["managedFields"] = []any{map[string]any{"manager": "private-manager"}}
		writeTestJSON(t, w, payload)
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	detail, err := client.CSINode(context.Background(), name)
	if err != nil {
		t.Fatalf("CSINode() error = %v", err)
	}
	if detail.Name != name || detail.DriverCount != 2 || detail.CreatedAt.IsZero() || len(detail.Drivers) != 2 {
		t.Fatalf("CSINode() = %#v", detail)
	}
	if detail.Drivers[0].Name != "ebs.csi.example.com" || detail.Drivers[0].AllocatableCount == nil ||
		*detail.Drivers[0].AllocatableCount != 12 || detail.Drivers[0].TopologyKeyCount != 2 {
		t.Fatalf("first CSINode driver = %#v", detail.Drivers[0])
	}
	if detail.Drivers[1].Name != "local.csi.example.com" || detail.Drivers[1].AllocatableCount != nil ||
		detail.Drivers[1].TopologyKeyCount != 0 {
		t.Fatalf("second CSINode driver = %#v", detail.Drivers[1])
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal CSINode detail: %v", err)
	}
	for _, forbidden := range []string{
		"storage-node-01", "storage-local-01", "topology.example.com/zone", "topology.kubernetes.io/region",
		"private-uid", "private-label", "private-annotation", "private-value", "private-manager", "managedFields",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("CSINode detail leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestDecodeCSINodeAcceptsEmptyDriverListAndZeroLimit(t *testing.T) {
	t.Parallel()

	payload := csiNodePayload("worker-01")
	payload["spec"].(map[string]any)["drivers"] = []any{
		map[string]any{
			"name": "ebs.csi.example.com", "nodeID": "storage-node-01",
			"topologyKeys": []any{}, "allocatable": map[string]any{"count": 0},
		},
	}
	detail, err := decodeCSINode(mustCSINodeJSON(t, payload), "worker-01")
	if err != nil {
		t.Fatalf("decodeCSINode() error = %v", err)
	}
	if detail.DriverCount != 1 || detail.Drivers[0].AllocatableCount == nil || *detail.Drivers[0].AllocatableCount != 0 {
		t.Fatalf("decodeCSINode() = %#v", detail)
	}

	payload["spec"].(map[string]any)["drivers"] = []any{}
	detail, err = decodeCSINode(mustCSINodeJSON(t, payload), "worker-01")
	if err != nil || detail.DriverCount != 0 || len(detail.Drivers) != 0 {
		t.Fatalf("empty decodeCSINode() = %#v, %v", detail, err)
	}
}

func TestClientRejectsUnsafeCSINodeTables(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "wrong api version", mutate: func(payload map[string]any) { payload["apiVersion"] = "storage.k8s.io/v1" }},
		{name: "wrong kind", mutate: func(payload map[string]any) { payload["kind"] = "CSINodeList" }},
		{name: "missing column", mutate: func(payload map[string]any) { payload["columnDefinitions"] = csiNodeTestColumns()[:1] }},
		{name: "duplicate column", mutate: func(payload map[string]any) {
			payload["columnDefinitions"] = append(csiNodeTestColumns(), map[string]any{"name": "Drivers", "type": "integer"})
		}},
		{name: "wrong driver type", mutate: func(payload map[string]any) {
			payload["columnDefinitions"].([]any)[1].(map[string]any)["type"] = "string"
		}},
		{name: "name mismatch", mutate: func(payload map[string]any) { csiNodeFirstRow(payload)["cells"].([]any)[0] = "worker-02" }},
		{name: "namespaced metadata", mutate: func(payload map[string]any) { csiNodeFirstMetadata(payload)["namespace"] = "default" }},
		{name: "invalid node", mutate: func(payload map[string]any) {
			csiNodeFirstMetadata(payload)["name"] = "worker/01"
			csiNodeFirstRow(payload)["cells"].([]any)[0] = "worker/01"
		}},
		{name: "string driver count", mutate: func(payload map[string]any) { csiNodeFirstRow(payload)["cells"].([]any)[1] = "2" }},
		{name: "fractional driver count", mutate: func(payload map[string]any) { csiNodeFirstRow(payload)["cells"].([]any)[1] = 1.5 }},
		{name: "negative driver count", mutate: func(payload map[string]any) { csiNodeFirstRow(payload)["cells"].([]any)[1] = -1 }},
		{name: "excessive driver count", mutate: func(payload map[string]any) { csiNodeFirstRow(payload)["cells"].([]any)[1] = 129 }},
		{name: "unsafe full object", mutate: func(payload map[string]any) {
			csiNodeFirstRow(payload)["object"] = map[string]any{
				"apiVersion": "storage.k8s.io/v1", "kind": "CSINode",
				"metadata": map[string]any{"name": "worker-01"},
				"spec":     map[string]any{"drivers": []any{map[string]any{"nodeID": "private-node-id"}}},
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			payload := singleCSINodeTable()
			tt.mutate(payload)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeTestJSON(t, w, payload)
			}))
			t.Cleanup(server.Close)
			client := newNetworkTestClient(t, server)

			_, err := client.CSINodes(context.Background())
			if !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("CSINodes() error = %v, want upstream error", err)
			}
			if strings.Contains(fmt.Sprint(err), "private") {
				t.Fatalf("CSINodes() error leaked upstream content: %v", err)
			}
		})
	}
}

func TestClientRejectsDuplicateCSINodesAndContinuationTokens(t *testing.T) {
	t.Parallel()

	t.Run("duplicate node", func(t *testing.T) {
		t.Parallel()
		payload := tableResponse(csiNodeTestColumns(), []any{
			csiNodeTableRow("worker-01", 1, "2026-07-31T08:00:00Z"),
			csiNodeTableRow("worker-01", 1, "2026-07-31T08:00:00Z"),
		})
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, payload)
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.CSINodes(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CSINodes() error = %v, want duplicate error", err)
		}
	})

	t.Run("repeated continuation", func(t *testing.T) {
		t.Parallel()
		var calls atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			payload := tableResponse(csiNodeTestColumns(), []any{})
			payload["metadata"] = map[string]any{"continue": "same-token"}
			writeTestJSON(t, w, payload)
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.CSINodes(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CSINodes() error = %v, want continuation error", err)
		}
		if calls.Load() != 2 {
			t.Fatalf("upstream calls = %d, want 2", calls.Load())
		}
	})

	t.Run("unsafe continuation", func(t *testing.T) {
		t.Parallel()
		payload := tableResponse(csiNodeTestColumns(), []any{})
		payload["metadata"] = map[string]any{"continue": "next\nprivate"}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, payload)
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.CSINodes(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CSINodes() error = %v, want continuation error", err)
		}
	})
}

func TestDecodeCSINodeRejectsInvalidUpstreamObjects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "api version", mutate: func(payload map[string]any) { payload["apiVersion"] = "storage.k8s.io/v1beta1" }},
		{name: "kind", mutate: func(payload map[string]any) { payload["kind"] = "Node" }},
		{name: "name", mutate: func(payload map[string]any) { payload["metadata"].(map[string]any)["name"] = "worker-02" }},
		{name: "namespace", mutate: func(payload map[string]any) { payload["metadata"].(map[string]any)["namespace"] = "default" }},
		{name: "creation time", mutate: func(payload map[string]any) { payload["metadata"].(map[string]any)["creationTimestamp"] = nil }},
		{name: "invalid name", mutate: func(payload map[string]any) { payload["metadata"].(map[string]any)["name"] = "Worker_01" }},
		{name: "missing drivers", mutate: func(payload map[string]any) { delete(payload["spec"].(map[string]any), "drivers") }},
		{name: "too many drivers", mutate: func(payload map[string]any) {
			drivers := make([]any, csiNodeMaxDrivers+1)
			for index := range drivers {
				drivers[index] = map[string]any{"name": fmt.Sprintf("driver-%03d.example.com", index), "nodeID": "node", "topologyKeys": []any{}}
			}
			payload["spec"].(map[string]any)["drivers"] = drivers
		}},
		{name: "duplicate driver", mutate: func(payload map[string]any) {
			drivers := payload["spec"].(map[string]any)["drivers"].([]any)
			drivers[1].(map[string]any)["name"] = drivers[0].(map[string]any)["name"]
		}},
		{name: "invalid driver", mutate: func(payload map[string]any) {
			payload["spec"].(map[string]any)["drivers"].([]any)[0].(map[string]any)["name"] = "CSI/driver"
		}},
		{name: "missing node id", mutate: func(payload map[string]any) {
			delete(payload["spec"].(map[string]any)["drivers"].([]any)[0].(map[string]any), "nodeID")
		}},
		{name: "empty node id", mutate: func(payload map[string]any) {
			payload["spec"].(map[string]any)["drivers"].([]any)[0].(map[string]any)["nodeID"] = ""
		}},
		{name: "unsafe node id", mutate: func(payload map[string]any) {
			payload["spec"].(map[string]any)["drivers"].([]any)[0].(map[string]any)["nodeID"] = "private\nnode"
		}},
		{name: "long node id", mutate: func(payload map[string]any) {
			payload["spec"].(map[string]any)["drivers"].([]any)[0].(map[string]any)["nodeID"] = strings.Repeat("x", csiNodeMaxNodeIDBytes+1)
		}},
		{name: "invalid topology key", mutate: func(payload map[string]any) {
			payload["spec"].(map[string]any)["drivers"].([]any)[0].(map[string]any)["topologyKeys"] = []any{"../private"}
		}},
		{name: "too many topology keys", mutate: func(payload map[string]any) {
			keys := make([]any, csiNodeMaxTopologyKeysPerDriver+1)
			for index := range keys {
				keys[index] = fmt.Sprintf("topology.example.com/key-%d", index)
			}
			payload["spec"].(map[string]any)["drivers"].([]any)[0].(map[string]any)["topologyKeys"] = keys
		}},
		{name: "too many total topology keys", mutate: func(payload map[string]any) {
			keys := make([]any, csiNodeMaxTopologyKeysPerDriver)
			for index := range keys {
				keys[index] = fmt.Sprintf("topology.example.com/key-%d", index)
			}
			drivers := make([]any, csiNodeMaxTopologyKeysTotal/csiNodeMaxTopologyKeysPerDriver+1)
			for index := range drivers {
				drivers[index] = map[string]any{
					"name": fmt.Sprintf("driver-%03d.example.com", index), "nodeID": "node", "topologyKeys": keys,
				}
			}
			payload["spec"].(map[string]any)["drivers"] = drivers
		}},
		{name: "negative allocatable", mutate: func(payload map[string]any) {
			payload["spec"].(map[string]any)["drivers"].([]any)[0].(map[string]any)["allocatable"] = map[string]any{"count": -1}
		}},
		{name: "fractional allocatable", mutate: func(payload map[string]any) {
			payload["spec"].(map[string]any)["drivers"].([]any)[0].(map[string]any)["allocatable"] = map[string]any{"count": 1.5}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			payload := csiNodePayload("worker-01")
			tt.mutate(payload)
			_, err := decodeCSINode(mustCSINodeJSON(t, payload), "worker-01")
			if !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("decodeCSINode() error = %v, want upstream error", err)
			}
			if strings.Contains(fmt.Sprint(err), "private") {
				t.Fatalf("decodeCSINode() error leaked upstream content: %v", err)
			}
		})
	}
}

func TestClientRejectsInvalidCSINodeNameBeforeRequest(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)
	if _, err := client.CSINode(context.Background(), "../nodes"); err == nil {
		t.Fatal("CSINode() accepted invalid name")
	}
	if calls.Load() != 0 {
		t.Fatalf("upstream calls = %d, want 0", calls.Load())
	}
}

func TestClientBoundsCSINodePagesItemsAndBodies(t *testing.T) {
	t.Run("pages", func(t *testing.T) {
		var calls atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			call := calls.Add(1)
			payload := tableResponse(csiNodeTestColumns(), []any{})
			payload["metadata"] = map[string]any{"continue": fmt.Sprintf("page-%d", call)}
			writeTestJSON(t, w, payload)
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.CSINodes(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CSINodes() error = %v, want page error", err)
		}
		if calls.Load() != csiNodeMaxListPages {
			t.Fatalf("upstream calls = %d, want %d", calls.Load(), csiNodeMaxListPages)
		}
	})

	t.Run("items", func(t *testing.T) {
		rows := make([]any, csiNodeMaxListItems+1)
		for index := range rows {
			name := fmt.Sprintf("worker-%04d", index)
			rows[index] = csiNodeTableRow(name, 1, "2026-07-31T08:00:00Z")
		}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, tableResponse(csiNodeTestColumns(), rows))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.CSINodes(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CSINodes() error = %v, want item error", err)
		}
	})

	t.Run("list body", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", int(csiNodeMaxPageBytes)+1)))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.CSINodes(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CSINodes() error = %v, want body error", err)
		}
	})

	t.Run("detail body", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", int(csiNodeMaxDetailBytes)+1)))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.CSINode(context.Background(), "worker-01"); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CSINode() error = %v, want body error", err)
		}
	})
}

func csiNodeTestColumns() []any {
	return []any{
		map[string]any{"name": "Name", "type": "string"},
		map[string]any{"name": "Drivers", "type": "integer"},
		map[string]any{"name": "Age", "type": "string"},
	}
}

func csiNodeTableRow(name string, driverCount int, createdAt string) map[string]any {
	return tableRow([]any{name, driverCount, "1h"}, name, "", createdAt)
}

func singleCSINodeTable() map[string]any {
	return tableResponse(csiNodeTestColumns(), []any{csiNodeTableRow("worker-01", 2, "2026-07-31T08:00:00Z")})
}

func csiNodeFirstRow(payload map[string]any) map[string]any {
	return payload["rows"].([]any)[0].(map[string]any)
}

func csiNodeFirstMetadata(payload map[string]any) map[string]any {
	return csiNodeFirstRow(payload)["object"].(map[string]any)["metadata"].(map[string]any)
}

func csiNodePayload(name string) map[string]any {
	return map[string]any{
		"apiVersion": "storage.k8s.io/v1",
		"kind":       "CSINode",
		"metadata": map[string]any{
			"name": name, "creationTimestamp": "2026-07-31T08:00:00Z",
		},
		"spec": map[string]any{"drivers": []any{
			map[string]any{
				"name": "local.csi.example.com", "nodeID": "storage-local-01", "topologyKeys": []any{},
			},
			map[string]any{
				"name": "ebs.csi.example.com", "nodeID": "storage-node-01",
				"topologyKeys": []any{"topology.example.com/zone", "topology.kubernetes.io/region"},
				"allocatable":  map[string]any{"count": 12},
			},
		}},
	}
}

func mustCSINodeJSON(t *testing.T, payload map[string]any) []byte {
	t.Helper()
	value, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal CSINode payload: %v", err)
	}
	return value
}
