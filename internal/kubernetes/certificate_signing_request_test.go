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

func TestClientListsCertificateSigningRequestsWithMetadataOnlyPagination(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requests := make([]string, 0, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != certificateSigningRequestCollectionPath {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Accept"); got != kubernetesPartialMetadataListAccept {
			t.Errorf("Accept = %q, want %q", got, kubernetesPartialMetadataListAccept)
		}
		if got := r.URL.Query().Get("limit"); got != certificateSigningRequestListPageSize {
			t.Errorf("limit = %q, want %q", got, certificateSigningRequestListPageSize)
		}
		mu.Lock()
		requests = append(requests, r.URL.RequestURI())
		mu.Unlock()

		if r.URL.Query().Get("continue") == "page-two" {
			writeTestJSON(t, w, accessMetadataList("", []any{
				accessMetadata("worker-02", "", "2026-07-29T09:00:00Z"),
			}))
			return
		}
		writeTestJSON(t, w, accessMetadataList("page-two", []any{
			accessMetadata("worker-01", "", "2026-07-30T09:00:00Z"),
		}))
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	items, err := client.CertificateSigningRequests(context.Background())
	if err != nil {
		t.Fatalf("CertificateSigningRequests() error = %v", err)
	}
	if len(items) != 2 || items[0].Name != "worker-01" || items[1].Name != "worker-02" ||
		items[0].CreatedAt.IsZero() || items[1].CreatedAt.IsZero() {
		t.Fatalf("CertificateSigningRequests() = %#v", items)
	}
	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		certificateSigningRequestCollectionPath + "?limit=250",
		certificateSigningRequestCollectionPath + "?continue=page-two&limit=250",
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

func TestClientReadsRedactedCertificateSigningRequestDetail(t *testing.T) {
	t.Parallel()

	const name = "worker-01"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != certificateSigningRequestCollectionPath+"/"+name {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		writeTestJSON(t, w, certificateSigningRequestPayload(name, map[string]any{
			"request":           "private-pkcs10",
			"signerName":        "kubernetes.io/kube-apiserver-client-kubelet",
			"expirationSeconds": 86400,
			"usages":            []any{"client auth", "digital signature"},
			"username":          "system:node:worker-01",
			"uid":               "private-requester-uid",
			"groups":            []any{"system:nodes", "private-group"},
			"extra":             map[string]any{"private-key": []any{"private-value"}},
		}, map[string]any{
			"certificate": "private-issued-certificate",
			"conditions": []any{map[string]any{
				"type": "Approved", "status": "True", "reason": "AutoApproved",
				"message": "private approval message", "lastUpdateTime": "2026-07-30T09:01:00Z",
				"lastTransitionTime": "2026-07-30T09:00:30Z",
			}},
		}))
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	detail, err := client.CertificateSigningRequest(context.Background(), name)
	if err != nil {
		t.Fatalf("CertificateSigningRequest() error = %v", err)
	}
	if detail.Name != name || detail.Requester != "system:node:worker-01" ||
		detail.SignerName != "kubernetes.io/kube-apiserver-client-kubelet" ||
		detail.RequestedExpirationSeconds == nil || *detail.RequestedExpirationSeconds != 86400 ||
		len(detail.Usages) != 2 || detail.Usages[0] != "client auth" || detail.Usages[1] != "digital signature" ||
		detail.State != domain.CertificateSigningRequestIssued || !detail.CertificateIssued ||
		detail.ConditionCount != 1 || len(detail.Conditions) != 1 ||
		detail.Conditions[0].Type != "Approved" || detail.Conditions[0].Status != "True" ||
		detail.Conditions[0].Reason != "AutoApproved" || detail.Conditions[0].LastUpdateTime == nil ||
		detail.Conditions[0].LastTransitionTime == nil || detail.CreatedAt.IsZero() {
		t.Fatalf("CertificateSigningRequest() = %#v", detail)
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal CertificateSigningRequest detail: %v", err)
	}
	for _, forbidden := range []string{
		"private-pkcs10", "private-issued-certificate", "private-requester-uid", "private-group",
		"private-key", "private-value", "private approval message", "private-label", "private-annotation",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("CertificateSigningRequest detail leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestDecodeCertificateSigningRequestStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		conditions  []any
		certificate string
		want        domain.KubernetesCertificateSigningRequestState
	}{
		{name: "pending", want: domain.CertificateSigningRequestPending},
		{name: "approved", conditions: []any{csrCondition("Approved", "Approved")}, want: domain.CertificateSigningRequestApproved},
		{name: "issued", conditions: []any{csrCondition("Approved", "Approved")}, certificate: "issued", want: domain.CertificateSigningRequestIssued},
		{name: "denied", conditions: []any{csrCondition("Denied", "Denied")}, want: domain.CertificateSigningRequestDenied},
		{name: "failed after approval", conditions: []any{csrCondition("Failed", "SignerFailed"), csrCondition("Approved", "Approved")}, want: domain.CertificateSigningRequestFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := certificateSigningRequestPayload("worker-01", map[string]any{
				"request": "private", "signerName": "example.com/node-client", "usages": []any{"client auth"},
				"username": "system:node:worker-01",
			}, map[string]any{"conditions": tt.conditions, "certificate": tt.certificate})
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal fixture: %v", err)
			}
			detail, err := decodeCertificateSigningRequest(encoded, "worker-01")
			if err != nil || detail.State != tt.want || detail.CertificateIssued != (tt.certificate != "") {
				t.Fatalf("decodeCertificateSigningRequest() = %#v, %v, want state %q", detail, err, tt.want)
			}
		})
	}
}

func TestClientValidatesCertificateSigningRequestNameBeforeRequest(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	for _, name := range []string{"", "../certificatesigningrequests", "Worker-01", strings.Repeat("a", 254)} {
		if _, err := client.CertificateSigningRequest(context.Background(), name); err == nil {
			t.Errorf("CertificateSigningRequest() accepted %q", name)
		}
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("invalid CertificateSigningRequest names made %d requests", got)
	}
}

func TestClientBoundsCertificateSigningRequestLists(t *testing.T) {
	t.Parallel()

	t.Run("page limit", func(t *testing.T) {
		var requests atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requestCount := requests.Add(1)
			writeTestJSON(t, w, accessMetadataList(strings.Repeat("p", int(requestCount)), []any{}))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)

		if _, err := client.CertificateSigningRequests(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CertificateSigningRequests() error = %v, want upstream error", err)
		}
		if got := requests.Load(); got != certificateSigningRequestMaxListPages {
			t.Fatalf("requests = %d, want %d", got, certificateSigningRequestMaxListPages)
		}
	})

	t.Run("item limit", func(t *testing.T) {
		items := make([]any, certificateSigningRequestMaxListItems+1)
		for index := range items {
			items[index] = accessMetadata("csr-"+strings.Repeat("a", 4)+string(rune('a'+index%26)), "", "2026-07-30T09:00:00Z")
		}
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, accessMetadataList("", items))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)

		if _, err := client.CertificateSigningRequests(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CertificateSigningRequests() error = %v, want upstream error", err)
		}
	})

	t.Run("repeated continuation token", func(t *testing.T) {
		var requests atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			writeTestJSON(t, w, accessMetadataList("same-token", []any{}))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)

		if _, err := client.CertificateSigningRequests(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CertificateSigningRequests() error = %v, want upstream error", err)
		}
		if got := requests.Load(); got != 2 {
			t.Fatalf("requests = %d, want 2", got)
		}
	})

	t.Run("detail bytes", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"padding":"` + strings.Repeat("x", int(certificateSigningRequestMaxDetailBytes)) + `"}`))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)

		if _, err := client.CertificateSigningRequest(context.Background(), "worker-01"); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("CertificateSigningRequest() error = %v, want upstream error", err)
		}
	})
}

func TestClientRejectsInvalidCertificateSigningRequestMetadataLists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response map[string]any
	}{
		{name: "wrong list kind", response: map[string]any{
			"apiVersion": "certificates.k8s.io/v1", "kind": "CertificateSigningRequestList", "items": []any{},
		}},
		{name: "namespaced item", response: accessMetadataList("", []any{
			accessMetadata("worker-01", "private", "2026-07-30T09:00:00Z"),
		})},
		{name: "duplicate item", response: accessMetadataList("", []any{
			accessMetadata("worker-01", "", "2026-07-30T09:00:00Z"),
			accessMetadata("worker-01", "", "2026-07-30T09:00:00Z"),
		})},
		{name: "full object fields", response: accessMetadataList("", []any{
			map[string]any{
				"apiVersion": "meta.k8s.io/v1", "kind": "PartialObjectMetadata",
				"metadata": map[string]any{"name": "worker-01", "creationTimestamp": "2026-07-30T09:00:00Z"},
				"spec":     map[string]any{"request": "private-pkcs10"},
			},
		})},
		{name: "unsafe continuation token", response: accessMetadataList("bad\nvalue", []any{})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeTestJSON(t, w, tt.response)
			}))
			t.Cleanup(server.Close)
			client := newNetworkTestClient(t, server)

			if _, err := client.CertificateSigningRequests(context.Background()); !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("CertificateSigningRequests() error = %v, want upstream error", err)
			}
		})
	}
}

func TestDecodeCertificateSigningRequestRejectsInvalidDetails(t *testing.T) {
	t.Parallel()

	validSpec := func() map[string]any {
		return map[string]any{
			"request": "private", "signerName": "example.com/node-client", "usages": []any{"client auth"},
			"username": "system:node:worker-01",
		}
	}
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "wrong api version", mutate: func(payload map[string]any) { payload["apiVersion"] = "certificates.k8s.io/v1beta1" }},
		{name: "wrong kind", mutate: func(payload map[string]any) { payload["kind"] = "CertificateSigningRequestList" }},
		{name: "wrong name", mutate: func(payload map[string]any) { payload["metadata"].(map[string]any)["name"] = "other" }},
		{name: "namespace", mutate: func(payload map[string]any) { payload["metadata"].(map[string]any)["namespace"] = "private" }},
		{name: "missing creation time", mutate: func(payload map[string]any) { delete(payload["metadata"].(map[string]any), "creationTimestamp") }},
		{name: "invalid signer", mutate: func(payload map[string]any) { payload["spec"].(map[string]any)["signerName"] = "not-qualified" }},
		{name: "unsafe requester", mutate: func(payload map[string]any) { payload["spec"].(map[string]any)["username"] = "user\nname" }},
		{name: "short expiration", mutate: func(payload map[string]any) { payload["spec"].(map[string]any)["expirationSeconds"] = 599 }},
		{name: "missing usages", mutate: func(payload map[string]any) { payload["spec"].(map[string]any)["usages"] = []any{} }},
		{name: "invalid usage", mutate: func(payload map[string]any) { payload["spec"].(map[string]any)["usages"] = []any{"private usage"} }},
		{name: "duplicate usage", mutate: func(payload map[string]any) {
			payload["spec"].(map[string]any)["usages"] = []any{"client auth", "client auth"}
		}},
		{name: "too many usages", mutate: func(payload map[string]any) {
			usages := make([]any, certificateSigningRequestMaxUsages+1)
			for index := range usages {
				usages[index] = "client auth"
			}
			payload["spec"].(map[string]any)["usages"] = usages
		}},
		{name: "duplicate condition", mutate: func(payload map[string]any) {
			payload["status"].(map[string]any)["conditions"] = []any{csrCondition("Approved", "A"), csrCondition("Approved", "B")}
		}},
		{name: "known condition false", mutate: func(payload map[string]any) {
			condition := csrCondition("Approved", "A")
			condition["status"] = "False"
			payload["status"].(map[string]any)["conditions"] = []any{condition}
		}},
		{name: "approved and denied", mutate: func(payload map[string]any) {
			payload["status"].(map[string]any)["conditions"] = []any{csrCondition("Approved", "A"), csrCondition("Denied", "D")}
		}},
		{name: "certificate without approval", mutate: func(payload map[string]any) {
			payload["status"].(map[string]any)["certificate"] = "issued"
		}},
		{name: "certificate with failure", mutate: func(payload map[string]any) {
			payload["status"].(map[string]any)["conditions"] = []any{csrCondition("Approved", "A"), csrCondition("Failed", "F")}
			payload["status"].(map[string]any)["certificate"] = "issued"
		}},
		{name: "non-string certificate", mutate: func(payload map[string]any) {
			payload["status"].(map[string]any)["certificate"] = 1
		}},
		{name: "unsafe condition reason", mutate: func(payload map[string]any) {
			condition := csrCondition("Approved", "bad\nreason")
			payload["status"].(map[string]any)["conditions"] = []any{condition}
		}},
		{name: "too many conditions", mutate: func(payload map[string]any) {
			conditions := make([]any, certificateSigningRequestMaxConditions+1)
			for index := range conditions {
				conditions[index] = csrCondition("Condition"+string(rune('A'+index)), "Observed")
			}
			payload["status"].(map[string]any)["conditions"] = conditions
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := certificateSigningRequestPayload("worker-01", validSpec(), map[string]any{"conditions": []any{}})
			tt.mutate(payload)
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal fixture: %v", err)
			}
			if _, err := decodeCertificateSigningRequest(encoded, "worker-01"); !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("decodeCertificateSigningRequest() error = %v, want upstream error", err)
			}
		})
	}
}

func certificateSigningRequestPayload(name string, spec, status map[string]any) map[string]any {
	return map[string]any{
		"apiVersion": "certificates.k8s.io/v1",
		"kind":       "CertificateSigningRequest",
		"metadata": map[string]any{
			"name": name, "creationTimestamp": "2026-07-30T09:00:00Z",
			"labels":      map[string]any{"private-label": "private-label-value"},
			"annotations": map[string]any{"private-annotation": "private-annotation-value"},
		},
		"spec": spec, "status": status,
	}
}

func csrCondition(conditionType, reason string) map[string]any {
	return map[string]any{
		"type": conditionType, "status": "True", "reason": reason,
		"lastUpdateTime": "2026-07-30T09:01:00Z", "lastTransitionTime": "2026-07-30T09:00:30Z",
	}
}
