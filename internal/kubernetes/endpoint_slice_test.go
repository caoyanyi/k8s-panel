package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

func TestClientReadsBoundedEndpointSliceSummaries(t *testing.T) {
	t.Parallel()

	var requested string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = r.URL.RequestURI()
		writeTestJSON(t, w, map[string]any{
			"apiVersion": "discovery.k8s.io/v1", "kind": "EndpointSliceList", "metadata": map[string]any{},
			"items": []any{
				endpointSliceObject("payments", "gateway-ipv4-b", "gateway-service", "IPv4", []any{
					map[string]any{
						"addresses":  []string{"10.42.0.10"},
						"conditions": map[string]any{"ready": true, "serving": false, "terminating": true},
						"hostname":   "private-host", "nodeName": "private-node", "zone": "private-zone",
						"targetRef": map[string]any{"kind": "Pod", "name": "private-pod"},
						"hints":     map[string]any{"forZones": []any{map[string]any{"name": "private-zone"}}},
					},
					map[string]any{"addresses": []string{"10.42.0.11"}, "conditions": map[string]any{
						"ready": false, "serving": true, "terminating": false,
					}},
					map[string]any{"addresses": []string{"10.42.0.12"}, "conditions": map[string]any{}},
				}, []any{
					map[string]any{"name": "private-port", "protocol": "TCP", "port": 8443, "appProtocol": "private.example.com/protocol"},
				}),
				endpointSliceObject("default", "dns-fqdn", "external-dns", "FQDN", nil, nil),
			},
		})
	}))
	t.Cleanup(server.Close)

	client := newNetworkTestClient(t, server)
	slices, err := client.EndpointSlices(context.Background(), "")
	if err != nil {
		t.Fatalf("EndpointSlices() error = %v", err)
	}
	if requested != "/apis/discovery.k8s.io/v1/endpointslices?limit=250" {
		t.Fatalf("request URI = %q", requested)
	}
	if len(slices) != 2 || slices[0].Namespace != "default" || slices[0].Name != "dns-fqdn" ||
		slices[1].Namespace != "payments" || slices[1].Name != "gateway-ipv4-b" {
		t.Fatalf("EndpointSlices() = %#v", slices)
	}
	selected := slices[1]
	if selected.ServiceName != "gateway-service" || selected.AddressType != "IPv4" ||
		selected.EndpointCount != 3 || selected.PortCount != 1 ||
		selected.ReadyEndpointCount != 2 || selected.ReadyDefaultedCount != 1 ||
		selected.ServingEndpointCount != 2 || selected.ServingDefaultedCount != 1 ||
		selected.TerminatingEndpointCount != 1 || selected.TerminatingDefaultedCount != 1 {
		t.Fatalf("EndpointSlice summary = %#v", selected)
	}
	encoded, err := json.Marshal(selected)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	for _, privateValue := range []string{
		"10.42.0.10", "private-host", "private-node", "private-zone", "private-pod", "private-port", "8443", "private.example.com/protocol", "private-label-value",
	} {
		if strings.Contains(string(encoded), privateValue) {
			t.Errorf("summary leaked %q: %s", privateValue, encoded)
		}
	}
}

func TestClientUsesNamespacedEndpointSlicePathAndRejectsEscapedScope(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.RequestURI() != "/apis/discovery.k8s.io/v1/namespaces/payments/endpointslices?limit=250" {
			t.Errorf("request URI = %q", r.URL.RequestURI())
		}
		writeTestJSON(t, w, map[string]any{
			"apiVersion": "discovery.k8s.io/v1", "kind": "EndpointSliceList", "metadata": map[string]any{},
			"items": []any{endpointSliceObject("other", "escaped", "gateway", "IPv4", nil, nil)},
		})
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	if _, err := client.EndpointSlices(context.Background(), "bad/namespace"); err == nil {
		t.Fatal("EndpointSlices() accepted an invalid namespace")
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid namespace made %d requests", calls.Load())
	}
	if _, err := client.EndpointSlices(context.Background(), "payments"); !errors.Is(err, domain.ErrUpstream) ||
		!strings.Contains(err.Error(), "namespace scope") {
		t.Fatalf("EndpointSlices() escaped scope error = %v", err)
	}
}

