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

func TestClientReadsHelmReleaseHistoryFromMetadataOnlyPages(t *testing.T) {
	t.Parallel()

	type requestSnapshot struct {
		accept       string
		limit        string
		selector     string
		continuation string
	}
	var mu sync.Mutex
	requests := make([]requestSnapshot, 0, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/payments/secrets" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		requests = append(requests, requestSnapshot{
			accept: r.Header.Get("Accept"), limit: r.URL.Query().Get("limit"),
			selector: r.URL.Query().Get("labelSelector"), continuation: r.URL.Query().Get("continue"),
		})
		mu.Unlock()
		if r.URL.Query().Get("continue") == "page-two" {
			writeTestJSON(t, w, accessMetadataList("", []any{
				helmHistoryMetadata("gateway", "payments", 4, "deployed", "2026-07-30T09:04:00Z"),
				helmHistoryMetadata("gateway", "payments", 2, "superseded", "2026-07-30T09:02:00Z"),
			}))
			return
		}
		first := helmHistoryMetadata("gateway", "payments", 1, "superseded", "2026-07-30T09:01:00Z")
		metadata := first["metadata"].(map[string]any)
		metadata["annotations"] = map[string]any{"private.example.com/token": "private-value"}
		metadata["managedFields"] = []any{map[string]any{"manager": "private-manager"}}
		writeTestJSON(t, w, accessMetadataList("page-two", []any{
			first,
			helmHistoryMetadata("gateway", "payments", 3, "failed", "2026-07-30T09:03:00Z"),
		}))
	}))
	t.Cleanup(server.Close)

	history, err := newNetworkTestClient(t, server).HelmReleaseHistory(context.Background(), "payments", "gateway")
	if err != nil {
		t.Fatalf("HelmReleaseHistory() error = %v", err)
	}
	if history.Name != "gateway" || history.Namespace != "payments" || history.Truncated || len(history.Revisions) != 4 {
		t.Fatalf("HelmReleaseHistory() = %#v", history)
	}
	wantRevisions := []int{4, 3, 2, 1}
	wantStatuses := []string{"deployed", "failed", "superseded", "superseded"}
	for index, revision := range history.Revisions {
		if revision.Revision != wantRevisions[index] || revision.Status != wantStatuses[index] || revision.CreatedAt.IsZero() {
			t.Fatalf("revision[%d] = %#v", index, revision)
		}
	}
	encoded, err := json.Marshal(history)
	if err != nil {
		t.Fatalf("marshal history: %v", err)
	}
	for _, forbidden := range []string{"private-value", "private-manager", "managedFields", "annotations", "labels", "sh.helm.release.v1"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("history leaked %q: %s", forbidden, encoded)
		}
	}

	mu.Lock()
	gotRequests := append([]requestSnapshot(nil), requests...)
	mu.Unlock()
	if len(gotRequests) != 2 {
		t.Fatalf("requests = %#v", gotRequests)
	}
	for index, request := range gotRequests {
		if request.accept != kubernetesPartialMetadataListAccept || request.limit != "50" ||
			request.selector != "owner=helm,name=gateway" {
			t.Fatalf("request[%d] = %#v", index, request)
		}
	}
	if gotRequests[0].continuation != "" || gotRequests[1].continuation != "page-two" {
		t.Fatalf("continuations = %#v", gotRequests)
	}
}

func TestClientTruncatesHelmReleaseHistoryToLatestTen(t *testing.T) {
	t.Parallel()

	items := make([]any, 0, 12)
	for revision := 1; revision <= 12; revision++ {
		items = append(items, helmHistoryMetadata(
			"gateway", "payments", revision, "superseded", fmt.Sprintf("2026-07-30T09:%02d:00Z", revision),
		))
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, accessMetadataList("", items))
	}))
	t.Cleanup(server.Close)

	history, err := newNetworkTestClient(t, server).HelmReleaseHistory(context.Background(), "payments", "gateway")
	if err != nil {
		t.Fatalf("HelmReleaseHistory() error = %v", err)
	}
	if !history.Truncated || len(history.Revisions) != 10 ||
		history.Revisions[0].Revision != 12 || history.Revisions[9].Revision != 3 {
		t.Fatalf("HelmReleaseHistory() = %#v", history)
	}
}

