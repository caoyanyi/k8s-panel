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

func TestClientListsPodSecurityAdmissionNamespacesFromBoundedMetadata(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requests := make([]string, 0, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Accept"); got != kubernetesPartialMetadataListAccept {
			t.Errorf("Accept = %q, want %q", got, kubernetesPartialMetadataListAccept)
		}
		if got := r.URL.Query().Get("limit"); got != podSecurityAdmissionListPageSize {
			t.Errorf("limit = %q, want %q", got, podSecurityAdmissionListPageSize)
		}
		mu.Lock()
		requests = append(requests, r.URL.RequestURI())
		mu.Unlock()

		if r.URL.Query().Get("continue") == "page-two" {
			writeTestJSON(t, w, podSecurityAdmissionMetadataList("", []any{
				podSecurityAdmissionMetadataObject("alpha", nil, "2026-07-21T08:00:00Z"),
			}))
			return
		}
		writeTestJSON(t, w, podSecurityAdmissionMetadataList("page-two", []any{
			podSecurityAdmissionMetadataObject("zeta", map[string]any{
				"pod-security.kubernetes.io/enforce":         "restricted",
				"pod-security.kubernetes.io/enforce-version": "v1.30",
				"pod-security.kubernetes.io/audit":           "baseline",
				"pod-security.kubernetes.io/warn-version":    "credential=top-secret",
				"example.com/private":                        "team-secret",
			}, "2026-07-22T08:00:00Z"),
		}))
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	items, err := client.PodSecurityAdmissionNamespaces(context.Background())
	if err != nil {
		t.Fatalf("PodSecurityAdmissionNamespaces() error = %v", err)
	}
	if len(items) != 2 || items[0].Name != "alpha" || items[1].Name != "zeta" {
		t.Fatalf("PodSecurityAdmissionNamespaces() = %#v", items)
	}
	if items[0].Enforce.Status != domain.PodSecurityAdmissionModeInherited ||
		items[0].Audit.Status != domain.PodSecurityAdmissionModeInherited ||
		items[0].Warn.Status != domain.PodSecurityAdmissionModeInherited || items[0].InvalidModeCount != 0 {
		t.Fatalf("inherited namespace = %#v", items[0])
	}
	zeta := items[1]
	if zeta.Enforce.Status != domain.PodSecurityAdmissionModeConfigured || zeta.Enforce.Level != "restricted" ||
		zeta.Enforce.Version != "v1.30" || zeta.Enforce.VersionDefaulted {
		t.Fatalf("enforce mode = %#v", zeta.Enforce)
	}
	if zeta.Audit.Status != domain.PodSecurityAdmissionModeConfigured || zeta.Audit.Level != "baseline" ||
		zeta.Audit.Version != "latest" || !zeta.Audit.VersionDefaulted {
		t.Fatalf("audit mode = %#v", zeta.Audit)
	}
	if zeta.Warn.Status != domain.PodSecurityAdmissionModeInvalid || zeta.Warn.Level != "" ||
		zeta.Warn.Version != "" || zeta.InvalidModeCount != 1 {
		t.Fatalf("warn mode = %#v, invalid count = %d", zeta.Warn, zeta.InvalidModeCount)
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal projected PSA posture: %v", err)
	}
	for _, forbidden := range []string{"credential=top-secret", "team-secret", "example.com/private", "pod-security.kubernetes.io"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("projected PSA posture leaked %q: %s", forbidden, encoded)
		}
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		"/api/v1/namespaces?limit=250",
		"/api/v1/namespaces?continue=page-two&limit=250",
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

func TestClientValidatesPodSecurityAdmissionModesWithoutEchoingValues(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, podSecurityAdmissionMetadataList("", []any{
			podSecurityAdmissionMetadataObject("payments", map[string]any{
				"pod-security.kubernetes.io/enforce":         "privileged",
				"pod-security.kubernetes.io/enforce-version": "latest",
				"pod-security.kubernetes.io/audit":           "restricted",
				"pod-security.kubernetes.io/audit-version":   "v1.0",
				"pod-security.kubernetes.io/warn":            "unknown-sensitive-value",
			}, "2026-07-22T08:00:00Z"),
		}))
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	items, err := client.PodSecurityAdmissionNamespaces(context.Background())
	if err != nil || len(items) != 1 {
		t.Fatalf("PodSecurityAdmissionNamespaces() = %#v, %v", items, err)
	}
	item := items[0]
	if item.Enforce.Status != domain.PodSecurityAdmissionModeConfigured || item.Enforce.VersionDefaulted ||
		item.Audit.Status != domain.PodSecurityAdmissionModeConfigured || item.Audit.Version != "v1.0" ||
		item.Warn.Status != domain.PodSecurityAdmissionModeInvalid || item.InvalidModeCount != 1 {
		t.Fatalf("projected modes = %#v", item)
	}
	encoded, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal projected mode: %v", err)
	}
	if strings.Contains(string(encoded), "unknown-sensitive-value") {
		t.Fatalf("invalid PSA value leaked: %s", encoded)
	}
}