func TestClientRejectsUntrustedEndpointSliceObjects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "api version", mutate: func(object map[string]any) { object["apiVersion"] = "discovery.k8s.io/v1beta1" }},
		{name: "kind", mutate: func(object map[string]any) { object["kind"] = "Endpoints" }},
		{name: "missing service label", mutate: func(object map[string]any) {
			object["metadata"].(map[string]any)["labels"] = map[string]string{}
		}},
		{name: "invalid service label", mutate: func(object map[string]any) {
			object["metadata"].(map[string]any)["labels"] = map[string]any{"kubernetes.io/service-name": " bad "}
		}},
		{name: "unknown address type", mutate: func(object map[string]any) { object["addressType"] = "Unknown" }},
		{name: "zero creation time", mutate: func(object map[string]any) {
			object["metadata"].(map[string]any)["creationTimestamp"] = "0001-01-01T00:00:00Z"
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			object := endpointSliceObject("payments", "gateway-ipv4", "gateway", "IPv4", nil, nil)
			test.mutate(object)
			raw, err := json.Marshal(object)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			remaining := maxEndpointSliceNestedEntries
			if _, err := decodeEndpointSlice(raw, "payments", &remaining); !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("decodeEndpointSlice() error = %v", err)
			}
		})
	}
}

func TestClientBoundsEndpointSliceNestedEntries(t *testing.T) {
	t.Parallel()

	repeated := func(count int) []any {
		items := make([]any, count)
		for index := range items {
			items[index] = map[string]any{}
		}
		return items
	}
	tests := []struct {
		name      string
		endpoints []any
		ports     []any
	}{
		{name: "endpoints", endpoints: repeated(maxEndpointSliceEndpointsPerObject + 1)},
		{name: "ports", ports: repeated(maxEndpointSlicePortsPerObject + 1)},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(endpointSliceObject("payments", "bounded", "gateway", "IPv4", test.endpoints, test.ports))
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			remaining := maxEndpointSliceNestedEntries
			if _, err := decodeEndpointSlice(raw, "payments", &remaining); !errors.Is(err, domain.ErrUpstream) ||
				!strings.Contains(err.Error(), "safe entry limit") {
				t.Fatalf("decodeEndpointSlice() error = %v", err)
			}
		})
	}
}

func TestClientStopsEndpointSliceProjectionAtRequestBudget(t *testing.T) {
	t.Parallel()

	endpoints := make([]any, maxEndpointSliceEndpointsPerObject)
	for index := range endpoints {
		endpoints[index] = map[string]any{}
	}
	items := make([]any, maxEndpointSliceNestedEntries/maxEndpointSliceEndpointsPerObject+1)
	for index := range items {
		items[index] = endpointSliceObject(
			"payments", fmt.Sprintf("gateway-%03d", index), "gateway", "IPv4", endpoints, nil,
		)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, map[string]any{
			"apiVersion": "discovery.k8s.io/v1", "kind": "EndpointSliceList", "metadata": map[string]any{}, "items": items,
		})
	}))
	t.Cleanup(server.Close)

	client := newNetworkTestClient(t, server)
	if _, err := client.EndpointSlices(context.Background(), "payments"); !errors.Is(err, domain.ErrUpstream) ||
		!strings.Contains(err.Error(), "nested entry limit") {
		t.Fatalf("EndpointSlices() error = %v", err)
	}
}

func endpointSliceObject(namespace, name, serviceName, addressType string, endpoints, ports []any) map[string]any {
	return map[string]any{
		"apiVersion": "discovery.k8s.io/v1", "kind": "EndpointSlice",
		"metadata": map[string]any{
			"namespace": namespace, "name": name, "creationTimestamp": "2026-07-28T05:00:00Z",
			"labels": map[string]string{"kubernetes.io/service-name": serviceName, "private-label": "private-label-value"},
		},
		"addressType": addressType,
		"endpoints":   endpoints,
		"ports":       ports,
	}
}
