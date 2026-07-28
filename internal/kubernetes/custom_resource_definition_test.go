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

func TestClientListsCustomResourceDefinitionsWithMetadataOnlyPagination(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requests := make([]string, 0, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != kubernetesPartialMetadataListAccept {
			t.Errorf("Accept = %q, want %q", got, kubernetesPartialMetadataListAccept)
		}
		if got := r.URL.Query().Get("limit"); got != customResourceDefinitionListPageSize {
			t.Errorf("limit = %q, want %q", got, customResourceDefinitionListPageSize)
		}
		mu.Lock()
		requests = append(requests, r.URL.RequestURI())
		mu.Unlock()

		if r.URL.Path != customResourceDefinitionCollectionPath {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("continue") == "page-two" {
			writeTestJSON(t, w, accessMetadataList("", []any{
				accessMetadata("widgets.platform.example.com", "", "2026-07-26T08:00:00Z"),
			}))
			return
		}
		writeTestJSON(t, w, accessMetadataList("page-two", []any{
			accessMetadata("certificates.cert-manager.io", "", "2026-07-25T08:00:00Z"),
		}))
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	items, err := client.CustomResourceDefinitions(context.Background())
	if err != nil {
		t.Fatalf("CustomResourceDefinitions() error = %v", err)
	}
	if len(items) != 2 || items[0].Name != "certificates.cert-manager.io" || items[0].Resource != "certificates" ||
		items[0].Group != "cert-manager.io" || items[1].Name != "widgets.platform.example.com" ||
		items[1].Resource != "widgets" || items[1].Group != "platform.example.com" || items[1].CreatedAt.IsZero() {
		t.Fatalf("CustomResourceDefinitions() = %#v", items)
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		customResourceDefinitionCollectionPath + "?limit=250",
		customResourceDefinitionCollectionPath + "?continue=page-two&limit=250",
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

func TestClientReadsBoundedCustomResourceDefinitionDetailWithoutSchemasOrWebhookConfig(t *testing.T) {
	t.Parallel()

	const name = "widgets.platform.example.com"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != customResourceDefinitionCollectionPath+"/"+name {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		writeTestJSON(t, w, map[string]any{
			"apiVersion": "apiextensions.k8s.io/v1",
			"kind":       "CustomResourceDefinition",
			"metadata": map[string]any{
				"name": name, "generation": 7, "creationTimestamp": "2026-07-26T08:00:00Z",
				"labels": map[string]any{"private-label": "must-not-be-projected"},
			},
			"spec": map[string]any{
				"group": "platform.example.com", "scope": "Namespaced",
				"names": map[string]any{
					"plural": "widgets", "singular": "widget", "kind": "Widget", "listKind": "WidgetList",
					"shortNames": []string{"wdg"}, "categories": []string{"all"},
				},
				"versions": []any{
					map[string]any{
						"name": "v1", "served": true, "storage": true,
						"schema": map[string]any{"openAPIV3Schema": map[string]any{
							"type": "object", "properties": map[string]any{"token": map[string]any{"default": "schema-secret"}},
						}},
					},
					map[string]any{
						"name": "v1beta1", "served": false, "storage": false, "deprecated": true,
						"deprecationWarning": "private deprecation warning",
					},
				},
				"conversion": map[string]any{
					"strategy": "Webhook",
					"webhook": map[string]any{"clientConfig": map[string]any{
						"url": "https://private-webhook.example.com", "caBundle": "private-ca-bundle",
					}},
				},
			},
			"status": map[string]any{
				"observedGeneration": 7,
				"storedVersions":     []string{"v1"},
				"conditions": []any{
					map[string]any{
						"type": "Established", "status": "True", "reason": "InitialNamesAccepted",
						"message": "private controller message", "observedGeneration": 7,
						"lastTransitionTime": "2026-07-26T08:01:00Z",
					},
				},
			},
		})
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	detail, err := client.CustomResourceDefinition(context.Background(), name)
	if err != nil {
		t.Fatalf("CustomResourceDefinition() error = %v", err)
	}
	if detail.Name != name || detail.Resource != "widgets" || detail.Group != "platform.example.com" ||
		detail.Scope != "Namespaced" || detail.Kind != "Widget" || detail.ListKind != "WidgetList" ||
		detail.Singular != "widget" || len(detail.ShortNames) != 1 || detail.ShortNames[0] != "wdg" ||
		len(detail.Categories) != 1 || detail.Categories[0] != "all" || detail.Generation != 7 ||
		detail.ObservedGeneration != 7 || detail.ConversionStrategy != "Webhook" ||
		detail.ConversionStrategyDefaulted || detail.VersionCount != 2 || len(detail.Versions) != 2 ||
		!detail.Versions[0].Served || !detail.Versions[0].Storage || !detail.Versions[1].Deprecated ||
		detail.StoredVersionCount != 1 || len(detail.StoredVersions) != 1 || detail.StoredVersions[0] != "v1" ||
		detail.ConditionCount != 1 || len(detail.Conditions) != 1 || detail.Conditions[0].Type != "Established" ||
		detail.Conditions[0].Status != "True" || detail.Conditions[0].Reason != "InitialNamesAccepted" ||
		detail.Conditions[0].ObservedGeneration != 7 || detail.Conditions[0].LastTransitionTime.IsZero() {
		t.Fatalf("CustomResourceDefinition() = %#v", detail)
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal CustomResourceDefinition detail: %v", err)
	}
	for _, forbidden := range []string{
		"schema-secret", "private-webhook.example.com", "private-ca-bundle", "private controller message",
		"private deprecation warning", "must-not-be-projected", "openAPIV3Schema", "clientConfig",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("CustomResourceDefinition detail leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestClientValidatesCustomResourceDefinitionNameBeforeRequest(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	for _, name := range []string{"", "widgets", "../customresourcedefinitions", "Widgets.platform.example.com"} {
		if _, err := client.CustomResourceDefinition(context.Background(), name); err == nil {
			t.Errorf("CustomResourceDefinition() accepted %q", name)
		}
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("invalid CRD names made %d requests", got)
	}
}

func TestClientBoundsAndValidatesCustomResourceDefinitionResponses(t *testing.T) {
	t.Parallel()

	t.Run("page limit", func(t *testing.T) {
		var requests atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			writeTestJSON(t, w, accessMetadataList("more", []any{}))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)

		if _, err := client.CustomResourceDefinitions(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CustomResourceDefinitions() error = %v, want upstream error", err)
		}
		if got := requests.Load(); got != customResourceDefinitionMaxListPages {
			t.Fatalf("requests = %d, want %d", got, customResourceDefinitionMaxListPages)
		}
	})

	t.Run("truncated versions", func(t *testing.T) {
		versions := make([]any, maxCustomResourceDefinitionVersions+1)
		for index := range versions {
			versions[index] = map[string]any{
				"name": versionName(index), "served": true, "storage": index == 0,
			}
		}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, customResourceDefinitionDetailResponse("widgets.platform.example.com", versions))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)

		detail, err := client.CustomResourceDefinition(context.Background(), "widgets.platform.example.com")
		if err != nil || detail.VersionCount != len(versions) || len(detail.Versions) != maxCustomResourceDefinitionVersions ||
			!detail.VersionsTruncated {
			t.Fatalf("CustomResourceDefinition() = %#v, %v", detail, err)
		}
	})

	t.Run("identity mismatch", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			response := customResourceDefinitionDetailResponse("gadgets.platform.example.com", []any{
				map[string]any{"name": "v1", "served": true, "storage": true},
			})
			writeTestJSON(t, w, response)
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)

		if _, err := client.CustomResourceDefinition(context.Background(), "widgets.platform.example.com"); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CustomResourceDefinition() error = %v, want upstream error", err)
		}
	})
}

