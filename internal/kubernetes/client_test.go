package kubernetes

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/domain"
	"github.com/caoyanyi/k8s-panel/internal/outbound"
)

func TestClientReadsProbeNamespacesAndWorkloads(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requested := make([]string, 0)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mu.Lock()
		requested = append(requested, r.URL.RequestURI())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/version":
			writeTestJSON(t, w, map[string]any{"gitVersion": "v1.36.2"})
		case "/api/v1/namespaces":
			if r.URL.Query().Get("continue") == "page-two" {
				writeTestJSON(t, w, map[string]any{
					"metadata": map[string]any{"continue": ""},
					"items": []any{
						map[string]any{
							"metadata": map[string]any{
								"name": "payments", "creationTimestamp": "2026-07-23T08:00:00Z",
								"labels": map[string]string{"team": "payments"},
							},
							"spec":   map[string]any{"finalizers": []string{"kubernetes"}},
							"status": map[string]any{"phase": "Active"},
						},
					},
				})
				return
			}
			writeTestJSON(t, w, map[string]any{
				"metadata": map[string]any{"continue": "page-two"},
				"items": []any{
					map[string]any{
						"metadata": map[string]any{"name": "default", "creationTimestamp": "2026-07-22T08:00:00Z"},
						"status":   map[string]any{"phase": "Active"},
					},
				},
			})
		case "/api/v1/nodes":
			writeTestJSON(t, w, map[string]any{
				"items": []any{
					map[string]any{"metadata": map[string]any{"name": "node-a"}},
					map[string]any{"metadata": map[string]any{"name": "node-b"}},
				},
			})
		case "/apis/apps/v1/namespaces/payments/deployments":
			writeTestJSON(t, w, map[string]any{
				"items": []any{
					map[string]any{
						"metadata": map[string]any{
							"name": "gateway", "namespace": "payments", "creationTimestamp": "2026-07-24T08:00:00Z",
						},
						"spec": map[string]any{
							"replicas": 3,
							"template": map[string]any{"spec": map[string]any{"containers": []any{
								map[string]any{"name": "app", "image": "registry.example.com/gateway:1.4.0"},
							}}},
						},
						"status": map[string]any{"readyReplicas": 2, "availableReplicas": 2},
					},
				},
			})
		case "/apis/apps/v1/namespaces/payments/statefulsets", "/apis/apps/v1/namespaces/payments/daemonsets", "/api/v1/namespaces/payments/pods":
			writeTestJSON(t, w, map[string]any{"items": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	policy := loopbackPolicy(t)
	client, err := NewClient(Connection{
		Server:      server.URL,
		CACert:      string(certificate),
		BearerToken: "test-token",
	}, policy)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	probe, err := client.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if probe.Version != "v1.36.2" || probe.NamespaceCount != 2 || probe.NodeCount != 2 {
		t.Errorf("Probe() = %#v", probe)
	}

	namespaces, err := client.Namespaces(context.Background())
	if err != nil {
		t.Fatalf("Namespaces() error = %v", err)
	}
	if len(namespaces) != 2 || namespaces[1].Name != "payments" {
		t.Fatalf("Namespaces() = %#v", namespaces)
	}
	if namespaces[0].Labels == nil || namespaces[0].Finalizers == nil {
		t.Errorf("empty namespace metadata must use JSON-safe collections: %#v", namespaces[0])
	}
	if namespaces[1].Labels["team"] != "payments" || len(namespaces[1].Finalizers) != 1 {
		t.Errorf("namespace metadata = %#v", namespaces[1])
	}

	workloads, err := client.Workloads(context.Background(), "payments", "")
	if err != nil {
		t.Fatalf("Workloads() error = %v", err)
	}
	if len(workloads) != 1 {
		t.Fatalf("len(Workloads()) = %d, want 1", len(workloads))
	}
	got := workloads[0]
	if got.Kind != "Deployment" || got.Name != "gateway" || got.Ready != 2 || got.Desired != 3 || got.Status != "Progressing" {
		t.Errorf("workload = %#v", got)
	}
	if len(got.Images) != 1 || got.Images[0] != "registry.example.com/gateway:1.4.0" {
		t.Errorf("images = %#v", got.Images)
	}

	mu.Lock()
	joinedRequests := strings.Join(requested, "\n")
	mu.Unlock()
	if !strings.Contains(joinedRequests, "continue=page-two") {
		t.Errorf("pagination continuation was not requested:\n%s", joinedRequests)
	}
}

func TestClientRejectsInvalidCA(t *testing.T) {
	t.Parallel()

	_, err := NewClient(Connection{
		Server:      "https://api.example.com",
		CACert:      "not a certificate",
		BearerToken: "token",
	}, loopbackPolicy(t))
	if err == nil {
		t.Fatal("NewClient() accepted invalid CA")
	}
}

func TestNewClientBoundsInitialDNSResolution(t *testing.T) {
	t.Parallel()

	policy := outbound.NewPolicy(deadlineRequiredResolver{}, nil)
	if _, err := NewClient(Connection{
		Server:      "https://api.example.com",
		BearerToken: "token",
	}, policy); err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
}

func TestNewClientContextHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewClientContext(ctx, Connection{
		Server: "https://api.example.com", BearerToken: "token",
	}, outbound.NewPolicy(deadlineRequiredResolver{}, nil)); !errors.Is(err, context.Canceled) {
		t.Fatalf("NewClientContext() error = %v, want context canceled", err)
	}
}

func TestClientReadsSanitizedWorkloadDetailEventsAndPodLogs(t *testing.T) {
	t.Parallel()

	var eventSelector string
	var logQuery string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/namespaces/payments/pods/gateway-0":
			writeTestJSON(t, w, map[string]any{
				"apiVersion": "v1",
				"kind":       "Pod",
				"metadata": map[string]any{
					"name": "gateway-0", "namespace": "payments", "uid": "uid-gateway-0", "resourceVersion": "42",
					"creationTimestamp": "2026-07-24T08:00:00Z",
					"labels":            map[string]string{"app": "gateway"},
					"annotations": map[string]string{
						"kubectl.kubernetes.io/last-applied-configuration": `{"token":"annotation-secret"}`,
						"example.com/owner": "payments-team",
					},
					"managedFields": []any{map[string]any{"manager": "kubectl"}},
				},
				"spec": map[string]any{
					"containers": []any{map[string]any{
						"name": "app", "image": "registry.example.com/gateway:1.4.0",
						"command": []any{"/bin/sh", "-c"}, "args": []any{"start --password=command-secret"},
						"env": []any{
							map[string]any{"name": "PUBLIC_MODE", "value": "literal-secret"},
							map[string]any{"name": "TOKEN", "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "gateway", "key": "token"}}},
						},
					}},
					"initContainers": []any{map[string]any{"name": "migrate", "image": "registry.example.com/migrate:1.0.0"}},
				},
				"status": map[string]any{
					"phase": "Running",
					"containerStatuses": []any{map[string]any{
						"name": "app", "ready": true, "restartCount": 2, "state": map[string]any{"running": map[string]any{}},
					}},
					"initContainerStatuses": []any{map[string]any{
						"name": "migrate", "ready": true, "restartCount": 0, "state": map[string]any{"terminated": map[string]any{"reason": "Completed"}},
					}},
					"conditions": []any{map[string]any{
						"type": "Ready", "status": "True", "reason": "ContainersReady", "lastTransitionTime": "2026-07-24T08:01:00Z",
					}},
				},
			})
		case "/api/v1/namespaces/payments/events":
			eventSelector = r.URL.Query().Get("fieldSelector")
			writeTestJSON(t, w, map[string]any{"items": []any{
				map[string]any{
					"metadata": map[string]any{"name": "gateway-old", "creationTimestamp": "2026-07-24T08:00:00Z"},
					"type":     "Normal", "reason": "Scheduled", "message": "Assigned pod", "count": 1,
					"source": map[string]any{"component": "default-scheduler"}, "lastTimestamp": "2026-07-24T08:00:01Z",
				},
				map[string]any{
					"metadata": map[string]any{"name": "gateway-new", "creationTimestamp": "2026-07-24T08:02:00Z"},
					"type":     "Warning", "reason": "BackOff", "message": "Back-off restarting container", "count": 3,
					"reportingComponent": "kubelet", "eventTime": "2026-07-24T08:03:00Z",
				},
			}})
		case "/api/v1/namespaces/payments/pods/gateway-0/log":
			logQuery = r.URL.RawQuery
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("2026-07-24T08:04:00Z ready\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := NewClient(Connection{Server: server.URL, CACert: string(certificate), BearerToken: "test-token"}, loopbackPolicy(t))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	reference := domain.WorkloadReference{Kind: "pod", Namespace: "payments", Name: "gateway-0"}

	detail, err := client.WorkloadDetail(context.Background(), reference)
	if err != nil {
		t.Fatalf("WorkloadDetail() error = %v", err)
	}
	if detail.UID != "uid-gateway-0" || detail.ResourceVersion != "42" || detail.Status != "Ready" {
		t.Errorf("detail = %#v", detail)
	}
	if len(detail.Containers) != 2 || detail.Containers[0].Name != "app" || !detail.Containers[0].Ready || detail.Containers[1].Type != "init" {
		t.Errorf("containers = %#v", detail.Containers)
	}
	for _, forbidden := range []string{"literal-secret", "annotation-secret", "command-secret", "managedFields", "last-applied-configuration", "status:"} {
		if strings.Contains(detail.YAML, forbidden) {
			t.Errorf("sanitized YAML contains %q:\n%s", forbidden, detail.YAML)
		}
	}
	if !strings.Contains(detail.YAML, "<redacted>") || !strings.Contains(detail.YAML, "app: gateway") {
		t.Errorf("sanitized YAML is missing safe fields or redaction:\n%s", detail.YAML)
	}

	events, err := client.WorkloadEvents(context.Background(), reference, 10)
	if err != nil {
		t.Fatalf("WorkloadEvents() error = %v", err)
	}
	if len(events) != 2 || events[0].Reason != "BackOff" || events[0].LastSeen != time.Date(2026, 7, 24, 8, 3, 0, 0, time.UTC) {
		t.Errorf("events = %#v", events)
	}
	if !strings.Contains(eventSelector, "involvedObject.kind=Pod") || !strings.Contains(eventSelector, "involvedObject.name=gateway-0") {
		t.Errorf("event fieldSelector = %q", eventSelector)
	}

	logs, err := client.PodLogs(context.Background(), domain.PodLogRequest{
		Namespace: "payments", Pod: "gateway-0", Container: "app", TailLines: 250, Previous: true, Timestamps: true,
	})
	if err != nil {
		t.Fatalf("PodLogs() error = %v", err)
	}
	if logs.Content != "2026-07-24T08:04:00Z ready\n" || logs.Container != "app" || logs.TailLines != 250 {
		t.Errorf("logs = %#v", logs)
	}
	for _, queryPart := range []string{"container=app", "tailLines=250", "previous=true", "timestamps=true"} {
		if !strings.Contains(logQuery, queryPart) {
			t.Errorf("log query %q does not contain %q", logQuery, queryPart)
		}
	}
}

func TestClientReadsNodeInventoryDetailAndEvents(t *testing.T) {
	t.Parallel()

	var eventSelector string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/nodes":
			writeTestJSON(t, w, map[string]any{"items": []any{
				nodeFixture("worker-b", "False", false),
				nodeFixture("control-01.example.internal", "True", true),
			}})
		case "/api/v1/nodes/control-01.example.internal":
			writeTestJSON(t, w, nodeFixture("control-01.example.internal", "True", true))
		case "/api/v1/events":
			eventSelector = r.URL.Query().Get("fieldSelector")
			writeTestJSON(t, w, map[string]any{"items": []any{
				map[string]any{
					"metadata": map[string]any{"name": "node-old", "creationTimestamp": "2026-07-24T08:00:00Z"},
					"type":     "Normal", "reason": "RegisteredNode", "message": "Node registered", "count": 1,
					"reportingComponent": "node-controller", "lastTimestamp": "2026-07-24T08:01:00Z",
				},
				map[string]any{
					"metadata": map[string]any{"name": "node-new", "creationTimestamp": "2026-07-24T08:02:00Z"},
					"type":     "Warning", "reason": "NodeNotReady", "message": "Node is not ready", "count": 2,
					"reportingComponent": "node-controller", "eventTime": "2026-07-24T08:03:00Z",
				},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := NewClient(Connection{Server: server.URL, CACert: string(certificate), BearerToken: "test-token"}, loopbackPolicy(t))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	nodes, err := client.Nodes(context.Background())
	if err != nil {
		t.Fatalf("Nodes() error = %v", err)
	}
	if len(nodes) != 2 || nodes[0].Name != "control-01.example.internal" || nodes[1].Status != "NotReady" {
		t.Fatalf("Nodes() = %#v", nodes)
	}
	first := nodes[0]
	if len(first.Roles) != 1 || first.Roles[0] != "control-plane" || first.InternalIP != "10.0.0.11" {
		t.Errorf("node identity = %#v", first)
	}
	if first.Allocatable.CPU != "3500m" || first.Allocatable.Memory != "15Gi" || first.Capacity.Pods != "110" {
		t.Errorf("node resources = %#v", first)
	}
	if !first.Unschedulable || first.TaintCount != 1 || first.Version != "v1.36.2" {
		t.Errorf("node scheduling = %#v", first)
	}

	detail, err := client.NodeDetail(context.Background(), "control-01.example.internal")
	if err != nil {
		t.Fatalf("NodeDetail() error = %v", err)
	}
	if detail.UID != "uid-control-01.example.internal" || detail.ResourceVersion != "91" || detail.SystemInfo.ContainerRuntimeVersion != "containerd://2.1.4" {
		t.Errorf("NodeDetail() = %#v", detail)
	}
	if len(detail.Taints) != 1 || detail.Taints[0].Effect != "NoSchedule" || len(detail.Conditions) != 2 {
		t.Errorf("node diagnostics = %#v", detail)
	}
	if detail.Labels["topology.kubernetes.io/zone"] != "cn-east-1a" {
		t.Errorf("node labels = %#v", detail.Labels)
	}

	events, err := client.NodeEvents(context.Background(), "control-01.example.internal", 1)
	if err != nil {
		t.Fatalf("NodeEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].Reason != "NodeNotReady" {
		t.Errorf("NodeEvents() = %#v", events)
	}
	if !strings.Contains(eventSelector, "involvedObject.kind=Node") || !strings.Contains(eventSelector, "involvedObject.name=control-01.example.internal") {
		t.Errorf("event fieldSelector = %q", eventSelector)
	}
}

func nodeFixture(name, ready string, controlPlane bool) map[string]any {
	labels := map[string]string{
		"kubernetes.io/hostname":      name,
		"topology.kubernetes.io/zone": "cn-east-1a",
	}
	if controlPlane {
		labels["node-role.kubernetes.io/control-plane"] = ""
	}
	return map[string]any{
		"metadata": map[string]any{
			"name": name, "uid": "uid-" + name, "resourceVersion": "91", "labels": labels,
			"creationTimestamp": "2026-07-20T08:00:00Z",
		},
		"spec": map[string]any{
			"unschedulable": controlPlane,
			"taints":        []any{map[string]any{"key": "node-role.kubernetes.io/control-plane", "effect": "NoSchedule"}},
		},
		"status": map[string]any{
			"capacity":    map[string]string{"cpu": "4", "memory": "16Gi", "pods": "110", "ephemeral-storage": "100Gi"},
			"allocatable": map[string]string{"cpu": "3500m", "memory": "15Gi", "pods": "100", "ephemeral-storage": "90Gi"},
			"addresses": []any{
				map[string]any{"type": "InternalIP", "address": "10.0.0.11"},
				map[string]any{"type": "Hostname", "address": name},
			},
			"nodeInfo": map[string]any{
				"architecture": "amd64", "operatingSystem": "linux", "osImage": "Ubuntu 24.04.2 LTS",
				"kernelVersion": "6.8.0", "containerRuntimeVersion": "containerd://2.1.4", "kubeletVersion": "v1.36.2",
			},
			"conditions": []any{
				map[string]any{"type": "MemoryPressure", "status": "False", "reason": "KubeletHasSufficientMemory", "lastTransitionTime": "2026-07-24T07:00:00Z"},
				map[string]any{"type": "Ready", "status": ready, "reason": "KubeletReady", "message": "kubelet is ready", "lastTransitionTime": "2026-07-24T08:00:00Z"},
			},
		},
	}
}

func TestClientTruncatesOversizedPodLogs(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(strings.Repeat("x", maxLogBytes+137)))
	}))
	t.Cleanup(server.Close)

	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := NewClient(Connection{Server: server.URL, CACert: string(certificate), BearerToken: "test-token"}, loopbackPolicy(t))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	logs, err := client.PodLogs(context.Background(), domain.PodLogRequest{
		Namespace: "payments", Pod: "gateway-0", Container: "app", TailLines: 200, Timestamps: true,
	})
	if err != nil {
		t.Fatalf("PodLogs() error = %v", err)
	}
	if !logs.Truncated || len(logs.Content) != maxLogBytes {
		t.Fatalf("logs truncated = %t, content bytes = %d", logs.Truncated, len(logs.Content))
	}
}

func TestAppendListItemsEnforcesResourceLimits(t *testing.T) {
	t.Parallel()

	items := make([]json.RawMessage, maxListItems)
	if _, _, err := appendListItems(items, 0, []json.RawMessage{json.RawMessage(`{}`)}); !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("item limit error = %v", err)
	}
	if _, _, err := appendListItems(nil, maxListBytes-1, []json.RawMessage{json.RawMessage(`{}`)}); !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("byte limit error = %v", err)
	}
}

func TestClientMutatesDeploymentWithResourceVersion(t *testing.T) {
	t.Parallel()

	var requests []map[string]any
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/apis/apps/v1/namespaces/payments/deployments/gateway" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Content-Type"); got != "application/merge-patch+json" {
			t.Errorf("Content-Type = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode patch: %v", err)
		}
		requests = append(requests, body)
		writeTestJSON(t, w, map[string]any{
			"metadata": map[string]any{
				"name": "gateway", "namespace": "payments", "resourceVersion": "43",
				"creationTimestamp": "2026-07-24T08:00:00Z",
			},
			"spec": map[string]any{
				"replicas": 5,
				"template": map[string]any{"spec": map[string]any{"containers": []any{
					map[string]any{"name": "app", "image": "registry.example.com/gateway:1.4.0"},
				}}},
			},
			"status": map[string]any{"readyReplicas": 3},
		})
	}))
	t.Cleanup(server.Close)

	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := NewClient(Connection{Server: server.URL, CACert: string(certificate), BearerToken: "test-token"}, loopbackPolicy(t))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	reference := domain.WorkloadReference{Kind: "deployment", Namespace: "payments", Name: "gateway"}
	scaled, err := client.ScaleWorkload(context.Background(), reference, "42", 5)
	if err != nil {
		t.Fatalf("ScaleWorkload() error = %v", err)
	}
	if scaled.Desired != 5 || scaled.Name != "gateway" {
		t.Errorf("scaled workload = %#v", scaled)
	}
	restartedAt := time.Date(2026, 7, 25, 8, 3, 0, 0, time.UTC)
	if _, err := client.RestartWorkload(context.Background(), reference, "43", restartedAt); err != nil {
		t.Fatalf("RestartWorkload() error = %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("patch count = %d", len(requests))
	}
	firstMetadata := requests[0]["metadata"].(map[string]any)
	firstSpec := requests[0]["spec"].(map[string]any)
	if firstMetadata["resourceVersion"] != "42" || firstSpec["replicas"] != float64(5) {
		t.Errorf("scale patch = %#v", requests[0])
	}
	secondMetadata := requests[1]["metadata"].(map[string]any)
	secondSpec := requests[1]["spec"].(map[string]any)
	template := secondSpec["template"].(map[string]any)
	templateMetadata := template["metadata"].(map[string]any)
	annotations := templateMetadata["annotations"].(map[string]any)
	if secondMetadata["resourceVersion"] != "43" || annotations[restartAnnotation] != restartedAt.Format(time.RFC3339Nano) {
		t.Errorf("restart patch = %#v", requests[1])
	}
}

