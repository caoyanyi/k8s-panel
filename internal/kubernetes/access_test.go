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

func TestClientListsAccessResourcesWithMetadataOnlyPagination(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requests := make([]string, 0, 3)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != kubernetesPartialMetadataListAccept {
			t.Errorf("Accept = %q, want %q", got, kubernetesPartialMetadataListAccept)
		}
		if got := r.URL.Query().Get("limit"); got != accessListPageSize {
			t.Errorf("limit = %q, want %q", got, accessListPageSize)
		}
		mu.Lock()
		requests = append(requests, r.URL.RequestURI())
		mu.Unlock()

		switch r.URL.Path {
		case "/apis/rbac.authorization.k8s.io/v1/clusterroles":
			if r.URL.Query().Get("continue") == "page-two" {
				writeTestJSON(t, w, accessMetadataList("", []any{
					accessMetadata("admin", "", "2026-07-22T08:00:00Z"),
				}))
				return
			}
			writeTestJSON(t, w, accessMetadataList("page-two", []any{
				accessMetadata("view", "", "2026-07-24T08:00:00Z"),
			}))
		case "/apis/rbac.authorization.k8s.io/v1/namespaces/payments/rolebindings":
			writeTestJSON(t, w, accessMetadataList("", []any{
				accessMetadata("gateway-readers", "payments", "2026-07-25T08:00:00Z"),
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	clusterRoles, err := client.AccessResources(context.Background(), domain.AccessResourceClusterRoles, "")
	if err != nil {
		t.Fatalf("AccessResources(clusterroles) error = %v", err)
	}
	if len(clusterRoles) != 2 || clusterRoles[0].Name != "admin" || clusterRoles[0].Kind != "ClusterRole" ||
		clusterRoles[0].Namespace != "" || clusterRoles[1].Name != "view" || clusterRoles[1].CreatedAt.IsZero() {
		t.Fatalf("AccessResources(clusterroles) = %#v", clusterRoles)
	}
	bindings, err := client.AccessResources(context.Background(), domain.AccessResourceRoleBindings, "payments")
	if err != nil || len(bindings) != 1 || bindings[0].Name != "gateway-readers" || bindings[0].Namespace != "payments" ||
		bindings[0].Kind != "RoleBinding" {
		t.Fatalf("AccessResources(rolebindings) = %#v, %v", bindings, err)
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		"/apis/rbac.authorization.k8s.io/v1/clusterroles?limit=250",
		"/apis/rbac.authorization.k8s.io/v1/clusterroles?continue=page-two&limit=250",
		"/apis/rbac.authorization.k8s.io/v1/namespaces/payments/rolebindings?limit=250",
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

func TestClientReadsBoundedAccessResourceDetailsWithoutSecretNames(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		switch r.URL.Path {
		case "/apis/rbac.authorization.k8s.io/v1/namespaces/payments/roles/gateway-reader":
			writeTestJSON(t, w, map[string]any{
				"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "Role",
				"metadata": map[string]any{"name": "gateway-reader", "namespace": "payments", "creationTimestamp": "2026-07-24T08:00:00Z"},
				"rules": []any{
					map[string]any{"apiGroups": []string{"", "apps"}, "resources": []string{"pods", "deployments"}, "resourceNames": []string{"gateway"}, "verbs": []string{"get", "list"}},
					map[string]any{"nonResourceURLs": []string{"/healthz"}, "verbs": []string{"get"}},
				},
				"privateField": "must-not-be-projected",
			})
		case "/apis/rbac.authorization.k8s.io/v1/namespaces/payments/rolebindings/gateway-readers":
			writeTestJSON(t, w, map[string]any{
				"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "RoleBinding",
				"metadata": map[string]any{"name": "gateway-readers", "namespace": "payments", "creationTimestamp": "2026-07-25T08:00:00Z"},
				"roleRef":  map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": "Role", "name": "gateway-reader"},
				"subjects": []any{
					map[string]any{"kind": "ServiceAccount", "name": "gateway", "namespace": "payments"},
					map[string]any{"kind": "Group", "name": "payments-readers", "namespace": "ignored", "apiGroup": "rbac.authorization.k8s.io"},
				},
			})
		case "/api/v1/namespaces/payments/serviceaccounts/gateway":
			writeTestJSON(t, w, map[string]any{
				"apiVersion": "v1", "kind": "ServiceAccount",
				"metadata":                     map[string]any{"name": "gateway", "namespace": "payments", "creationTimestamp": "2026-07-23T08:00:00Z"},
				"automountServiceAccountToken": false,
				"secrets":                      []any{map[string]any{"name": "private-token-secret"}},
				"imagePullSecrets":             []any{map[string]any{"name": "private-registry-secret"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	role, err := client.AccessResourceDetail(context.Background(), domain.KubernetesAccessResourceReference{
		Kind: domain.AccessResourceRoles, Namespace: "payments", Name: "gateway-reader",
	})
	if err != nil || role.RuleCount != 2 || len(role.Rules) != 2 || role.RulesTruncated ||
		len(role.Rules[0].Resources) != 2 || role.Rules[1].NonResourceURLs[0] != "/healthz" {
		t.Fatalf("AccessResourceDetail(role) = %#v, %v", role, err)
	}
	binding, err := client.AccessResourceDetail(context.Background(), domain.KubernetesAccessResourceReference{
		Kind: domain.AccessResourceRoleBindings, Namespace: "payments", Name: "gateway-readers",
	})
	if err != nil || binding.RoleRef == nil || binding.RoleRef.Kind != "Role" || binding.RoleRef.Name != "gateway-reader" ||
		binding.SubjectCount != 2 || len(binding.Subjects) != 2 || binding.Subjects[0].Kind != "ServiceAccount" ||
		binding.Subjects[1].Namespace != "" {
		t.Fatalf("AccessResourceDetail(rolebinding) = %#v, %v", binding, err)
	}
	serviceAccount, err := client.AccessResourceDetail(context.Background(), domain.KubernetesAccessResourceReference{
		Kind: domain.AccessResourceServiceAccounts, Namespace: "payments", Name: "gateway",
	})
	if err != nil || serviceAccount.AutomountServiceAccountToken == nil || *serviceAccount.AutomountServiceAccountToken ||
		serviceAccount.SecretCount != 1 || serviceAccount.ImagePullSecretCount != 1 {
		t.Fatalf("AccessResourceDetail(serviceaccount) = %#v, %v", serviceAccount, err)
	}
	encoded, err := json.Marshal([]domain.KubernetesAccessResourceDetail{role, binding, serviceAccount})
	if err != nil {
		t.Fatalf("marshal access details: %v", err)
	}
	for _, forbidden := range []string{"private-token-secret", "private-registry-secret", "must-not-be-projected"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("access detail leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestClientValidatesAccessScopeBeforeRequest(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	invalidLists := []domain.KubernetesAccessResourceReference{
		{Kind: "secrets", Namespace: "payments"},
		{Kind: domain.AccessResourceRoles},
		{Kind: domain.AccessResourceRoleBindings, Namespace: "bad/namespace"},
		{Kind: domain.AccessResourceClusterRoles, Namespace: "payments"},
	}
	for _, reference := range invalidLists {
		if _, err := client.AccessResources(context.Background(), reference.Kind, reference.Namespace); err == nil {
			t.Errorf("AccessResources() accepted %#v", reference)
		}
	}
	invalidDetails := []domain.KubernetesAccessResourceReference{
		{Kind: domain.AccessResourceRoles, Namespace: "payments"},
		{Kind: domain.AccessResourceClusterRoleBindings, Name: "../binding"},
	}
	for _, reference := range invalidDetails {
		if _, err := client.AccessResourceDetail(context.Background(), reference); err == nil {
			t.Errorf("AccessResourceDetail() accepted %#v", reference)
		}
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("invalid access scopes made %d requests", got)
	}
}

func TestClientBoundsAndValidatesAccessResponses(t *testing.T) {
	t.Parallel()

	t.Run("page limit", func(t *testing.T) {
		var requests atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			writeTestJSON(t, w, accessMetadataList("more", []any{}))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)

		if _, err := client.AccessResources(context.Background(), domain.AccessResourceClusterRoles, ""); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("AccessResources() error = %v, want upstream error", err)
		}
		if got := requests.Load(); got != accessMaxListPages {
			t.Fatalf("requests = %d, want %d", got, accessMaxListPages)
		}
	})

	t.Run("scope mismatch", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, accessMetadataList("", []any{
				accessMetadata("reader", "default", "2026-07-24T08:00:00Z"),
			}))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)

		if _, err := client.AccessResources(context.Background(), domain.AccessResourceRoles, "payments"); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("AccessResources() error = %v, want upstream error", err)
		}
	})

	t.Run("bounded subjects", func(t *testing.T) {
		subjects := make([]any, maxAccessSubjects+1)
		for index := range subjects {
			subjects[index] = map[string]any{"kind": "Group", "name": "reader", "apiGroup": "rbac.authorization.k8s.io"}
		}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, map[string]any{
				"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "ClusterRoleBinding",
				"metadata": map[string]any{"name": "readers", "creationTimestamp": "2026-07-24T08:00:00Z"},
				"roleRef":  map[string]any{"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": "system:view"},
				"subjects": subjects,
			})
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)

		detail, err := client.AccessResourceDetail(context.Background(), domain.KubernetesAccessResourceReference{
			Kind: domain.AccessResourceClusterRoleBindings, Name: "readers",
		})
		if err != nil || detail.RoleRef == nil || detail.RoleRef.Name != "system:view" ||
			detail.SubjectCount != maxAccessSubjects+1 || len(detail.Subjects) != maxAccessSubjects || !detail.SubjectsTruncated {
			t.Fatalf("AccessResourceDetail() = %#v, %v", detail, err)
		}
	})
}

func accessMetadataList(continueToken string, items []any) map[string]any {
	return map[string]any{
		"apiVersion": "meta.k8s.io/v1",
		"kind":       "PartialObjectMetadataList",
		"metadata":   map[string]any{"continue": continueToken},
		"items":      items,
	}
}

func accessMetadata(name, namespace, createdAt string) map[string]any {
	metadata := map[string]any{"name": name, "creationTimestamp": createdAt}
	if namespace != "" {
		metadata["namespace"] = namespace
	}
	return map[string]any{
		"apiVersion": "meta.k8s.io/v1",
		"kind":       "PartialObjectMetadata",
		"metadata":   metadata,
	}
}