func TestClientReturnsEmptyHelmReleaseHistoryAsArray(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, accessMetadataList("", []any{}))
	}))
	t.Cleanup(server.Close)

	history, err := newNetworkTestClient(t, server).HelmReleaseHistory(context.Background(), "payments", "gateway")
	if err != nil {
		t.Fatalf("HelmReleaseHistory() error = %v", err)
	}
	if history.Revisions == nil || len(history.Revisions) != 0 || history.Truncated {
		t.Fatalf("HelmReleaseHistory() = %#v", history)
	}
	encoded, err := json.Marshal(history)
	if err != nil {
		t.Fatalf("marshal history: %v", err)
	}
	if !strings.Contains(string(encoded), `"revisions":[]`) {
		t.Fatalf("empty history is not an array: %s", encoded)
	}
}

func TestClientRejectsInvalidHelmReleaseHistoryInputBeforeRequest(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)
	for _, input := range []struct {
		namespace string
		name      string
		field     string
	}{
		{namespace: "", name: "gateway", field: "namespace"},
		{namespace: "PAYMENTS", name: "gateway", field: "namespace"},
		{namespace: "payments", name: "", field: "release_name"},
		{namespace: "payments", name: "../gateway", field: "release_name"},
		{namespace: "payments", name: "Gateway", field: "release_name"},
	} {
		_, err := client.HelmReleaseHistory(context.Background(), input.namespace, input.name)
		var validationErr *domain.ValidationError
		if !errors.As(err, &validationErr) || validationErr.Field != input.field {
			t.Errorf("HelmReleaseHistory(%q, %q) error = %v", input.namespace, input.name, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid inputs made %d upstream requests", calls.Load())
	}
}

func TestClientRejectsUnsafeHelmReleaseHistoryMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload func() any
	}{
		{name: "wrong list identity", payload: func() any {
			return map[string]any{"apiVersion": "v1", "kind": "SecretList", "items": []any{}}
		}},
		{name: "wrong item identity", payload: func() any {
			item := helmHistoryMetadata("gateway", "payments", 1, "deployed", "2026-07-30T09:00:00Z")
			item["kind"] = "Secret"
			return accessMetadataList("", []any{item})
		}},
		{name: "wrong namespace", payload: func() any {
			return accessMetadataList("", []any{helmHistoryMetadata("gateway", "other", 1, "deployed", "2026-07-30T09:00:00Z")})
		}},
		{name: "wrong owner", payload: func() any {
			item := helmHistoryMetadata("gateway", "payments", 1, "deployed", "2026-07-30T09:00:00Z")
			item["metadata"].(map[string]any)["labels"].(map[string]string)["owner"] = "other"
			return accessMetadataList("", []any{item})
		}},
		{name: "wrong release label", payload: func() any {
			item := helmHistoryMetadata("gateway", "payments", 1, "deployed", "2026-07-30T09:00:00Z")
			item["metadata"].(map[string]any)["labels"].(map[string]string)["name"] = "other"
			return accessMetadataList("", []any{item})
		}},
		{name: "invalid revision", payload: func() any {
			item := helmHistoryMetadata("gateway", "payments", 1, "deployed", "2026-07-30T09:00:00Z")
			item["metadata"].(map[string]any)["labels"].(map[string]string)["version"] = "0"
			return accessMetadataList("", []any{item})
		}},
		{name: "overflow revision", payload: func() any {
			item := helmHistoryMetadata("gateway", "payments", 1, "deployed", "2026-07-30T09:00:00Z")
			item["metadata"].(map[string]any)["labels"].(map[string]string)["version"] = "2147483648"
			return accessMetadataList("", []any{item})
		}},
		{name: "wrong secret name", payload: func() any {
			item := helmHistoryMetadata("gateway", "payments", 1, "deployed", "2026-07-30T09:00:00Z")
			item["metadata"].(map[string]any)["name"] = "private-secret"
			return accessMetadataList("", []any{item})
		}},
		{name: "invalid status", payload: func() any {
			item := helmHistoryMetadata("gateway", "payments", 1, "deployed", "2026-07-30T09:00:00Z")
			item["metadata"].(map[string]any)["labels"].(map[string]string)["status"] = "healthy"
			return accessMetadataList("", []any{item})
		}},
		{name: "missing creation time", payload: func() any {
			item := helmHistoryMetadata("gateway", "payments", 1, "deployed", "2026-07-30T09:00:00Z")
			delete(item["metadata"].(map[string]any), "creationTimestamp")
			return accessMetadataList("", []any{item})
		}},
		{name: "duplicate revision", payload: func() any {
			return accessMetadataList("", []any{
				helmHistoryMetadata("gateway", "payments", 1, "deployed", "2026-07-30T09:00:00Z"),
				helmHistoryMetadata("gateway", "payments", 1, "superseded", "2026-07-30T09:01:00Z"),
			})
		}},
		{name: "secret data", payload: func() any {
			item := helmHistoryMetadata("gateway", "payments", 1, "deployed", "2026-07-30T09:00:00Z")
			item["data"] = map[string]any{"release": "private-release-payload"}
			return accessMetadataList("", []any{item})
		}},
		{name: "unsafe continuation", payload: func() any {
			return accessMetadataList("next\npage", []any{})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeTestJSON(t, w, tt.payload())
			}))
			t.Cleanup(server.Close)
			_, err := newNetworkTestClient(t, server).HelmReleaseHistory(context.Background(), "payments", "gateway")
			if !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("HelmReleaseHistory() error = %v, want upstream error", err)
			}
		})
	}
}