func TestClientPreviewsAndUpdatesDeploymentImage(t *testing.T) {
	t.Parallel()

	type patchRequest struct {
		DryRun string
		Body   map[string]any
	}
	requests := make([]patchRequest, 0, 2)
	getCount := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apis/apps/v1/namespaces/payments/deployments/gateway" {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			getCount++
			writeTestDeployment(t, w, "42", "registry.example.com/gateway:1.4.0")
		case http.MethodPatch:
			if got := r.Header.Get("Content-Type"); got != "application/strategic-merge-patch+json" {
				t.Errorf("Content-Type = %q", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode image patch: %v", err)
			}
			requests = append(requests, patchRequest{DryRun: r.URL.Query().Get("dryRun"), Body: body})
			resourceVersion := "43"
			if r.URL.Query().Get("dryRun") == "All" {
				resourceVersion = "42"
			}
			writeTestDeployment(t, w, resourceVersion, "registry.example.com/gateway:1.5.0")
		default:
			http.Error(w, "unsupported method", http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(server.Close)

	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := NewClient(Connection{Server: server.URL, CACert: string(certificate), BearerToken: "test-token"}, loopbackPolicy(t))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	change := domain.WorkloadImageChange{
		Reference:       domain.WorkloadReference{Kind: "deployment", Namespace: "payments", Name: "gateway"},
		ResourceVersion: "42",
		Container:       "app",
		CurrentImage:    "registry.example.com/gateway:1.4.0",
		Image:           "registry.example.com/gateway:1.5.0",
	}
	preview, err := client.PreviewWorkloadImage(context.Background(), change)
	if err != nil {
		t.Fatalf("PreviewWorkloadImage() error = %v", err)
	}
	if preview.Container != "app" || preview.ResourceVersion != "42" || len(preview.Changes) != 1 ||
		preview.Changes[0].Before != change.CurrentImage || preview.Changes[0].After != change.Image {
		t.Errorf("preview = %#v", preview)
	}
	updated, err := client.UpdateWorkloadImage(context.Background(), change)
	if err != nil {
		t.Fatalf("UpdateWorkloadImage() error = %v", err)
	}
	if updated.Name != "gateway" || len(updated.Images) != 1 || updated.Images[0] != change.Image {
		t.Errorf("updated workload = %#v", updated)
	}
	if getCount != 2 || len(requests) != 2 || requests[0].DryRun != "All" || requests[1].DryRun != "" {
		t.Fatalf("GET count = %d, patch requests = %#v", getCount, requests)
	}
	for _, request := range requests {
		if len(request.Body) != 2 {
			t.Errorf("patch top-level fields = %#v", request.Body)
		}
		metadata, _ := request.Body["metadata"].(map[string]any)
		spec, _ := request.Body["spec"].(map[string]any)
		template, _ := spec["template"].(map[string]any)
		templateSpec, _ := template["spec"].(map[string]any)
		containers, _ := templateSpec["containers"].([]any)
		if metadata["resourceVersion"] != "42" || len(containers) != 1 {
			t.Errorf("image patch = %#v", request.Body)
			continue
		}
		container, _ := containers[0].(map[string]any)
		if len(container) != 2 || container["name"] != "app" || container["image"] != change.Image {
			t.Errorf("container patch = %#v", container)
		}
	}
}

func TestClientRejectsStaleDeploymentImageBeforePatch(t *testing.T) {
	t.Parallel()

	patchCount := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patchCount++
		}
		writeTestDeployment(t, w, "44", "registry.example.com/gateway:1.4.1")
	}))
	t.Cleanup(server.Close)
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := NewClient(Connection{Server: server.URL, CACert: string(certificate), BearerToken: "test-token"}, loopbackPolicy(t))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.PreviewWorkloadImage(context.Background(), domain.WorkloadImageChange{
		Reference:       domain.WorkloadReference{Kind: "deployment", Namespace: "payments", Name: "gateway"},
		ResourceVersion: "42",
		Container:       "app",
		CurrentImage:    "registry.example.com/gateway:1.4.0",
		Image:           "registry.example.com/gateway:1.5.0",
	})
	if !errors.Is(err, domain.ErrConflict) || patchCount != 0 {
		t.Fatalf("PreviewWorkloadImage() error = %v, patch count = %d", err, patchCount)
	}
}

