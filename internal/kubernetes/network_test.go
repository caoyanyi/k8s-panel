package kubernetes

import (
	"context"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

func TestClientReadsBoundedServiceInventory(t *testing.T) {
	t.Parallel()

	ports := make([]any, 0, domain.MaxServicePorts+1)
	ports = append(ports,
		map[string]any{"name": "http", "protocol": "TCP", "port": 80, "targetPort": "web", "nodePort": 30080},
		map[string]any{"name": "metrics", "protocol": "TCP", "port": 9090, "targetPort": 9091},
	)
	for index := 2; index < domain.MaxServicePorts+1; index++ {
		ports = append(ports, map[string]any{"protocol": "UDP", "port": 10000 + index})
	}
	externalIPs := make([]string, 0, domain.MaxNetworkAddresses-1)
	for index := 0; index < domain.MaxNetworkAddresses-1; index++ {
		externalIPs = append(externalIPs, fmt.Sprintf("203.0.113.%d", index+1))
	}

	var requested string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = r.URL.RequestURI()
		if r.URL.Path != "/api/v1/namespaces/payments/services" {
			http.NotFound(w, r)
			return
		}
		writeTestJSON(t, w, map[string]any{"items": []any{
			map[string]any{
				"metadata": map[string]any{"name": "edge", "namespace": "payments", "creationTimestamp": "2026-07-24T08:00:00Z"},
				"spec": map[string]any{
					"type": "LoadBalancer", "clusterIP": "10.96.0.20", "externalIPs": externalIPs, "ports": ports,
				},
				"status": map[string]any{"loadBalancer": map[string]any{"ingress": []any{
					map[string]any{"ip": externalIPs[0]},
					map[string]any{"ip": "198.51.100.20"},
					map[string]any{"hostname": "edge.example.com"},
				}}},
			},
			map[string]any{
				"metadata": map[string]any{"name": "directory", "namespace": "payments", "creationTimestamp": "2026-07-23T08:00:00Z"},
				"spec":     map[string]any{"type": "ExternalName", "externalName": "directory.example.net"},
			},
		}})
	}))
	t.Cleanup(server.Close)

	client := newNetworkTestClient(t, server)
	services, err := client.Services(context.Background(), "payments")
	if err != nil {
		t.Fatalf("Services() error = %v", err)
	}
	if len(services) != 2 || services[0].Name != "directory" || services[1].Name != "edge" {
		t.Fatalf("Services() = %#v", services)
	}
	if requested != "/api/v1/namespaces/payments/services?limit=500" {
		t.Fatalf("request URI = %q", requested)
	}
	directory := services[0]
	if directory.Type != "ExternalName" || directory.ExternalName != "directory.example.net" || directory.Ports == nil || directory.ExternalAddresses == nil {
		t.Errorf("ExternalName service = %#v", directory)
	}
	edge := services[1]
	if edge.ClusterIP != "10.96.0.20" || edge.PortCount != domain.MaxServicePorts+1 || len(edge.Ports) != domain.MaxServicePorts {
		t.Errorf("bounded service ports = %#v", edge)
	}
	if edge.Ports[0].TargetPort != "web" || edge.Ports[1].TargetPort != "9091" || edge.Ports[0].NodePort != 30080 {
		t.Errorf("service ports = %#v", edge.Ports[:2])
	}
	if edge.AddressCount != domain.MaxNetworkAddresses+1 || len(edge.ExternalAddresses) != domain.MaxNetworkAddresses {
		t.Errorf("bounded service addresses = %#v", edge)
	}
	if edge.ExternalAddresses[0] != "203.0.113.1" || edge.ExternalAddresses[len(edge.ExternalAddresses)-1] != "198.51.100.20" {
		t.Errorf("service address order = %#v", edge.ExternalAddresses)
	}
}

