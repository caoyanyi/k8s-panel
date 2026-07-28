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

func TestClientReadsStorageTableSummaries(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requested := make([]string, 0, 5)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != kubernetesTableAccept {
			t.Errorf("Accept = %q, want %q", got, kubernetesTableAccept)
		}
		if got := r.URL.Query().Get("includeObject"); got != "Metadata" {
			t.Errorf("includeObject = %q, want Metadata", got)
		}
		if got := r.URL.Query().Get("limit"); got != listPageSize {
			t.Errorf("limit = %q, want %q", got, listPageSize)
		}
		mu.Lock()
		requested = append(requested, r.URL.RequestURI())
		mu.Unlock()

		switch r.URL.Path {
		case "/api/v1/persistentvolumeclaims":
			response := tableResponse(persistentVolumeClaimColumns(), []any{
				tableRow([]any{"cache", "Pending", "", "", "RWO", "fast", "Filesystem"}, "cache", "payments", "2026-07-25T08:00:00Z"),
			})
			if r.URL.Query().Get("continue") == "page-two" {
				response["rows"] = []any{
					tableRow([]any{"data", "Bound", "pv-data", "20Gi", "RWO", "standard", "Filesystem"}, "data", "default", "2026-07-24T08:00:00Z"),
				}
			} else {
				response["metadata"] = map[string]any{"continue": "page-two"}
			}
			writeTestJSON(t, w, response)
		case "/api/v1/namespaces/payments/persistentvolumeclaims":
			writeTestJSON(t, w, tableResponse(persistentVolumeClaimColumns(), []any{
				tableRow([]any{"cache", "Pending", "", "", "RWO", "fast", "Filesystem"}, "cache", "payments", "2026-07-25T08:00:00Z"),
			}))
		case "/api/v1/persistentvolumes":
			writeTestJSON(t, w, tableResponse([]any{
				map[string]any{"name": "Name", "type": "string"},
				map[string]any{"name": "Status", "type": "string"},
				map[string]any{"name": "Claim", "type": "string"},
				map[string]any{"name": "Capacity", "type": "string"},
				map[string]any{"name": "Access Modes", "type": "string"},
				map[string]any{"name": "StorageClass", "type": "string"},
				map[string]any{"name": "Reclaim Policy", "type": "string"},
				map[string]any{"name": "VolumeMode", "type": "string"},
			}, []any{
				tableRow([]any{"pv-data", "Bound", "default/data", "20Gi", "RWO", "standard", "Delete", "Filesystem"}, "pv-data", "", "2026-07-23T08:00:00Z"),
			}))
		case "/apis/storage.k8s.io/v1/storageclasses":
			writeTestJSON(t, w, tableResponse([]any{
				map[string]any{"name": "Name", "type": "string"},
				map[string]any{"name": "Provisioner", "type": "string"},
				map[string]any{"name": "ReclaimPolicy", "type": "string"},
				map[string]any{"name": "VolumeBindingMode", "type": "string"},
				map[string]any{"name": "AllowVolumeExpansion", "type": "string"},
			}, []any{
				tableRow([]any{"standard (default)", "csi.example.com", "Delete", "WaitForFirstConsumer", true}, "standard", "", "2026-07-22T08:00:00Z"),
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	claims, err := client.PersistentVolumeClaims(context.Background(), "")
	if err != nil {
		t.Fatalf("PersistentVolumeClaims() error = %v", err)
	}
	if len(claims) != 2 || claims[0].Namespace != "default" || claims[0].Name != "data" ||
		claims[0].Status != "Bound" || claims[0].Volume != "pv-data" || claims[0].Capacity != "20Gi" ||
		claims[0].AccessModes != "RWO" || claims[0].StorageClass != "standard" || claims[0].VolumeMode != "Filesystem" {
		t.Fatalf("PersistentVolumeClaims() = %#v", claims)
	}
	scopedClaims, err := client.PersistentVolumeClaims(context.Background(), "payments")
	if err != nil || len(scopedClaims) != 1 || scopedClaims[0].Namespace != "payments" {
		t.Fatalf("PersistentVolumeClaims(payments) = %#v, %v", scopedClaims, err)
	}

	volumes, err := client.PersistentVolumes(context.Background())
	if err != nil || len(volumes) != 1 || volumes[0].Name != "pv-data" || volumes[0].Claim != "default/data" ||
		volumes[0].ReclaimPolicy != "Delete" || volumes[0].CreatedAt.IsZero() {
		t.Fatalf("PersistentVolumes() = %#v, %v", volumes, err)
	}
	classes, err := client.StorageClasses(context.Background())
	if err != nil || len(classes) != 1 || classes[0].Name != "standard" ||
		classes[0].Provisioner != "csi.example.com" || classes[0].VolumeBindingMode != "WaitForFirstConsumer" ||
		!classes[0].AllowVolumeExpansion || !classes[0].Default {
		t.Fatalf("StorageClasses() = %#v, %v", classes, err)
	}

	mu.Lock()
	gotRequests := append([]string(nil), requested...)
	mu.Unlock()
	wantRequests := []string{
		"/api/v1/persistentvolumeclaims?includeObject=Metadata&limit=500",
		"/api/v1/persistentvolumeclaims?continue=page-two&includeObject=Metadata&limit=500",
		"/api/v1/namespaces/payments/persistentvolumeclaims?includeObject=Metadata&limit=500",
		"/api/v1/persistentvolumes?includeObject=Metadata&limit=500",
		"/apis/storage.k8s.io/v1/storageclasses?includeObject=Metadata&limit=500",
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

func TestClientRejectsUnsafeStorageTableWithoutLeakingVolumeSource(t *testing.T) {
	t.Parallel()

	const sensitiveHandle = "storage-account/private-volume-handle"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, map[string]any{
			"apiVersion": "meta.k8s.io/v1",
			"kind":       "Table",
			"columnDefinitions": []any{
				map[string]any{"name": "Name", "type": "string"},
				map[string]any{"name": "Status", "type": "string"},
				map[string]any{"name": "Claim", "type": "string"},
				map[string]any{"name": "Capacity", "type": "string"},
				map[string]any{"name": "Access Modes", "type": "string"},
				map[string]any{"name": "StorageClass", "type": "string"},
				map[string]any{"name": "Reclaim Policy", "type": "string"},
				map[string]any{"name": "VolumeMode", "type": "string"},
			},
			"rows": []any{map[string]any{
				"cells": []any{"unsafe", "Bound", "payments/data", "20Gi", "RWO", "standard", "Delete", "Filesystem"},
				"object": map[string]any{
					"apiVersion": "v1", "kind": "PersistentVolume",
					"metadata": map[string]any{"name": "unsafe"},
					"spec":     map[string]any{"csi": map[string]any{"volumeHandle": sensitiveHandle}},
				},
			}},
		})
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	_, err := client.PersistentVolumes(context.Background())
	if !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("PersistentVolumes() error = %v, want upstream error", err)
	}
	if strings.Contains(err.Error(), sensitiveHandle) {
		t.Fatalf("PersistentVolumes() error leaked a volume handle: %v", err)
	}
}

func TestClientValidatesStorageScopeBeforeRequest(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	if _, err := client.PersistentVolumeClaims(context.Background(), "bad/namespace"); err == nil {
		t.Fatal("PersistentVolumeClaims() accepted an invalid namespace")
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("invalid namespace made %d requests", got)
	}
}

func TestClientRejectsMalformedStorageTablesAndBoundsPages(t *testing.T) {
	t.Parallel()

	t.Run("namespace escape", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, tableResponse(persistentVolumeClaimColumns(), []any{
				tableRow([]any{"data", "Bound", "pv-data", "20Gi", "RWO", "standard", "Filesystem"}, "data", "default", "2026-07-24T08:00:00Z"),
			}))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.PersistentVolumeClaims(context.Background(), "payments"); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("PersistentVolumeClaims() error = %v, want upstream error", err)
		}
	})

	t.Run("missing column", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, tableResponse([]any{
				map[string]any{"name": "Name", "type": "string"},
			}, []any{}))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.PersistentVolumes(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("PersistentVolumes() error = %v, want upstream error", err)
		}
	})

	t.Run("page limit", func(t *testing.T) {
		var requests atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			response := tableResponse(persistentVolumeClaimColumns(), []any{})
			response["metadata"] = map[string]any{"continue": "more"}
			writeTestJSON(t, w, response)
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.PersistentVolumeClaims(context.Background(), ""); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("PersistentVolumeClaims() error = %v, want upstream error", err)
		}
		if got := requests.Load(); got != maxListPages {
			t.Fatalf("requests = %d, want %d", got, maxListPages)
		}
	})
}

