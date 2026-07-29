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

func TestClientListsAdmissionWebhookConfigurationsWithMetadataOnlyPagination(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requests := make([]string, 0, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != kubernetesPartialMetadataListAccept {
			t.Errorf("Accept = %q, want %q", got, kubernetesPartialMetadataListAccept)
		}
		if got := r.URL.Query().Get("limit"); got != admissionWebhookListPageSize {
			t.Errorf("limit = %q, want %q", got, admissionWebhookListPageSize)
		}
		mu.Lock()
		requests = append(requests, r.URL.RequestURI())
		mu.Unlock()
		if r.URL.Path != validatingAdmissionWebhookCollectionPath {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("continue") == "page-two" {
			writeTestJSON(t, w, accessMetadataList("", []any{
				accessMetadata("alpha.platform.example.com", "", "2026-07-28T08:00:00Z"),
			}))
			return
		}
		writeTestJSON(t, w, accessMetadataList("page-two", []any{
			accessMetadata("zeta.platform.example.com", "", "2026-07-28T09:00:00Z"),
		}))
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	items, err := client.AdmissionWebhookConfigurations(context.Background(), domain.AdmissionWebhookConfigurationValidating)
	if err != nil {
		t.Fatalf("AdmissionWebhookConfigurations() error = %v", err)
	}
	if len(items) != 2 || items[0].Name != "alpha.platform.example.com" ||
		items[0].Kind != domain.AdmissionWebhookConfigurationValidating || items[0].CreatedAt.IsZero() ||
		items[1].Name != "zeta.platform.example.com" {
		t.Fatalf("AdmissionWebhookConfigurations() = %#v", items)
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		validatingAdmissionWebhookCollectionPath + "?limit=250",
		validatingAdmissionWebhookCollectionPath + "?continue=page-two&limit=250",
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

func TestClientReadsBoundedAdmissionWebhookDetailWithoutSensitiveConfiguration(t *testing.T) {
	t.Parallel()

	const name = "policy.platform.example.com"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != validatingAdmissionWebhookCollectionPath+"/"+name {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		writeTestJSON(t, w, admissionWebhookConfigurationResponse("ValidatingWebhookConfiguration", name, []any{
			map[string]any{
				"name": "validate.policy.platform.example.com",
				"clientConfig": map[string]any{
					"service": map[string]any{
						"namespace": "policy-system", "name": "policy-webhook", "path": "/private-admit-path",
					},
					"caBundle": "cHJpdmF0ZS1jYS1idW5kbGU=",
				},
				"sideEffects": "None", "admissionReviewVersions": []string{"v1", "v1beta1"},
				"rules": []any{
					map[string]any{
						"operations": []string{"CREATE", "UPDATE"}, "apiGroups": []string{"apps"},
						"apiVersions": []string{"v1"}, "resources": []string{"deployments", "statefulsets"},
					},
				},
				"namespaceSelector": map[string]any{
					"matchLabels":      map[string]any{"private-label": "private-selector-value"},
					"matchExpressions": []any{map[string]any{"key": "private-expression-key"}},
				},
				"objectSelector":  map[string]any{"matchLabels": map[string]any{"managed": "private-object-value"}},
				"matchConditions": []any{map[string]any{"name": "private-condition", "expression": "private CEL expression"}},
			},
			map[string]any{
				"name": "external.policy.platform.example.com",
				"clientConfig": map[string]any{
					"url": "https://private-webhook.platform.example.com/private/admit/never-project",
				},
				"failurePolicy": "Ignore", "matchPolicy": "Exact", "sideEffects": "NoneOnDryRun",
				"timeoutSeconds": 2, "admissionReviewVersions": []string{"v1"},
			},
		}))
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	detail, err := client.AdmissionWebhookConfiguration(
		context.Background(), domain.AdmissionWebhookConfigurationValidating, name,
	)
	if err != nil {
		t.Fatalf("AdmissionWebhookConfiguration() error = %v", err)
	}
	if detail.Name != name || detail.Kind != domain.AdmissionWebhookConfigurationValidating || detail.Generation != 3 ||
		detail.WebhookCount != 2 || len(detail.Webhooks) != 2 || detail.CreatedAt.IsZero() {
		t.Fatalf("AdmissionWebhookConfiguration() = %#v", detail)
	}
	service := detail.Webhooks[0]
	if service.TargetType != "service" || service.ServiceNamespace != "policy-system" ||
		service.ServiceName != "policy-webhook" || service.ServicePort != 443 || !service.ServicePortDefaulted ||
		!service.CABundleConfigured || service.FailurePolicy != "Fail" || !service.FailurePolicyDefaulted ||
		service.MatchPolicy != "Equivalent" || !service.MatchPolicyDefaulted || service.SideEffects != "None" ||
		service.TimeoutSeconds != 10 || !service.TimeoutSecondsDefaulted ||
		len(service.AdmissionReviewVersions) != 2 || service.RuleCount != 1 || service.OperationCount != 2 ||
		service.APIGroupCount != 1 || service.APIVersionCount != 1 || service.ResourceCount != 2 ||
		service.NamespaceSelectorLabelCount != 1 || service.NamespaceSelectorExpressionCount != 1 ||
		service.ObjectSelectorLabelCount != 1 || service.MatchConditionCount != 1 {
		t.Fatalf("service webhook = %#v", service)
	}
	external := detail.Webhooks[1]
	if external.TargetType != "url" || external.ServiceNamespace != "" || external.ServiceName != "" ||
		external.FailurePolicy != "Ignore" || external.FailurePolicyDefaulted || external.MatchPolicy != "Exact" ||
		external.MatchPolicyDefaulted || external.TimeoutSeconds != 2 || external.TimeoutSecondsDefaulted ||
		external.CABundleConfigured {
		t.Fatalf("external webhook = %#v", external)
	}

	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal admission webhook detail: %v", err)
	}
	for _, forbidden := range []string{
		"private-admit-path", "private-ca-bundle", "cHJpdmF0ZS1jYS1idW5kbGU=", "private-webhook.platform.example.com",
		"never-project", "private-label", "private-selector-value", "private-expression-key", "private-object-value",
		"private-condition", "private CEL expression", "deployments", "statefulsets",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("admission webhook detail leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestClientReadsMutatingAdmissionWebhookReinvocationPolicy(t *testing.T) {
	t.Parallel()

	const name = "mutate.platform.example.com"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != mutatingAdmissionWebhookCollectionPath+"/"+name {
			http.NotFound(w, r)
			return
		}
		writeTestJSON(t, w, admissionWebhookConfigurationResponse("MutatingWebhookConfiguration", name, []any{
			minimalAdmissionWebhook("default.mutate.platform.example.com"),
			func() map[string]any {
				webhook := minimalAdmissionWebhook("repeat.mutate.platform.example.com")
				webhook["reinvocationPolicy"] = "IfNeeded"
				return webhook
			}(),
		}))
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	detail, err := client.AdmissionWebhookConfiguration(
		context.Background(), domain.AdmissionWebhookConfigurationMutating, name,
	)
	if err != nil {
		t.Fatalf("AdmissionWebhookConfiguration() error = %v", err)
	}
	if detail.Kind != domain.AdmissionWebhookConfigurationMutating || detail.Webhooks[0].ReinvocationPolicy != "Never" ||
		!detail.Webhooks[0].ReinvocationPolicyDefaulted || detail.Webhooks[1].ReinvocationPolicy != "IfNeeded" ||
		detail.Webhooks[1].ReinvocationPolicyDefaulted {
		t.Fatalf("mutating admission webhook detail = %#v", detail)
	}
}

func TestClientValidatesAdmissionWebhookReferenceBeforeRequest(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	for _, kind := range []domain.KubernetesAdmissionWebhookConfigurationKind{"", "Validating", "unknown"} {
		if _, err := client.AdmissionWebhookConfigurations(context.Background(), kind); err == nil {
			t.Errorf("AdmissionWebhookConfigurations() accepted kind %q", kind)
		}
	}
	for _, name := range []string{"", "../validatingwebhookconfigurations", "Policy.platform.example.com"} {
		if _, err := client.AdmissionWebhookConfiguration(
			context.Background(), domain.AdmissionWebhookConfigurationValidating, name,
		); err == nil {
			t.Errorf("AdmissionWebhookConfiguration() accepted name %q", name)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("invalid admission webhook inputs made %d requests", requests.Load())
	}
}

func TestClientBoundsAdmissionWebhookResponses(t *testing.T) {
	t.Parallel()

	t.Run("list page limit", func(t *testing.T) {
		var requests atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			writeTestJSON(t, w, accessMetadataList("next-page", []any{}))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.AdmissionWebhookConfigurations(
			context.Background(), domain.AdmissionWebhookConfigurationValidating,
		); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("AdmissionWebhookConfigurations() error = %v, want upstream error", err)
		}
		if got := requests.Load(); got != admissionWebhookMaxListPages {
			t.Fatalf("requests = %d, want %d", got, admissionWebhookMaxListPages)
		}
	})

	t.Run("list item limit", func(t *testing.T) {
		items := make([]any, admissionWebhookMaxListItems+1)
		for index := range items {
			items[index] = accessMetadata(admissionWebhookTestName(index), "", "2026-07-28T08:00:00Z")
		}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, accessMetadataList("", items))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.AdmissionWebhookConfigurations(
			context.Background(), domain.AdmissionWebhookConfigurationValidating,
		); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("AdmissionWebhookConfigurations() error = %v, want upstream error", err)
		}
	})

	t.Run("detail bytes", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", int(admissionWebhookMaxDetailBytes)+1)))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)
		if _, err := client.AdmissionWebhookConfiguration(
			context.Background(), domain.AdmissionWebhookConfigurationValidating, "policy.platform.example.com",
		); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("AdmissionWebhookConfiguration() error = %v, want upstream error", err)
		}
	})
}

func TestClientRejectsInvalidAdmissionWebhookResponses(t *testing.T) {
	t.Parallel()

	t.Run("metadata lists", func(t *testing.T) {
		tests := []map[string]any{
			{"apiVersion": "admissionregistration.k8s.io/v1", "kind": "ValidatingWebhookConfigurationList", "items": []any{}},
			accessMetadataList(strings.Repeat("x", admissionWebhookMaxContinueBytes+1), []any{}),
			accessMetadataList("", []any{accessMetadata("Invalid.Name", "", "2026-07-28T08:00:00Z")}),
			accessMetadataList("", []any{accessMetadata("policy.platform.example.com", "unexpected", "2026-07-28T08:00:00Z")}),
		}
		for _, response := range tests {
			response := response
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeTestJSON(t, w, response)
			}))
			client := newNetworkTestClient(t, server)
			if _, err := client.AdmissionWebhookConfigurations(
				context.Background(), domain.AdmissionWebhookConfigurationValidating,
			); !errors.Is(err, domain.ErrUpstream) {
				server.Close()
				t.Fatalf("AdmissionWebhookConfigurations() error = %v, want upstream error", err)
			}
			server.Close()
		}
	})

	const name = "policy.platform.example.com"
	tests := []struct {
		name   string
		kind   domain.KubernetesAdmissionWebhookConfigurationKind
		mutate func(map[string]any)
	}{
		{name: "wrong identity", kind: domain.AdmissionWebhookConfigurationValidating, mutate: func(response map[string]any) { response["kind"] = "MutatingWebhookConfiguration" }},
		{name: "wrong name", kind: domain.AdmissionWebhookConfigurationValidating, mutate: func(response map[string]any) {
			response["metadata"].(map[string]any)["name"] = "other.platform.example.com"
		}},
		{name: "namespaced", kind: domain.AdmissionWebhookConfigurationValidating, mutate: func(response map[string]any) { response["metadata"].(map[string]any)["namespace"] = "default" }},
		{name: "no webhooks", kind: domain.AdmissionWebhookConfigurationValidating, mutate: func(response map[string]any) { response["webhooks"] = []any{} }},
		{name: "too many webhooks", kind: domain.AdmissionWebhookConfigurationValidating, mutate: func(response map[string]any) {
			webhooks := make([]any, maxAdmissionWebhooks+1)
			for index := range webhooks {
				webhooks[index] = minimalAdmissionWebhook(admissionWebhookTestName(index))
			}
			response["webhooks"] = webhooks
		}},
		{name: "duplicate webhook name", kind: domain.AdmissionWebhookConfigurationValidating, mutate: func(response map[string]any) {
			response["webhooks"] = []any{minimalAdmissionWebhook("same.platform.example.com"), minimalAdmissionWebhook("same.platform.example.com")}
		}},
		{name: "missing target", kind: domain.AdmissionWebhookConfigurationValidating, mutate: func(response map[string]any) { responseWebhook(response)["clientConfig"] = map[string]any{} }},
		{name: "multiple targets", kind: domain.AdmissionWebhookConfigurationValidating, mutate: func(response map[string]any) {
			responseWebhook(response)["clientConfig"] = map[string]any{
				"service": map[string]any{"namespace": "policy-system", "name": "policy"}, "url": "https://example.com/admit",
			}
		}},
		{name: "invalid service port", kind: domain.AdmissionWebhookConfigurationValidating, mutate: func(response map[string]any) {
			responseWebhook(response)["clientConfig"].(map[string]any)["service"].(map[string]any)["port"] = 70000
		}},
		{name: "invalid URL", kind: domain.AdmissionWebhookConfigurationValidating, mutate: func(response map[string]any) {
			responseWebhook(response)["clientConfig"] = map[string]any{"url": "http://example.com/admit"}
		}},
		{name: "invalid failure policy", kind: domain.AdmissionWebhookConfigurationValidating, mutate: func(response map[string]any) { responseWebhook(response)["failurePolicy"] = "Maybe" }},
		{name: "invalid match policy", kind: domain.AdmissionWebhookConfigurationValidating, mutate: func(response map[string]any) { responseWebhook(response)["matchPolicy"] = "Maybe" }},
		{name: "missing side effects", kind: domain.AdmissionWebhookConfigurationValidating, mutate: func(response map[string]any) { delete(responseWebhook(response), "sideEffects") }},
		{name: "invalid timeout", kind: domain.AdmissionWebhookConfigurationValidating, mutate: func(response map[string]any) { responseWebhook(response)["timeoutSeconds"] = 31 }},
		{name: "no review versions", kind: domain.AdmissionWebhookConfigurationValidating, mutate: func(response map[string]any) { responseWebhook(response)["admissionReviewVersions"] = []string{} }},
		{name: "duplicate review version", kind: domain.AdmissionWebhookConfigurationValidating, mutate: func(response map[string]any) {
			responseWebhook(response)["admissionReviewVersions"] = []string{"v1", "v1"}
		}},
		{name: "validating reinvocation policy", kind: domain.AdmissionWebhookConfigurationValidating, mutate: func(response map[string]any) { responseWebhook(response)["reinvocationPolicy"] = "Never" }},
		{name: "invalid mutating reinvocation policy", kind: domain.AdmissionWebhookConfigurationMutating, mutate: func(response map[string]any) { responseWebhook(response)["reinvocationPolicy"] = "Always" }},
		{name: "too many rules", kind: domain.AdmissionWebhookConfigurationValidating, mutate: func(response map[string]any) {
			responseWebhook(response)["rules"] = make([]any, maxAdmissionWebhookRules+1)
		}},
		{name: "too many match conditions", kind: domain.AdmissionWebhookConfigurationValidating, mutate: func(response map[string]any) {
			responseWebhook(response)["matchConditions"] = make([]any, maxAdmissionWebhookMatchConditions+1)
		}},
		{name: "too many selector labels", kind: domain.AdmissionWebhookConfigurationValidating, mutate: func(response map[string]any) {
			labels := make(map[string]any, maxAdmissionWebhookSelectorEntries+1)
			for index := 0; index <= maxAdmissionWebhookSelectorEntries; index++ {
				labels[admissionWebhookTestName(index)] = "private"
			}
			responseWebhook(response)["namespaceSelector"] = map[string]any{"matchLabels": labels}
		}},
		{name: "nested entry limit", kind: domain.AdmissionWebhookConfigurationValidating, mutate: func(response map[string]any) {
			webhooks := make([]any, maxAdmissionWebhooks)
			for webhookIndex := range webhooks {
				webhook := minimalAdmissionWebhook(admissionWebhookTestName(webhookIndex))
				rules := make([]any, maxAdmissionWebhookRules)
				for ruleIndex := range rules {
					rules[ruleIndex] = map[string]any{}
				}
				webhook["rules"] = rules
				webhooks[webhookIndex] = webhook
			}
			response["webhooks"] = webhooks
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			objectKind := "ValidatingWebhookConfiguration"
			if tt.kind == domain.AdmissionWebhookConfigurationMutating {
				objectKind = "MutatingWebhookConfiguration"
			}
			response := admissionWebhookConfigurationResponse(objectKind, name, []any{minimalAdmissionWebhook("validate.platform.example.com")})
			tt.mutate(response)
			payload, err := json.Marshal(response)
			if err != nil {
				t.Fatalf("marshal admission webhook response: %v", err)
			}
			if _, err := decodeAdmissionWebhookConfiguration(payload, tt.kind, name); !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("decodeAdmissionWebhookConfiguration() error = %v, want upstream error", err)
			}
		})
	}
}

func admissionWebhookConfigurationResponse(kind, name string, webhooks []any) map[string]any {
	return map[string]any{
		"apiVersion": "admissionregistration.k8s.io/v1", "kind": kind,
		"metadata": map[string]any{"name": name, "generation": 3, "creationTimestamp": "2026-07-28T08:00:00Z"},
		"webhooks": webhooks,
	}
}

func minimalAdmissionWebhook(name string) map[string]any {
	return map[string]any{
		"name":         name,
		"clientConfig": map[string]any{"service": map[string]any{"namespace": "policy-system", "name": "policy-webhook"}},
		"sideEffects":  "None", "admissionReviewVersions": []string{"v1"},
	}
}

func responseWebhook(response map[string]any) map[string]any {
	return response["webhooks"].([]any)[0].(map[string]any)
}

func admissionWebhookTestName(index int) string {
	return "webhook-" + strings.ToLower(string(rune('a'+index/26))) + strings.ToLower(string(rune('a'+index%26))) + ".platform.example.com"
}