func TestClientRejectsInvalidCustomResourceDefinitionMetadataLists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response map[string]any
	}{
		{
			name: "unexpected list type",
			response: map[string]any{
				"apiVersion": "apiextensions.k8s.io/v1", "kind": "CustomResourceDefinitionList", "items": []any{},
			},
		},
		{
			name:     "unsafe continuation token",
			response: accessMetadataList(strings.Repeat("x", customResourceDefinitionMaxContinueBytes+1), []any{}),
		},
		{
			name: "invalid metadata name",
			response: accessMetadataList("", []any{
				accessMetadata("widgets", "", "2026-07-26T08:00:00Z"),
			}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeTestJSON(t, w, tt.response)
			}))
			t.Cleanup(server.Close)
			client := newNetworkTestClient(t, server)

			if _, err := client.CustomResourceDefinitions(context.Background()); !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("CustomResourceDefinitions() error = %v, want upstream error", err)
			}
		})
	}
}

func TestDecodeCustomResourceDefinitionDefaultsAndRejectsUnsafeDetails(t *testing.T) {
	t.Parallel()

	const name = "widgets.platform.example.com"
	valid := customResourceDefinitionDetailResponse(name, []any{
		map[string]any{"name": "v1", "served": true, "storage": true},
	})
	names := valid["spec"].(map[string]any)["names"].(map[string]any)
	delete(names, "singular")
	delete(names, "listKind")
	detail := decodeCustomResourceDefinitionMap(t, valid, name)
	if detail.Singular != "widget" || detail.ListKind != "WidgetList" || detail.ConversionStrategy != "None" ||
		!detail.ConversionStrategyDefaulted {
		t.Fatalf("decodeCustomResourceDefinition() defaults = %#v", detail)
	}

	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{
			name: "nested entry limit",
			mutate: func(response map[string]any) {
				values := make([]string, maxCustomResourceDefinitionNestedEntries+1)
				for index := range values {
					values[index] = versionName(index)
				}
				response["spec"].(map[string]any)["names"].(map[string]any)["shortNames"] = values
			},
		},
		{
			name: "unknown conversion strategy",
			mutate: func(response map[string]any) {
				response["spec"].(map[string]any)["conversion"] = map[string]any{"strategy": "Unsafe"}
			},
		},
		{
			name: "future observed generation",
			mutate: func(response map[string]any) {
				response["status"].(map[string]any)["observedGeneration"] = 2
			},
		},
		{
			name: "duplicate version",
			mutate: func(response map[string]any) {
				response["spec"].(map[string]any)["versions"] = []any{
					map[string]any{"name": "v1", "served": true, "storage": true},
					map[string]any{"name": "v1", "served": false, "storage": false},
				}
			},
		},
		{
			name: "unknown stored version",
			mutate: func(response map[string]any) {
				response["status"].(map[string]any)["storedVersions"] = []string{"v2"}
			},
		},
		{
			name: "unsafe condition",
			mutate: func(response map[string]any) {
				response["status"].(map[string]any)["conditions"] = []any{
					map[string]any{
						"type": "Established", "status": "Maybe", "reason": "Unknown",
						"lastTransitionTime": "2026-07-26T08:01:00Z",
					},
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			response := customResourceDefinitionDetailResponse(name, []any{
				map[string]any{"name": "v1", "served": true, "storage": true},
			})
			tt.mutate(response)
			payload, err := json.Marshal(response)
			if err != nil {
				t.Fatalf("marshal CRD response: %v", err)
			}
			if _, err := decodeCustomResourceDefinition(payload, name); !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("decodeCustomResourceDefinition() error = %v, want upstream error", err)
			}
		})
	}
}