func TestStorageTableCellsEnforceBoundedScalarValues(t *testing.T) {
	t.Parallel()

	row := kubernetesTableRow{Cells: []json.RawMessage{
		json.RawMessage(`null`),
		json.RawMessage(`true`),
		json.RawMessage(`"true"`),
		json.RawMessage(`"<unset>"`),
		json.RawMessage(`"yes"`),
		json.RawMessage(`"bad\nvalue"`),
		json.RawMessage(`{}`),
	}}
	if value, err := storageStringCell(row, 0); err != nil || value != "" {
		t.Fatalf("storageStringCell(null) = %q, %v", value, err)
	}
	for _, index := range []int{-1, 5, 6} {
		if _, err := storageStringCell(row, index); !errors.Is(err, domain.ErrUpstream) {
			t.Errorf("storageStringCell(%d) error = %v, want upstream error", index, err)
		}
	}
	for index, want := range map[int]bool{0: false, 1: true, 2: true, 3: false} {
		if value, err := storageBoolCell(row, index); err != nil || value != want {
			t.Errorf("storageBoolCell(%d) = %t, %v, want %t", index, value, err, want)
		}
	}
	for _, index := range []int{-1, 4, 6} {
		if _, err := storageBoolCell(row, index); !errors.Is(err, domain.ErrUpstream) {
			t.Errorf("storageBoolCell(%d) error = %v, want upstream error", index, err)
		}
	}

	overlong, err := json.Marshal(strings.Repeat("x", maxTableStringBytes+1))
	if err != nil {
		t.Fatalf("marshal overlong table string: %v", err)
	}
	if _, err := storageStringCell(kubernetesTableRow{Cells: []json.RawMessage{overlong}}, 0); !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("overlong storage string error = %v, want upstream error", err)
	}
}

func persistentVolumeClaimColumns() []any {
	return []any{
		map[string]any{"name": "Name", "type": "string"},
		map[string]any{"name": "Status", "type": "string"},
		map[string]any{"name": "Volume", "type": "string"},
		map[string]any{"name": "Capacity", "type": "string"},
		map[string]any{"name": "Access Modes", "type": "string"},
		map[string]any{"name": "StorageClass", "type": "string"},
		map[string]any{"name": "VolumeMode", "type": "string"},
	}
}