func TestClientReadsBoundedIngressInventory(t *testing.T) {
	t.Parallel()

	rules := make([]any, 0, domain.MaxIngressHosts+2)
	for index := 0; index < domain.MaxIngressHosts+2; index++ {
		rules = append(rules, map[string]any{
			"host": fmt.Sprintf("app-%02d.example.com", index),
			"http": map[string]any{"paths": []any{
				map[string]any{"path": "/", "pathType": "Prefix"},
				map[string]any{"path": "/health", "pathType": "Exact"},
			}},
		})
	}
	addresses := make([]any, 0, domain.MaxNetworkAddresses+2)
	for index := 0; index < domain.MaxNetworkAddresses; index++ {
		addresses = append(addresses, map[string]any{"ip": fmt.Sprintf("198.51.100.%d", index+1)})
	}
	addresses = append(addresses, map[string]any{"ip": "198.51.100.1"}, map[string]any{"hostname": "gateway.example.com"})

	var mu sync.Mutex
	requested := make([]string, 0, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requested = append(requested, r.URL.RequestURI())
		mu.Unlock()
		if r.URL.Path != "/apis/networking.k8s.io/v1/ingresses" {
			http.NotFound(w, r)
			return
		}
		writeTestJSON(t, w, map[string]any{"items": []any{
			map[string]any{
				"metadata": map[string]any{"name": "web", "namespace": "payments", "creationTimestamp": "2026-07-24T08:00:00Z"},
				"spec":     map[string]any{"ingressClassName": "nginx", "tls": []any{map[string]any{"secretName": "web-tls"}}, "rules": rules},
				"status":   map[string]any{"loadBalancer": map[string]any{"ingress": addresses}},
			},
			map[string]any{
				"metadata": map[string]any{"name": "catch-all", "namespace": "default", "creationTimestamp": "2026-07-22T08:00:00Z"},
				"spec":     map[string]any{"rules": []any{map[string]any{"http": map[string]any{"paths": []any{map[string]any{"path": "/"}}}}}},
			},
		}})
	}))
	t.Cleanup(server.Close)

	client := newNetworkTestClient(t, server)
	ingresses, err := client.Ingresses(context.Background(), "")
	if err != nil {
		t.Fatalf("Ingresses() error = %v", err)
	}
	if len(ingresses) != 2 || ingresses[0].Namespace != "default" || ingresses[0].Name != "catch-all" || ingresses[1].Name != "web" {
		t.Fatalf("Ingresses() = %#v", ingresses)
	}
	mu.Lock()
	gotRequests := append([]string(nil), requested...)
	mu.Unlock()
	if len(gotRequests) != 1 || gotRequests[0] != "/apis/networking.k8s.io/v1/ingresses?limit=500" {
		t.Fatalf("request URIs = %#v", gotRequests)
	}
	if len(ingresses[0].Hosts) != 1 || ingresses[0].Hosts[0] != "*" || ingresses[0].PathCount != 1 {
		t.Errorf("catch-all ingress = %#v", ingresses[0])
	}
	web := ingresses[1]
	if web.ClassName != "nginx" || !web.TLS || web.RuleCount != domain.MaxIngressHosts+2 || web.PathCount != (domain.MaxIngressHosts+2)*2 {
		t.Errorf("ingress summary = %#v", web)
	}
	if web.HostCount != domain.MaxIngressHosts+2 || len(web.Hosts) != domain.MaxIngressHosts {
		t.Errorf("bounded ingress hosts = %#v", web.Hosts)
	}
	if web.AddressCount != domain.MaxNetworkAddresses+1 || len(web.Addresses) != domain.MaxNetworkAddresses {
		t.Errorf("bounded ingress addresses = %#v", web.Addresses)
	}
}

func TestClientRejectsInvalidNetworkNamespaceWithoutRequest(t *testing.T) {
	t.Parallel()

	var requests int
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	if _, err := client.Services(context.Background(), "bad/namespace"); err == nil {
		t.Fatal("Services() accepted an invalid namespace")
	}
	if _, err := client.Ingresses(context.Background(), "bad/namespace"); err == nil {
		t.Fatal("Ingresses() accepted an invalid namespace")
	}
	if requests != 0 {
		t.Fatalf("invalid namespaces made %d requests", requests)
	}
}

func TestClientUsesClusterWideServiceAndNamespacedIngressPaths(t *testing.T) {
	t.Parallel()

	requested := make([]string, 0, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.RequestURI())
		writeTestJSON(t, w, map[string]any{"items": []any{}})
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	if _, err := client.Services(context.Background(), ""); err != nil {
		t.Fatalf("Services() error = %v", err)
	}
	if _, err := client.Ingresses(context.Background(), "payments"); err != nil {
		t.Fatalf("Ingresses() error = %v", err)
	}
	want := []string{
		"/api/v1/services?limit=500",
		"/apis/networking.k8s.io/v1/namespaces/payments/ingresses?limit=500",
	}
	if len(requested) != len(want) || requested[0] != want[0] || requested[1] != want[1] {
		t.Fatalf("request URIs = %#v, want %#v", requested, want)
	}
}

func newNetworkTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := NewClient(Connection{Server: server.URL, CACert: string(certificate), BearerToken: "test-token"}, loopbackPolicy(t))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}
