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

func TestClientReadsConfigurationTableSummaries(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requested := make([]string, 0, 2)
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
		case "/api/v1/configmaps":
			response := tableResponse(
				[]any{
					map[string]any{"name": "Data", "type": "integer"},
					map[string]any{"name": "Name", "type": "string"},
					map[string]any{"name": "Age", "type": "string"},
				},
				[]any{
					tableRow([]any{3, "settings", "2d"}, "settings", "payments", "2026-07-24T08:00:00Z"),
				},
			)
			if r.URL.Query().Get("continue") == "page-two" {
				response["rows"] = []any{
					tableRow([]any{0, "empty", "3d"}, "empty", "default", "2026-07-23T08:00:00Z"),
				}
			} else {
				response["metadata"] = map[string]any{"continue": "page-two"}
			}
			writeTestJSON(t, w, response)
		case "/api/v1/namespaces/payments/configmaps":
			writeTestJSON(t, w, tableResponse(
				[]any{
					map[string]any{"name": "Name", "type": "string"},
					map[string]any{"name": "Data", "type": "integer"},
				},
				[]any{
					tableRow([]any{"settings", 3}, "settings", "payments", "2026-07-24T08:00:00Z"),
				},
			))
		case "/api/v1/namespaces/payments/secrets":
			writeTestJSON(t, w, tableResponse(
				[]any{
					map[string]any{"name": "Name", "type": "string"},
					map[string]any{"name": "Type", "type": "string"},
					map[string]any{"name": "Data", "type": "integer"},
				},
				[]any{
					tableRow([]any{"registry", "kubernetes.io/dockerconfigjson", 1}, "registry", "payments", "2026-07-25T08:00:00Z"),
				},
			))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	configMaps, err := client.ConfigMaps(context.Background(), "")
	if err != nil {
		t.Fatalf("ConfigMaps() error = %v", err)
	}
	if len(configMaps) != 2 || configMaps[0].Namespace != "default" || configMaps[0].Name != "empty" || configMaps[1].DataCount != 3 {
		t.Fatalf("ConfigMaps() = %#v", configMaps)
	}
	if configMaps[1].CreatedAt.IsZero() {
		t.Errorf("ConfigMap CreatedAt is zero: %#v", configMaps[1])
	}
	scopedConfigMaps, err := client.ConfigMaps(context.Background(), "payments")
	if err != nil || len(scopedConfigMaps) != 1 || scopedConfigMaps[0].Namespace != "payments" {
		t.Fatalf("ConfigMaps(payments) = %#v, %v", scopedConfigMaps, err)
	}

	secrets, err := client.Secrets(context.Background(), "payments")
	if err != nil {
		t.Fatalf("Secrets() error = %v", err)
	}
	if len(secrets) != 1 || secrets[0].Name != "registry" || secrets[0].Namespace != "payments" ||
		secrets[0].Type != "kubernetes.io/dockerconfigjson" || secrets[0].DataCount != 1 {
		t.Fatalf("Secrets() = %#v", secrets)
	}

	mu.Lock()
	gotRequests := append([]string(nil), requested...)
	mu.Unlock()
	wantRequests := []string{
		"/api/v1/configmaps?includeObject=Metadata&limit=500",
		"/api/v1/configmaps?continue=page-two&includeObject=Metadata&limit=500",
		"/api/v1/namespaces/payments/configmaps?includeObject=Metadata&limit=500",
		"/api/v1/namespaces/payments/secrets?includeObject=Metadata&limit=500",
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

func TestClientRejectsUnsafeConfigurationTableWithoutLeakingValues(t *testing.T) {
	t.Parallel()

	const sensitiveValue = "c2Vuc2l0aXZlLXNlY3JldA=="
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, map[string]any{
			"apiVersion": "meta.k8s.io/v1",
			"kind":       "Table",
			"columnDefinitions": []any{
				map[string]any{"name": "Name", "type": "string"},
				map[string]any{"name": "Type", "type": "string"},
				map[string]any{"name": "Data", "type": "integer"},
			},
			"rows": []any{map[string]any{
				"cells": []any{"unsafe", "Opaque", 1},
				"object": map[string]any{
					"apiVersion": "v1", "kind": "Secret",
					"metadata": map[string]any{"name": "unsafe", "namespace": "payments"},
					"data":     map[string]any{"token": sensitiveValue},
				},
			}},
		})
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	_, err := client.Secrets(context.Background(), "payments")
	if err == nil {
		t.Fatal("Secrets() accepted a full Secret row object")
	}
	if strings.Contains(err.Error(), sensitiveValue) {
		t.Fatalf("Secrets() error leaked a Secret value: %v", err)
	}
}

func TestClientRejectsConfigurationRowsOutsideRequestedNamespace(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, tableResponse(
			[]any{
				map[string]any{"name": "Name", "type": "string"},
				map[string]any{"name": "Type", "type": "string"},
				map[string]any{"name": "Data", "type": "integer"},
			},
			[]any{
				tableRow([]any{"registry", "Opaque", 1}, "registry", "default", "2026-07-25T08:00:00Z"),
			},
		))
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	if _, err := client.Secrets(context.Background(), "payments"); !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("Secrets() error = %v, want upstream error", err)
	}
}

