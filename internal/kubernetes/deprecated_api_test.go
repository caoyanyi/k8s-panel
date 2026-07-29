package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

func TestClientListsBoundedDeprecatedAPIRequestEvidence(t *testing.T) {
	t.Parallel()

	payload := strings.Join([]string{
		"# HELP apiserver_requested_deprecated_apis Gauge of deprecated APIs that have been requested.",
		"# TYPE apiserver_requested_deprecated_apis gauge",
		`private_metric{credential="not-projected"} 99`,
		`apiserver_requested_deprecated_apis{resource="componentstatuses",subresource="status",version="v1",group="",removed_release="1.40"} 1.0`,
		`apiserver_requested_deprecated_apis{group="extensions",removed_release="1.22",resource="ingresses",subresource="",version="v1beta1"} 1`,
		`apiserver_requested_deprecated_apis{group="apps",version="v1beta1",resource="deployments",subresource="scale",removed_release="1.9"} 1e0`,
		"",
	}, "\n")
	server := newDeprecatedAPITestServer(t, payload, func(r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/metrics" || r.URL.RawQuery != "" {
			t.Errorf("request = %s %s, want GET /metrics", r.Method, r.URL.RequestURI())
		}
		if got := r.Header.Get("Accept"); got != deprecatedAPIMetricsAccept {
			t.Errorf("Accept = %q, want %q", got, deprecatedAPIMetricsAccept)
		}
	})
	client := newNetworkTestClient(t, server)

	items, err := client.DeprecatedAPIRequests(context.Background())
	if err != nil {
		t.Fatalf("DeprecatedAPIRequests() error = %v", err)
	}
	want := []domain.KubernetesDeprecatedAPIRequest{
		{Group: "apps", Version: "v1beta1", Resource: "deployments", Subresource: "scale", RemovedRelease: "1.9"},
		{Group: "extensions", Version: "v1beta1", Resource: "ingresses", RemovedRelease: "1.22"},
		{Version: "v1", Resource: "componentstatuses", Subresource: "status", RemovedRelease: "1.40"},
	}
	if fmt.Sprint(items) != fmt.Sprint(want) {
		t.Fatalf("DeprecatedAPIRequests() = %#v, want %#v", items, want)
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	if strings.Contains(string(encoded), "not-projected") || strings.Contains(string(encoded), "private_metric") {
		t.Fatalf("deprecated API evidence leaked unrelated metrics: %s", encoded)
	}
}

func TestClientReturnsEmptyDeprecatedAPIRequestEvidence(t *testing.T) {
	t.Parallel()

	server := newDeprecatedAPITestServer(t, "# HELP other_metric private\nother_metric 1\n", nil)
	client := newNetworkTestClient(t, server)

	items, err := client.DeprecatedAPIRequests(context.Background())
	if err != nil || len(items) != 0 || items == nil {
		t.Fatalf("DeprecatedAPIRequests() = %#v, %v, want non-nil empty list", items, err)
	}
}

