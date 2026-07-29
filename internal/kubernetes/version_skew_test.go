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

func TestClientBuildsBoundedNodeVersionSkewReportFromTable(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requests := make([]string, 0, 3)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.RequestURI())
		mu.Unlock()
		switch r.URL.Path {
		case "/version":
			if got := r.Header.Get("Accept"); got != "application/json" {
				t.Errorf("version Accept = %q, want application/json", got)
			}
			writeTestJSON(t, w, map[string]any{
				"major": "1", "minor": "36", "gitVersion": "v1.36.2+vendor.1",
				"gitCommit": "private-commit", "platform": "private-platform",
			})
		case "/api/v1/nodes":
			if got := r.Header.Get("Accept"); got != kubernetesTableAccept {
				t.Errorf("node Accept = %q, want %q", got, kubernetesTableAccept)
			}
			if got := r.URL.Query().Get("includeObject"); got != "None" {
				t.Errorf("includeObject = %q, want None", got)
			}
			if got := r.URL.Query().Get("limit"); got != nodeVersionTablePageSize {
				t.Errorf("limit = %q, want %q", got, nodeVersionTablePageSize)
			}
			if r.URL.Query().Get("continue") == "page-two" {
				writeTestJSON(t, w, nodeVersionTable("", []any{
					nodeVersionTableRow("alpha", "Ready", "v1.36.2", "private-alpha"),
					nodeVersionTableRow("beta", "Ready", "v1.35.8-vendor.2", "private-beta"),
					nodeVersionTableRow("delta", "Ready", "v1.37.0", "private-delta"),
					nodeVersionTableRow("gamma", "NotReady", "v1.32.11", "private-gamma"),
					nodeVersionTableRow("major", "Ready", "v2.0.0", "private-major"),
				}))
				return
			}
			writeTestJSON(t, w, nodeVersionTable("page-two", []any{
				nodeVersionTableRow("zeta", "Ready", "v1.33.9", "private-zeta"),
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	report, err := client.NodeVersionSkew(context.Background())
	if err != nil {
		t.Fatalf("NodeVersionSkew() error = %v", err)
	}
	if report.APIServerVersion != "v1.36.2+vendor.1" || len(report.Nodes) != 6 {
		t.Fatalf("NodeVersionSkew() = %#v", report)
	}
	want := []struct {
		name       string
		status     domain.KubernetesNodeVersionSkewStatus
		skew       int
		maximum    int
		comparable bool
	}{
		{name: "alpha", status: domain.NodeVersionSameMinor, skew: 0, maximum: 3, comparable: true},
		{name: "beta", status: domain.NodeVersionWithinPolicy, skew: 1, maximum: 3, comparable: true},
		{name: "delta", status: domain.NodeVersionNewerThanServer, skew: -1, maximum: 3, comparable: true},
		{name: "gamma", status: domain.NodeVersionOutsidePolicy, skew: 4, maximum: 3, comparable: true},
		{name: "major", status: domain.NodeVersionMajorMismatch, comparable: false},
		{name: "zeta", status: domain.NodeVersionUpgradeBlocking, skew: 3, maximum: 3, comparable: true},
	}
	for index, expected := range want {
		item := report.Nodes[index]
		if item.Name != expected.name || item.Status != expected.status || item.MinorSkew != expected.skew ||
			item.MaximumMinorSkew != expected.maximum || item.MinorSkewComparable != expected.comparable {
			t.Errorf("node[%d] = %#v, want %#v", index, item, expected)
		}
	}

	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	for _, forbidden := range []string{"private-commit", "private-platform", "private-alpha", "NotReady"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("node version report leaked %q: %s", forbidden, encoded)
		}
	}

	mu.Lock()
	gotRequests := append([]string(nil), requests...)
	mu.Unlock()
	wantRequests := []string{
		"/version",
		"/api/v1/nodes?includeObject=None&limit=250",
		"/api/v1/nodes?continue=page-two&includeObject=None&limit=250",
	}
	if fmt.Sprint(gotRequests) != fmt.Sprint(wantRequests) {
		t.Fatalf("request URIs = %#v, want %#v", gotRequests, wantRequests)
	}
}

func TestClientClassifiesLegacyKubeletAtTwoMinorSkewAsUpgradeBlocking(t *testing.T) {
	t.Parallel()

	server := newNodeVersionTestServer(t, "v1.26.7", nodeVersionTable("", []any{
		nodeVersionTableRow("legacy", "Ready", "v1.24.17", "private"),
	}))
	client := newNetworkTestClient(t, server)

	report, err := client.NodeVersionSkew(context.Background())
	if err != nil || len(report.Nodes) != 1 {
		t.Fatalf("NodeVersionSkew() = %#v, %v", report, err)
	}
	item := report.Nodes[0]
	if item.Status != domain.NodeVersionUpgradeBlocking || item.MinorSkew != 2 || item.MaximumMinorSkew != 2 {
		t.Fatalf("legacy node = %#v", item)
	}
}

func TestClientRejectsUnsafeNodeVersionEvidenceWithoutEchoingValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		serverVersion string
		table         map[string]any
		forbidden     string
	}{
		{
			name: "invalid API server version", serverVersion: "v1.36.secret=value",
			table: nodeVersionTable("", nil), forbidden: "secret=value",
		},
		{
			name: "invalid kubelet version", serverVersion: "v1.36.2",
			table:     nodeVersionTable("", []any{nodeVersionTableRow("worker", "Ready", "v1.35.2-secret=value!", "private")}),
			forbidden: "secret=value!",
		},
		{
			name: "unexpected row object", serverVersion: "v1.36.2",
			table: nodeVersionTable("", []any{map[string]any{
				"cells":  []any{"worker", "Ready", "v1.36.2", "private"},
				"object": map[string]any{"apiVersion": "v1", "kind": "Node", "credential": "secret-object"},
			}}),
			forbidden: "secret-object",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := newNodeVersionTestServer(t, tt.serverVersion, tt.table)
			client := newNetworkTestClient(t, server)

			_, err := client.NodeVersionSkew(context.Background())
			if !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("NodeVersionSkew() error = %v, want upstream error", err)
			}
			if strings.Contains(err.Error(), tt.forbidden) {
				t.Fatalf("error leaked %q: %v", tt.forbidden, err)
			}
		})
	}
}

