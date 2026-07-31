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

func TestClientReadsDeploymentRevisionHistoryFromMetadataOnlyPages(t *testing.T) {
	t.Parallel()

	type requestSnapshot struct {
		path         string
		accept       string
		limit        string
		continuation string
	}
	var mu sync.Mutex
	requests := make([]requestSnapshot, 0, 3)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, requestSnapshot{
			path: r.URL.Path, accept: r.Header.Get("Accept"), limit: r.URL.Query().Get("limit"),
			continuation: r.URL.Query().Get("continue"),
		})
		mu.Unlock()

		switch r.URL.Path {
		case "/apis/apps/v1/namespaces/payments/deployments/gateway":
			item := deploymentRevisionMetadata("Deployment", "payments", "gateway", "uid-gateway", "4", nil)
			metadata := item["metadata"].(map[string]any)
			metadata["labels"] = map[string]string{"private.example.com/tenant": "private-tenant"}
			metadata["managedFields"] = []any{map[string]any{"manager": "private-manager"}}
			writeTestJSON(t, w, item)
		case "/apis/apps/v1/namespaces/payments/replicasets":
			if r.URL.Query().Get("continue") == "page-two" {
				writeTestJSON(t, w, accessMetadataList("", []any{
					deploymentRevisionMetadata("ReplicaSet", "payments", "gateway-4", "uid-rs-4", "4", deploymentOwner("gateway", "uid-gateway")),
					deploymentRevisionMetadata("ReplicaSet", "payments", "gateway-2", "uid-rs-2", "2", deploymentOwner("gateway", "uid-gateway")),
				}))
				return
			}
			unassigned := deploymentRevisionMetadata("ReplicaSet", "payments", "gateway-pending", "uid-rs-pending", "", deploymentOwner("gateway", "uid-gateway"))
			unassigned["metadata"].(map[string]any)["annotations"] = map[string]string{"private.example.com/token": "private-value"}
			writeTestJSON(t, w, accessMetadataList("page-two", []any{
				deploymentRevisionMetadata("ReplicaSet", "payments", "gateway-1", "uid-rs-1", "1", deploymentOwner("gateway", "uid-gateway")),
				deploymentRevisionMetadata("ReplicaSet", "payments", "other-9", "uid-rs-other", "9", deploymentOwner("other", "uid-other")),
				deploymentRevisionMetadata("ReplicaSet", "payments", "gateway-old", "uid-rs-old", "8", deploymentOwner("gateway", "uid-old-deployment")),
				unassigned,
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	history, err := newNetworkTestClient(t, server).DeploymentRevisionHistory(context.Background(), domain.WorkloadReference{
		Kind: "deployment", Namespace: "payments", Name: "gateway",
	})
	if err != nil {
		t.Fatalf("DeploymentRevisionHistory() error = %v", err)
	}
	if history.Name != "gateway" || history.Namespace != "payments" || history.CurrentRevision != 4 ||
		history.UnassignedReplicaSetCount != 1 || history.Truncated || len(history.Revisions) != 3 {
		t.Fatalf("DeploymentRevisionHistory() = %#v", history)
	}
	wantRevisions := []int{4, 2, 1}
	for index, revision := range history.Revisions {
		if revision.Revision != wantRevisions[index] || revision.ReplicaSet == "" || revision.CreatedAt.IsZero() ||
			revision.Current != (revision.Revision == 4) {
			t.Fatalf("revision[%d] = %#v", index, revision)
		}
	}
	encoded, err := json.Marshal(history)
	if err != nil {
		t.Fatalf("marshal history: %v", err)
	}
	for _, forbidden := range []string{
		"uid-gateway", "uid-rs", "private-value", "private-manager", "private-tenant",
		"managedFields", "annotations", "labels", "ownerReferences", "change-cause",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("history leaked %q: %s", forbidden, encoded)
		}
	}

	mu.Lock()
	gotRequests := append([]requestSnapshot(nil), requests...)
	mu.Unlock()
	if len(gotRequests) != 3 {
		t.Fatalf("requests = %#v", gotRequests)
	}
	if gotRequests[0].path != "/apis/apps/v1/namespaces/payments/deployments/gateway" ||
		gotRequests[0].accept != "application/json;as=PartialObjectMetadata;g=meta.k8s.io;v=v1" {
		t.Fatalf("deployment request = %#v", gotRequests[0])
	}
	for index, request := range gotRequests[1:] {
		if request.path != "/apis/apps/v1/namespaces/payments/replicasets" ||
			request.accept != kubernetesPartialMetadataListAccept || request.limit != "250" {
			t.Fatalf("ReplicaSet request[%d] = %#v", index, request)
		}
	}
	if gotRequests[1].continuation != "" || gotRequests[2].continuation != "page-two" {
		t.Fatalf("continuations = %#v", gotRequests)
	}
}

func TestClientHandlesUnrecordedAndTruncatedDeploymentRevisions(t *testing.T) {
	t.Parallel()

	items := make([]any, 0, 22)
	for revision := 1; revision <= 21; revision++ {
		items = append(items, deploymentRevisionMetadata(
			"ReplicaSet", "payments", fmt.Sprintf("gateway-%d", revision), fmt.Sprintf("uid-rs-%d", revision),
			fmt.Sprint(revision), deploymentOwner("gateway", "uid-gateway"),
		))
	}
	items = append(items, deploymentRevisionMetadata(
		"ReplicaSet", "payments", "gateway-pending", "uid-rs-pending", "", deploymentOwner("gateway", "uid-gateway"),
	))
	server := deploymentRevisionServer(t,
		deploymentRevisionMetadata("Deployment", "payments", "gateway", "uid-gateway", "", nil),
		accessMetadataList("", items),
	)

	history, err := newNetworkTestClient(t, server).DeploymentRevisionHistory(context.Background(), domain.WorkloadReference{
		Kind: "Deployment", Namespace: "payments", Name: "gateway",
	})
	if err != nil {
		t.Fatalf("DeploymentRevisionHistory() error = %v", err)
	}
	if history.CurrentRevision != 0 || history.UnassignedReplicaSetCount != 1 || !history.Truncated ||
		len(history.Revisions) != 20 || history.Revisions[0].Revision != 21 || history.Revisions[19].Revision != 2 {
		t.Fatalf("DeploymentRevisionHistory() = %#v", history)
	}
	for _, revision := range history.Revisions {
		if revision.Current {
			t.Fatalf("revision was marked current without a Deployment revision: %#v", revision)
		}
	}
}

func TestClientReturnsEmptyDeploymentRevisionHistoryAsArray(t *testing.T) {
	t.Parallel()

	server := deploymentRevisionServer(t,
		deploymentRevisionMetadata("Deployment", "payments", "gateway", "uid-gateway", "1", nil),
		accessMetadataList("", []any{}),
	)
	history, err := newNetworkTestClient(t, server).DeploymentRevisionHistory(context.Background(), domain.WorkloadReference{
		Kind: "deployment", Namespace: "payments", Name: "gateway",
	})
	if err != nil {
		t.Fatalf("DeploymentRevisionHistory() error = %v", err)
	}
	if history.Revisions == nil || len(history.Revisions) != 0 || history.Truncated {
		t.Fatalf("DeploymentRevisionHistory() = %#v", history)
	}
	encoded, err := json.Marshal(history)
	if err != nil {
		t.Fatalf("marshal history: %v", err)
	}
	if !strings.Contains(string(encoded), `"revisions":[]`) {
		t.Fatalf("empty history is not an array: %s", encoded)
	}
}

func TestClientRejectsInvalidDeploymentRevisionHistoryInputBeforeRequest(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)
	for _, input := range []struct {
		reference domain.WorkloadReference
		field     string
	}{
		{reference: domain.WorkloadReference{Kind: "statefulset", Namespace: "payments", Name: "gateway"}, field: "kind"},
		{reference: domain.WorkloadReference{Kind: "deployment", Namespace: "PAYMENTS", Name: "gateway"}, field: "namespace"},
		{reference: domain.WorkloadReference{Kind: "deployment", Namespace: "payments", Name: "../gateway"}, field: "name"},
	} {
		_, err := client.DeploymentRevisionHistory(context.Background(), input.reference)
		var validationErr *domain.ValidationError
		if !errors.As(err, &validationErr) || validationErr.Field != input.field {
			t.Errorf("DeploymentRevisionHistory(%#v) error = %v", input.reference, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid inputs made %d upstream requests", calls.Load())
	}
}

func TestClientRejectsUnsafeDeploymentRevisionMetadata(t *testing.T) {
	t.Parallel()

	validDeployment := func() map[string]any {
		return deploymentRevisionMetadata("Deployment", "payments", "gateway", "uid-gateway", "4", nil)
	}
	validReplicaSet := func() map[string]any {
		return deploymentRevisionMetadata(
			"ReplicaSet", "payments", "gateway-4", "uid-rs-4", "4", deploymentOwner("gateway", "uid-gateway"),
		)
	}
	tests := []struct {
		name       string
		deployment func() any
		list       func() any
	}{
		{name: "full Deployment response", deployment: func() any {
			item := validDeployment()
			item["apiVersion"] = "apps/v1"
			item["kind"] = "Deployment"
			item["spec"] = map[string]any{"template": map[string]any{"private": "value"}}
			return item
		}},
		{name: "wrong Deployment namespace", deployment: func() any {
			item := validDeployment()
			item["metadata"].(map[string]any)["namespace"] = "other"
			return item
		}},
		{name: "missing Deployment UID", deployment: func() any {
			item := validDeployment()
			delete(item["metadata"].(map[string]any), "uid")
			return item
		}},
		{name: "non-canonical Deployment revision", deployment: func() any {
			item := validDeployment()
			item["metadata"].(map[string]any)["annotations"].(map[string]string)["deployment.kubernetes.io/revision"] = "04"
			return item
		}},
		{name: "wrong ReplicaSet list identity", list: func() any {
			return map[string]any{"apiVersion": "apps/v1", "kind": "ReplicaSetList", "items": []any{}}
		}},
		{name: "ReplicaSet contains spec", list: func() any {
			item := validReplicaSet()
			item["spec"] = map[string]any{"private": "value"}
			return accessMetadataList("", []any{item})
		}},
		{name: "wrong ReplicaSet namespace", list: func() any {
			item := validReplicaSet()
			item["metadata"].(map[string]any)["namespace"] = "other"
			return accessMetadataList("", []any{item})
		}},
		{name: "unsafe ReplicaSet name", list: func() any {
			item := validReplicaSet()
			item["metadata"].(map[string]any)["name"] = "../private"
			return accessMetadataList("", []any{item})
		}},
		{name: "invalid ReplicaSet revision", list: func() any {
			item := validReplicaSet()
			item["metadata"].(map[string]any)["annotations"].(map[string]string)["deployment.kubernetes.io/revision"] = "+4"
			return accessMetadataList("", []any{item})
		}},
		{name: "overflow ReplicaSet revision", list: func() any {
			item := validReplicaSet()
			item["metadata"].(map[string]any)["annotations"].(map[string]string)["deployment.kubernetes.io/revision"] = "2147483648"
			return accessMetadataList("", []any{item})
		}},
		{name: "duplicate revision", list: func() any {
			return accessMetadataList("", []any{
				validReplicaSet(),
				deploymentRevisionMetadata("ReplicaSet", "payments", "gateway-other", "uid-rs-other", "4", deploymentOwner("gateway", "uid-gateway")),
			})
		}},
		{name: "multiple controller owners", list: func() any {
			item := validReplicaSet()
			item["metadata"].(map[string]any)["ownerReferences"] = append(
				deploymentOwner("gateway", "uid-gateway"), deploymentOwner("other", "uid-other")[0],
			)
			return accessMetadataList("", []any{item})
		}},
		{name: "too many owner references", list: func() any {
			item := validReplicaSet()
			owners := make([]any, 17)
			for index := range owners {
				owners[index] = map[string]any{
					"apiVersion": "apps/v1", "kind": "Deployment", "name": fmt.Sprintf("owner-%d", index),
					"uid": fmt.Sprintf("uid-owner-%d", index), "controller": false,
				}
			}
			item["metadata"].(map[string]any)["ownerReferences"] = owners
			return accessMetadataList("", []any{item})
		}},
		{name: "too many annotations", list: func() any {
			item := validReplicaSet()
			annotations := item["metadata"].(map[string]any)["annotations"].(map[string]string)
			for index := 0; index < 256; index++ {
				annotations[fmt.Sprintf("private.example.com/%d", index)] = "value"
			}
			return accessMetadataList("", []any{item})
		}},
		{name: "unsafe continuation", list: func() any {
			return accessMetadataList("next\npage", []any{})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			deployment := validDeployment()
			if tt.deployment != nil {
				deployment = tt.deployment().(map[string]any)
			}
			list := any(accessMetadataList("", []any{validReplicaSet()}))
			if tt.list != nil {
				list = tt.list()
			}
			server := deploymentRevisionServer(t, deployment, list)
			_, err := newNetworkTestClient(t, server).DeploymentRevisionHistory(context.Background(), domain.WorkloadReference{
				Kind: "deployment", Namespace: "payments", Name: "gateway",
			})
			if !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("DeploymentRevisionHistory() error = %v, want upstream error", err)
			}
		})
	}
}

func TestClientRejectsRepeatedDeploymentRevisionContinuation(t *testing.T) {
	t.Parallel()

	server := deploymentRevisionServer(t,
		deploymentRevisionMetadata("Deployment", "payments", "gateway", "uid-gateway", "1", nil),
		accessMetadataList("same-token", []any{}),
	)
	_, err := newNetworkTestClient(t, server).DeploymentRevisionHistory(context.Background(), domain.WorkloadReference{
		Kind: "deployment", Namespace: "payments", Name: "gateway",
	})
	if !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("DeploymentRevisionHistory() error = %v, want repeated continuation error", err)
	}
}

func TestClientBoundsDeploymentRevisionPagesItemsAndBodies(t *testing.T) {
	t.Run("Deployment body", func(t *testing.T) {
		var listCalls atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/deployments/gateway") {
				_, _ = w.Write([]byte(strings.Repeat("x", 256*1024+1)))
				return
			}
			listCalls.Add(1)
		}))
		t.Cleanup(server.Close)
		_, err := newNetworkTestClient(t, server).DeploymentRevisionHistory(context.Background(), domain.WorkloadReference{
			Kind: "deployment", Namespace: "payments", Name: "gateway",
		})
		if !errors.Is(err, domain.ErrUpstream) || listCalls.Load() != 0 {
			t.Fatalf("DeploymentRevisionHistory() error = %v, list calls = %d", err, listCalls.Load())
		}
	})

	t.Run("pages", func(t *testing.T) {
		var listCalls atomic.Int64
		deployment := deploymentRevisionMetadata("Deployment", "payments", "gateway", "uid-gateway", "1", nil)
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/deployments/gateway") {
				writeTestJSON(t, w, deployment)
				return
			}
			call := listCalls.Add(1)
			writeTestJSON(t, w, accessMetadataList(fmt.Sprintf("page-%d", call), []any{}))
		}))
		t.Cleanup(server.Close)
		_, err := newNetworkTestClient(t, server).DeploymentRevisionHistory(context.Background(), domain.WorkloadReference{
			Kind: "deployment", Namespace: "payments", Name: "gateway",
		})
		if !errors.Is(err, domain.ErrUpstream) || listCalls.Load() != 4 {
			t.Fatalf("DeploymentRevisionHistory() error = %v, list calls = %d", err, listCalls.Load())
		}
	})

	t.Run("items", func(t *testing.T) {
		items := make([]any, 1001)
		for index := range items {
			items[index] = deploymentRevisionMetadata(
				"ReplicaSet", "payments", fmt.Sprintf("other-%d", index), fmt.Sprintf("uid-other-%d", index),
				"", deploymentOwner("other", "uid-other"),
			)
		}
		server := deploymentRevisionServer(t,
			deploymentRevisionMetadata("Deployment", "payments", "gateway", "uid-gateway", "1", nil),
			accessMetadataList("", items),
		)
		_, err := newNetworkTestClient(t, server).DeploymentRevisionHistory(context.Background(), domain.WorkloadReference{
			Kind: "deployment", Namespace: "payments", Name: "gateway",
		})
		if !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("DeploymentRevisionHistory() error = %v, want item limit error", err)
		}
	})

	t.Run("body", func(t *testing.T) {
		deployment := deploymentRevisionMetadata("Deployment", "payments", "gateway", "uid-gateway", "1", nil)
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/deployments/gateway") {
				writeTestJSON(t, w, deployment)
				return
			}
			_, _ = w.Write([]byte(strings.Repeat("x", 2*1024*1024+1)))
		}))
		t.Cleanup(server.Close)
		_, err := newNetworkTestClient(t, server).DeploymentRevisionHistory(context.Background(), domain.WorkloadReference{
			Kind: "deployment", Namespace: "payments", Name: "gateway",
		})
		if !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("DeploymentRevisionHistory() error = %v, want byte limit error", err)
		}
	})
}

func deploymentRevisionServer(t *testing.T, deployment, list any) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/deployments/gateway") {
			writeTestJSON(t, w, deployment)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/replicasets") {
			writeTestJSON(t, w, list)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func deploymentRevisionMetadata(
	kind, namespace, name, uid, revision string,
	owners []any,
) map[string]any {
	annotations := map[string]string{"private.example.com/change-cause": "private-change"}
	if revision != "" {
		annotations["deployment.kubernetes.io/revision"] = revision
	}
	metadata := map[string]any{
		"name": name, "namespace": namespace, "uid": uid,
		"creationTimestamp": "2026-07-30T09:04:00Z", "annotations": annotations,
	}
	if owners != nil {
		metadata["ownerReferences"] = owners
	}
	return map[string]any{
		"apiVersion": "meta.k8s.io/v1", "kind": "PartialObjectMetadata", "metadata": metadata,
	}
}

func deploymentOwner(name, uid string) []any {
	return []any{map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment", "name": name, "uid": uid,
		"controller": true, "blockOwnerDeletion": true,
	}}
}