func TestClientValidatesConfigurationNamespacesBeforeRequest(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	if _, err := client.ConfigMaps(context.Background(), "bad/namespace"); err == nil {
		t.Fatal("ConfigMaps() accepted an invalid namespace")
	}
	if _, err := client.Secrets(context.Background(), ""); err == nil {
		t.Fatal("Secrets() accepted an empty namespace")
	}
	if _, err := client.Secrets(context.Background(), "bad/namespace"); err == nil {
		t.Fatal("Secrets() accepted an invalid namespace")
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("invalid namespaces made %d requests", got)
	}
}

func TestClientBoundsConfigurationTablePages(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		response := tableResponse(
			[]any{map[string]any{"name": "Data", "type": "integer"}},
			[]any{},
		)
		response["metadata"] = map[string]any{"continue": "more"}
		writeTestJSON(t, w, response)
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	_, err := client.ConfigMaps(context.Background(), "")
	if !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("ConfigMaps() error = %v, want upstream error", err)
	}
	if got := requests.Load(); got != maxListPages {
		t.Fatalf("requests = %d, want %d", got, maxListPages)
	}
}

func TestConfigurationTableDecodersEnforceSummaryShape(t *testing.T) {
	t.Parallel()

	var duplicateColumns kubernetesTable
	if err := json.Unmarshal([]byte(`{"columnDefinitions":[{"name":"Data"},{"name":"Data"}]}`), &duplicateColumns); err != nil {
		t.Fatalf("decode duplicate columns fixture: %v", err)
	}
	if _, err := configurationTableColumn(duplicateColumns.ColumnDefinitions, "Data"); !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("duplicate Data column error = %v", err)
	}
	if _, err := configurationTableColumn(duplicateColumns.ColumnDefinitions, "Type"); !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("missing Type column error = %v", err)
	}

	safeMetadata := json.RawMessage(`{
		"apiVersion":"meta.k8s.io/v1",
		"kind":"PartialObjectMetadata",
		"metadata":{"name":"settings","namespace":"payments","creationTimestamp":"2026-07-24T08:00:00Z"}
	}`)
	item, err := decodeConfigurationTableRow(kubernetesTableRow{
		Cells: []json.RawMessage{json.RawMessage(`3`), json.RawMessage(`""`)}, Object: safeMetadata,
	}, 0, 1)
	if err != nil || item.Type != "Opaque" || item.DataCount != 3 {
		t.Fatalf("decodeConfigurationTableRow() = %#v, %v", item, err)
	}
	invalidRows := []kubernetesTableRow{
		{Cells: nil, Object: safeMetadata},
		{Cells: []json.RawMessage{json.RawMessage(`-1`)}, Object: safeMetadata},
		{Cells: []json.RawMessage{json.RawMessage(`1`), json.RawMessage(`"bad\nvalue"`)}, Object: safeMetadata},
		{Cells: []json.RawMessage{json.RawMessage(`1`)}, Object: json.RawMessage(`{"apiVersion":`)},
		{Cells: []json.RawMessage{json.RawMessage(`1`)}, Object: json.RawMessage(`{
			"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadata",
			"metadata":{"name":"settings","namespace":"payments"}
		}`)},
	}
	for index, row := range invalidRows {
		typeColumn := -1
		if index == 2 {
			typeColumn = 1
		}
		if _, err := decodeConfigurationTableRow(row, 0, typeColumn); !errors.Is(err, domain.ErrUpstream) {
			t.Errorf("invalid row %d error = %v", index, err)
		}
	}
}

func tableResponse(columns, rows []any) map[string]any {
	return map[string]any{
		"apiVersion":        "meta.k8s.io/v1",
		"kind":              "Table",
		"metadata":          map[string]any{"continue": ""},
		"columnDefinitions": columns,
		"rows":              rows,
	}
}

func tableRow(cells []any, name, namespace, createdAt string) map[string]any {
	return map[string]any{
		"cells": cells,
		"object": map[string]any{
			"apiVersion": "meta.k8s.io/v1",
			"kind":       "PartialObjectMetadata",
			"metadata": map[string]any{
				"name": name, "namespace": namespace, "creationTimestamp": createdAt,
			},
		},
	}
}