func TestClientBoundsNodeVersionSkewReport(t *testing.T) {
	t.Parallel()

	t.Run("page limit", func(t *testing.T) {
		var tableRequests atomic.Int64
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/version" {
				writeTestJSON(t, w, map[string]any{"gitVersion": "v1.36.2"})
				return
			}
			request := tableRequests.Add(1)
			writeTestJSON(t, w, nodeVersionTable(fmt.Sprintf("page-%d", request), nil))
		}))
		t.Cleanup(server.Close)
		client := newNetworkTestClient(t, server)

		_, err := client.NodeVersionSkew(context.Background())
		if !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("NodeVersionSkew() error = %v, want upstream error", err)
		}
		if got := tableRequests.Load(); got != nodeVersionMaxTablePages {
			t.Fatalf("table requests = %d, want %d", got, nodeVersionMaxTablePages)
		}
	})

	t.Run("duplicate node", func(t *testing.T) {
		server := newNodeVersionTestServer(t, "v1.36.2", nodeVersionTable("", []any{
			nodeVersionTableRow("worker", "Ready", "v1.36.2", "private-one"),
			nodeVersionTableRow("worker", "Ready", "v1.36.2", "private-two"),
		}))
		client := newNetworkTestClient(t, server)
		if _, err := client.NodeVersionSkew(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("NodeVersionSkew() error = %v, want upstream error", err)
		}
	})

	t.Run("unsafe continuation", func(t *testing.T) {
		server := newNodeVersionTestServer(t, "v1.36.2", nodeVersionTable("next\npage", nil))
		client := newNetworkTestClient(t, server)
		if _, err := client.NodeVersionSkew(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("NodeVersionSkew() error = %v, want upstream error", err)
		}
	})

	t.Run("duplicate required column", func(t *testing.T) {
		table := nodeVersionTable("", nil)
		table["columnDefinitions"] = []any{
			map[string]any{"name": "Name", "type": "string"},
			map[string]any{"name": "Version", "type": "string"},
			map[string]any{"name": "Version", "type": "string"},
		}
		server := newNodeVersionTestServer(t, "v1.36.2", table)
		client := newNetworkTestClient(t, server)
		if _, err := client.NodeVersionSkew(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("NodeVersionSkew() error = %v, want upstream error", err)
		}
	})
}

func TestParseKubernetesComponentVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value string
		major int
		minor int
		patch int
		valid bool
	}{
		{value: "v1.36.2", major: 1, minor: 36, patch: 2, valid: true},
		{value: "v1.30.7-gke.123", major: 1, minor: 30, patch: 7, valid: true},
		{value: "v1.29.4+k3s1", major: 1, minor: 29, patch: 4, valid: true},
		{value: "v1.31.0-rc.1+build.5", major: 1, minor: 31, patch: 0, valid: true},
		{value: "1.36.2"},
		{value: "v1.36"},
		{value: "v1.036.2"},
		{value: "v1.36.2-secret=value!"},
		{value: "v1.36.2\nprivate"},
		{value: "v1.36.2-"},
		{value: "v1.36.2+"},
		{value: "v1.36.2.4"},
		{value: "v1.36.2-.."},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			version, ok := parseKubernetesComponentVersion(tt.value)
			if ok != tt.valid || version.Major != tt.major || version.Minor != tt.minor || version.Patch != tt.patch {
				t.Fatalf("parseKubernetesComponentVersion(%q) = %#v, %t", tt.value, version, ok)
			}
		})
	}
}

func newNodeVersionTestServer(t *testing.T, serverVersion string, table map[string]any) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			writeTestJSON(t, w, map[string]any{"gitVersion": serverVersion})
		case "/api/v1/nodes":
			writeTestJSON(t, w, table)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func nodeVersionTable(continueToken string, rows []any) map[string]any {
	return map[string]any{
		"apiVersion": "meta.k8s.io/v1",
		"kind":       "Table",
		"metadata":   map[string]any{"continue": continueToken, "resourceVersion": "private-version"},
		"columnDefinitions": []any{
			map[string]any{"name": "Name", "type": "string"},
			map[string]any{"name": "Status", "type": "string"},
			map[string]any{"name": "Version", "type": "string"},
			map[string]any{"name": "Private", "type": "string"},
		},
		"rows": rows,
	}
}

func nodeVersionTableRow(name, status, version, private string) map[string]any {
	return map[string]any{
		"cells":  []any{name, status, version, private},
		"object": nil,
	}
}