func customResourceDefinitionDetailResponse(name string, versions []any) map[string]any {
	return map[string]any{
		"apiVersion": "apiextensions.k8s.io/v1", "kind": "CustomResourceDefinition",
		"metadata": map[string]any{"name": name, "generation": 1, "creationTimestamp": "2026-07-26T08:00:00Z"},
		"spec": map[string]any{
			"group": "platform.example.com", "scope": "Namespaced", "versions": versions,
			"names": map[string]any{"plural": "widgets", "singular": "widget", "kind": "Widget", "listKind": "WidgetList"},
		},
		"status": map[string]any{"observedGeneration": 1, "storedVersions": []string{"v1"}, "conditions": []any{}},
	}
}

func versionName(index int) string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	if index < len(digits) {
		return "v" + string(digits[index])
	}
	return "v" + string(digits[index/len(digits)]) + string(digits[index%len(digits)])
}

func decodeCustomResourceDefinitionMap(
	t *testing.T,
	response map[string]any,
	name string,
) domain.KubernetesCustomResourceDefinitionDetail {
	t.Helper()
	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal CRD response: %v", err)
	}
	detail, err := decodeCustomResourceDefinition(payload, name)
	if err != nil {
		t.Fatalf("decodeCustomResourceDefinition() error = %v", err)
	}
	return detail
}