func TestClientRejectsRepeatedHelmReleaseHistoryContinuation(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, accessMetadataList("same-token", []any{}))
	}))
	t.Cleanup(server.Close)
	_, err := newNetworkTestClient(t, server).HelmReleaseHistory(context.Background(), "payments", "gateway")
	if !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("HelmReleaseHistory() error = %v, want repeated continuation error", err)
	}
}

func TestClientBoundsHelmReleaseHistoryPagesItemsAndBodies(t *testing.T) {
	t.Run("pages", func(t *testing.T) {
		var calls atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			call := calls.Add(1)
			writeTestJSON(t, w, accessMetadataList(fmt.Sprintf("page-%d", call), []any{}))
		}))
		t.Cleanup(server.Close)
		_, err := newNetworkTestClient(t, server).HelmReleaseHistory(context.Background(), "payments", "gateway")
		if !errors.Is(err, domain.ErrUpstream) || calls.Load() != 4 {
			t.Fatalf("HelmReleaseHistory() error = %v, calls = %d", err, calls.Load())
		}
	})

	t.Run("items", func(t *testing.T) {
		items := make([]any, 201)
		for index := range items {
			revision := index + 1
			items[index] = helmHistoryMetadata(
				"gateway", "payments", revision, "superseded", "2026-07-30T09:00:00Z",
			)
		}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, accessMetadataList("", items))
		}))
		t.Cleanup(server.Close)
		_, err := newNetworkTestClient(t, server).HelmReleaseHistory(context.Background(), "payments", "gateway")
		if !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("HelmReleaseHistory() error = %v, want item limit error", err)
		}
	})

	t.Run("bytes", func(t *testing.T) {
		item := helmHistoryMetadata("gateway", "payments", 1, "deployed", "2026-07-30T09:00:00Z")
		item["metadata"].(map[string]any)["annotations"] = map[string]any{
			"private.example.com/padding": strings.Repeat("x", 2*1024*1024),
		}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, accessMetadataList("", []any{item}))
		}))
		t.Cleanup(server.Close)
		_, err := newNetworkTestClient(t, server).HelmReleaseHistory(context.Background(), "payments", "gateway")
		if !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("HelmReleaseHistory() error = %v, want byte limit error", err)
		}
	})
}

func helmHistoryMetadata(release, namespace string, revision int, status, createdAt string) map[string]any {
	return map[string]any{
		"apiVersion": "meta.k8s.io/v1",
		"kind":       "PartialObjectMetadata",
		"metadata": map[string]any{
			"name":              fmt.Sprintf("sh.helm.release.v1.%s.v%d", release, revision),
			"namespace":         namespace,
			"creationTimestamp": createdAt,
			"labels": map[string]string{
				"owner": "helm", "name": release, "version": fmt.Sprintf("%d", revision), "status": status,
				"private.example.com/label": "private-value",
			},
		},
	}
}