func TestClientBoundsPodSecurityAdmissionNamespaceInventory(t *testing.T) {
	t.Parallel()

	t.Run("page limit", func(t *testing.T) {
		var requests atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requestNumber := requests.Add(1)
			writeTestJSON(t, w, podSecurityAdmissionMetadataList(fmt.Sprintf("more-%d", requestNumber), []any{}))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)

		if _, err := client.PodSecurityAdmissionNamespaces(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("PodSecurityAdmissionNamespaces() error = %v, want upstream error", err)
		}
		if got := requests.Load(); got != maxPodSecurityAdmissionListPages {
			t.Fatalf("requests = %d, want %d", got, maxPodSecurityAdmissionListPages)
		}
	})

	t.Run("label limit", func(t *testing.T) {
		labels := make(map[string]any, maxPodSecurityAdmissionLabelsPerNamespace+1)
		for index := 0; index <= maxPodSecurityAdmissionLabelsPerNamespace; index++ {
			labels[fmt.Sprintf("example.com/label-%03d", index)] = "value"
		}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, podSecurityAdmissionMetadataList("", []any{
				podSecurityAdmissionMetadataObject("payments", labels, "2026-07-22T08:00:00Z"),
			}))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)

		if _, err := client.PodSecurityAdmissionNamespaces(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("PodSecurityAdmissionNamespaces() error = %v, want upstream error", err)
		}
	})

	t.Run("duplicate namespace", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, podSecurityAdmissionMetadataList("", []any{
				podSecurityAdmissionMetadataObject("payments", nil, "2026-07-22T08:00:00Z"),
				podSecurityAdmissionMetadataObject("payments", nil, "2026-07-22T08:00:00Z"),
			}))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)

		if _, err := client.PodSecurityAdmissionNamespaces(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("PodSecurityAdmissionNamespaces() error = %v, want upstream error", err)
		}
	})

	t.Run("unsafe continuation", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, podSecurityAdmissionMetadataList("next\npage", []any{}))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)

		if _, err := client.PodSecurityAdmissionNamespaces(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("PodSecurityAdmissionNamespaces() error = %v, want upstream error", err)
		}
	})
}

func podSecurityAdmissionMetadataList(continueToken string, items []any) map[string]any {
	return map[string]any{
		"apiVersion": "meta.k8s.io/v1",
		"kind":       "PartialObjectMetadataList",
		"metadata":   map[string]any{"continue": continueToken},
		"items":      items,
	}
}

func podSecurityAdmissionMetadataObject(name string, labels map[string]any, createdAt string) map[string]any {
	metadata := map[string]any{
		"name":              name,
		"creationTimestamp": createdAt,
		"labels":            labels,
		"annotations":       map[string]string{"example.com/private": "annotation-secret"},
		"finalizers":        []string{"example.com/private-finalizer"},
		"managedFields":     []any{map[string]any{"manager": "private-controller"}},
		"uid":               "private-uid",
		"resourceVersion":   "private-version",
	}
	return map[string]any{
		"apiVersion": "meta.k8s.io/v1",
		"kind":       "PartialObjectMetadata",
		"metadata":   metadata,
	}
}