func TestClientMapsOversizedMutationResponseToUpstream(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", int(maxMutationBytes+1))))
	}))
	t.Cleanup(server.Close)
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := NewClient(Connection{Server: server.URL, CACert: string(certificate), BearerToken: "test-token"}, loopbackPolicy(t))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.PreviewWorkloadImage(context.Background(), domain.WorkloadImageChange{
		Reference:       domain.WorkloadReference{Kind: "deployment", Namespace: "payments", Name: "gateway"},
		ResourceVersion: "42",
		Container:       "app",
		CurrentImage:    "registry.example.com/gateway:1.4.0",
		Image:           "registry.example.com/gateway:1.5.0",
	})
	if !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("PreviewWorkloadImage() error = %v, want ErrUpstream", err)
	}
}

func TestClientChecksCapabilitiesSeriallyWithBoundedResults(t *testing.T) {
	t.Parallel()

	type attributes struct {
		Group       string `json:"group"`
		Resource    string `json:"resource"`
		Subresource string `json:"subresource"`
		Verb        string `json:"verb"`
		Namespace   string `json:"namespace"`
	}
	var mu sync.Mutex
	requests := make([]attributes, 0, 10)
	var active atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews" {
			http.NotFound(w, r)
			return
		}
		if current := active.Add(1); current != 1 {
			t.Errorf("concurrent capability requests = %d, want 1", current)
		}
		defer active.Add(-1)
		var body struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
			Spec       struct {
				ResourceAttributes attributes `json:"resourceAttributes"`
			} `json:"spec"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode capability review: %v", err)
		}
		if body.APIVersion != "authorization.k8s.io/v1" || body.Kind != "SelfSubjectAccessReview" {
			t.Errorf("capability review type = %s/%s", body.APIVersion, body.Kind)
		}
		mu.Lock()
		index := len(requests)
		requests = append(requests, body.Spec.ResourceAttributes)
		mu.Unlock()
		status := map[string]any{"allowed": true, "denied": false, "reason": "internal role details"}
		if index == 1 {
			status = map[string]any{"allowed": false, "denied": true, "reason": "internal deny details"}
		}
		if index == 2 {
			status = map[string]any{"allowed": false, "denied": false, "evaluationError": "internal authorizer failure"}
		}
		writeTestJSON(t, w, map[string]any{"status": status})
	}))
	t.Cleanup(server.Close)

	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := NewClient(Connection{Server: server.URL, CACert: string(certificate), BearerToken: "test-token"}, loopbackPolicy(t))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	capabilities, err := client.Capabilities(context.Background(), "payments")
	if err != nil {
		t.Fatalf("Capabilities() error = %v", err)
	}
	expected := []attributes{
		{Resource: "namespaces", Verb: "list"},
		{Resource: "nodes", Verb: "list"},
		{Resource: "pods", Verb: "list", Namespace: "payments"},
		{Resource: "pods", Subresource: "log", Verb: "get", Namespace: "payments"},
		{Resource: "events", Verb: "list", Namespace: "payments"},
		{Group: "apps", Resource: "deployments", Verb: "list", Namespace: "payments"},
		{Group: "apps", Resource: "statefulsets", Verb: "list", Namespace: "payments"},
		{Group: "apps", Resource: "daemonsets", Verb: "list", Namespace: "payments"},
		{Group: "apps", Resource: "deployments", Verb: "patch", Namespace: "payments"},
		{Group: "apps", Resource: "deployments", Subresource: "scale", Verb: "patch", Namespace: "payments"},
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != len(expected) || len(capabilities) != len(expected) {
		t.Fatalf("requests = %d, capabilities = %d, want %d", len(requests), len(capabilities), len(expected))
	}
	for index := range expected {
		if requests[index] != expected[index] {
			t.Errorf("request %d = %#v, want %#v", index, requests[index], expected[index])
		}
	}
	if capabilities[0].Key != "namespaces.list" || capabilities[0].State != domain.KubernetesCapabilityAllowed ||
		capabilities[1].State != domain.KubernetesCapabilityDenied || capabilities[2].State != domain.KubernetesCapabilityIndeterminate {
		t.Fatalf("capabilities = %#v", capabilities[:3])
	}
}

func TestClientRejectsInvalidCapabilityNamespaceWithoutRequest(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
	}))
	t.Cleanup(server.Close)
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := NewClient(Connection{Server: server.URL, CACert: string(certificate), BearerToken: "test-token"}, loopbackPolicy(t))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if _, err := client.Capabilities(context.Background(), "Invalid_Namespace"); err == nil {
		t.Fatal("Capabilities() accepted an invalid namespace")
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("capability calls = %d, want 0", got)
	}
}

func TestClientStopsCapabilityChecksOnCancellation(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	started := make(chan struct{})
	releaseBlockedRequest := make(chan struct{})
	defer close(releaseBlockedRequest)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if call == 1 {
			writeTestJSON(t, w, map[string]any{"status": map[string]any{"allowed": true}})
			return
		}
		if call == 2 {
			close(started)
			select {
			case <-r.Context().Done():
			case <-releaseBlockedRequest:
			}
			return
		}
		t.Errorf("unexpected capability request %d", call)
	}))
	t.Cleanup(server.Close)
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := NewClient(Connection{Server: server.URL, CACert: string(certificate), BearerToken: "test-token"}, loopbackPolicy(t))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, capabilityErr := client.Capabilities(ctx, "payments")
		result <- capabilityErr
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("second capability check did not start")
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("Capabilities() error = %v, want context canceled", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("capability calls = %d, want 2", got)
	}
}

func TestClientBoundsCapabilityReviewResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", int(maxCapabilityReviewBytes+1))))
	}))
	t.Cleanup(server.Close)
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := NewClient(Connection{Server: server.URL, CACert: string(certificate), BearerToken: "test-token"}, loopbackPolicy(t))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if _, err := client.Capabilities(context.Background(), "payments"); !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("Capabilities() error = %v, want ErrUpstream", err)
	}
}

func writeTestDeployment(t *testing.T, w http.ResponseWriter, resourceVersion, image string) {
	t.Helper()
	writeTestJSON(t, w, map[string]any{
		"metadata": map[string]any{
			"name": "gateway", "namespace": "payments", "resourceVersion": resourceVersion,
			"creationTimestamp": "2026-07-24T08:00:00Z",
		},
		"spec": map[string]any{
			"replicas": 3,
			"template": map[string]any{"spec": map[string]any{"containers": []any{
				map[string]any{"name": "app", "image": image},
			}}},
		},
		"status": map[string]any{"readyReplicas": 3},
	})
}

func TestClientMapsPatchConflict(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "conflict", http.StatusConflict)
	}))
	t.Cleanup(server.Close)
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := NewClient(Connection{Server: server.URL, CACert: string(certificate), BearerToken: "test-token"}, loopbackPolicy(t))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.ScaleWorkload(context.Background(), domain.WorkloadReference{
		Kind: "deployment", Namespace: "payments", Name: "gateway",
	}, "41", 2)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("ScaleWorkload() error = %v", err)
	}
}

func TestClientPreservesRequestCancellation(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := NewClient(Connection{Server: server.URL, CACert: string(certificate), BearerToken: "test-token"}, loopbackPolicy(t))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.WorkloadDetail(ctx, domain.WorkloadReference{
		Kind: "deployment", Namespace: "payments", Name: "gateway",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WorkloadDetail() error = %v, want context cancellation", err)
	}
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func loopbackPolicy(t *testing.T) *outbound.Policy {
	t.Helper()
	prefix := netip.MustParsePrefix("127.0.0.0/8")
	return outbound.NewPolicy(systemResolver{}, []netip.Prefix{prefix})
}

type systemResolver struct{}

func (systemResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, network, host)
}

type deadlineRequiredResolver struct{}

func (deadlineRequiredResolver) LookupIP(ctx context.Context, _, _ string) ([]net.IP, error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("DNS lookup has no deadline")
	}
	return []net.IP{net.ParseIP("93.184.216.34")}, nil
}
