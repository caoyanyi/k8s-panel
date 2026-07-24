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
	"testing"

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
							"metadata": map[string]any{"name": "payments", "creationTimestamp": "2026-07-23T08:00:00Z"},
							"status":   map[string]any{"phase": "Active"},
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