func TestClientRejectsInvalidDeprecatedAPIMetricsWithoutEchoingValues(t *testing.T) {
	t.Parallel()

	valid := `group="apps",version="v1beta1",resource="deployments",subresource="",removed_release="1.30"`
	tests := []struct {
		name    string
		payload string
	}{
		{name: "missing label", payload: `apiserver_requested_deprecated_apis{group="apps",version="v1beta1",resource="deployments",subresource=""} 1`},
		{name: "duplicate label", payload: `apiserver_requested_deprecated_apis{group="apps",group="secret-token",version="v1beta1",resource="deployments",subresource="",removed_release="1.30"} 1`},
		{name: "unknown label", payload: `apiserver_requested_deprecated_apis{group="apps",version="v1beta1",resource="deployments",subresource="",removed_release="1.30",client="secret-token"} 1`},
		{name: "unsafe escaped value", payload: `apiserver_requested_deprecated_apis{group="apps",version="v1beta1",resource="secret\ntoken",subresource="",removed_release="1.30"} 1`},
		{name: "invalid escape", payload: `apiserver_requested_deprecated_apis{group="apps",version="v1beta1",resource="secret\ttoken",subresource="",removed_release="1.30"} 1`},
		{name: "invalid removal release", payload: `apiserver_requested_deprecated_apis{group="apps",version="v1beta1",resource="deployments",subresource="",removed_release="v1.30-secret-token"} 1`},
		{name: "zero sample", payload: "apiserver_requested_deprecated_apis{" + valid + "} 0"},
		{name: "non-finite sample", payload: "apiserver_requested_deprecated_apis{" + valid + "} NaN"},
		{name: "rounded sample", payload: "apiserver_requested_deprecated_apis{" + valid + "} 0.999999999999999999999999999999"},
		{name: "fraction sample", payload: "apiserver_requested_deprecated_apis{" + valid + "} 1/1"},
		{name: "hexadecimal sample", payload: "apiserver_requested_deprecated_apis{" + valid + "} 0x1"},
		{name: "sample timestamp", payload: "apiserver_requested_deprecated_apis{" + valid + "} 1 123456"},
		{name: "unsupported sample whitespace", payload: "apiserver_requested_deprecated_apis{" + valid + "} 1\v"},
		{name: "missing sample separator", payload: "apiserver_requested_deprecated_apis{" + valid + "}1"},
		{name: "missing braces", payload: "apiserver_requested_deprecated_apis 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := newDeprecatedAPITestServer(t, tt.payload+"\n", nil)
			client := newNetworkTestClient(t, server)

			_, err := client.DeprecatedAPIRequests(context.Background())
			if !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("DeprecatedAPIRequests() error = %v, want upstream error", err)
			}
			if strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "secret\\ntoken") {
				t.Fatalf("error leaked metric content: %v", err)
			}
		})
	}
}

func TestClientRejectsDuplicateDeprecatedAPIRequestEvidence(t *testing.T) {
	t.Parallel()

	line := `apiserver_requested_deprecated_apis{group="apps",version="v1beta1",resource="deployments",subresource="",removed_release="1.30"} 1`
	server := newDeprecatedAPITestServer(t, line+"\n"+line+"\n", nil)
	client := newNetworkTestClient(t, server)

	if _, err := client.DeprecatedAPIRequests(context.Background()); !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("DeprecatedAPIRequests() error = %v, want upstream error", err)
	}
}

func TestClientBoundsDeprecatedAPIMetrics(t *testing.T) {
	t.Run("sample limit", func(t *testing.T) {
		var payload strings.Builder
		for index := 0; index <= deprecatedAPIMaxSamples; index++ {
			fmt.Fprintf(
				&payload,
				"apiserver_requested_deprecated_apis{group=\"apps\",version=\"v1beta1\",resource=\"resource-%03d\",subresource=\"\",removed_release=\"1.30\"} 1\n",
				index,
			)
		}
		server := newDeprecatedAPITestServer(t, payload.String(), nil)
		client := newNetworkTestClient(t, server)

		if _, err := client.DeprecatedAPIRequests(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("DeprecatedAPIRequests() error = %v, want upstream error", err)
		}
	})

	t.Run("line limit", func(t *testing.T) {
		server := newDeprecatedAPITestServer(t, strings.Repeat("x", deprecatedAPIMaxLineBytes+1)+"\n", nil)
		client := newNetworkTestClient(t, server)

		if _, err := client.DeprecatedAPIRequests(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("DeprecatedAPIRequests() error = %v, want upstream error", err)
		}
	})

	t.Run("response limit", func(t *testing.T) {
		server := newDeprecatedAPITestServer(t, strings.Repeat("# padding\n", int(deprecatedAPIMaxResponseBytes/10)+1), nil)
		client := newNetworkTestClient(t, server)

		if _, err := client.DeprecatedAPIRequests(context.Background()); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("DeprecatedAPIRequests() error = %v, want upstream error", err)
		}
	})
}

func newDeprecatedAPITestServer(
	t *testing.T,
	payload string,
	assertRequest func(*http.Request),
) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if assertRequest != nil {
			assertRequest(r)
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		if _, err := io.WriteString(w, payload); err != nil {
			t.Errorf("write metrics: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}
