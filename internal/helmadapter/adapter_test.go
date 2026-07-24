package helmadapter

import (
	"context"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/kubernetes"
	"github.com/caoyanyi/k8s-panel/internal/outbound"
	"github.com/caoyanyi/k8s-panel/internal/platform"
)

func TestParseValues(t *testing.T) {
	t.Parallel()

	values, err := parseValues("replicaCount: 3\nimage:\n  tag: 1.4.0\n")
	if err != nil {
		t.Fatalf("parseValues() error = %v", err)
	}
	want := map[string]any{
		"replicaCount": float64(3),
		"image":        map[string]any{"tag": "1.4.0"},
	}
	if !reflect.DeepEqual(values, want) {
		t.Errorf("parseValues() = %#v, want %#v", values, want)
	}

	if _, err := parseValues("- one\n- two\n"); err == nil {
		t.Fatal("parseValues() accepted a sequence root")
	}
}

func TestChartOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		chart      string
		repository *platform.RepositoryConnection
		wantRepo   string
		wantErr    bool
	}{
		{
			name:       "configured repository",
			chart:      "gateway",
			repository: &platform.RepositoryConnection{URL: "https://charts.example.com", Username: "reader", Password: "secret"},
			wantRepo:   "https://charts.example.com",
		},
		{name: "OCI", chart: "oci://registry.example.com/charts/gateway"},
		{name: "local file", chart: "file:///tmp/chart", wantErr: true},
		{name: "absolute path", chart: "/tmp/chart", wantErr: true},
		{name: "repository missing", chart: "gateway", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			options, err := chartOptions(tt.chart, "1.2.3", tt.repository)
			if tt.wantErr {
				if err == nil {
					t.Fatal("chartOptions() error = nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("chartOptions() error = %v", err)
			}
			if options.RepoURL != tt.wantRepo || options.Version != "1.2.3" {
				t.Errorf("chartOptions() = %#v", options)
			}
			if tt.repository != nil && (options.Username != "reader" || options.Password != "secret") {
				t.Errorf("chartOptions() did not carry repository credentials")
			}
		})
	}
}

func TestSecureHTTPGetterRejectsNonHTTPSRedirect(t *testing.T) {
	t.Parallel()

	plainServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("chart"))
	}))
	defer plainServer.Close()
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plainServer.URL+"/chart.tgz", http.StatusFound)
	}))
	defer tlsServer.Close()

	roots := x509.NewCertPool()
	roots.AddCert(tlsServer.Certificate())
	policy := outbound.NewPolicy(net.DefaultResolver, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")})
	chartGetter, err := newSecureHTTPGetter(context.Background(), policy, roots, tlsServer.URL, "", "", time.Second)
	if err != nil {
		t.Fatalf("newSecureHTTPGetter() error = %v", err)
	}
	if _, err := chartGetter.Get(tlsServer.URL + "/chart.tgz"); err == nil {
		t.Fatal("Get() followed a redirect to plain HTTP")
	}
}

func TestActionConfigurationInjectsOutboundPolicy(t *testing.T) {
	t.Parallel()

	policy := outbound.NewPolicy(net.DefaultResolver, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")})
	_, settings, cleanup, err := actionConfiguration(
		kubernetes.Connection{Server: "https://127.0.0.1:6443", BearerToken: "token"},
		"default",
		policy,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("actionConfiguration() error = %v", err)
	}
	defer cleanup()
	restConfig, err := settings.RESTClientGetter().ToRESTConfig()
	if err != nil {
		t.Fatalf("ToRESTConfig() error = %v", err)
	}
	if restConfig.Dial == nil {
		t.Fatal("REST config has no policy dialer")
	}
	if restConfig.Proxy == nil {
		t.Fatal("REST config did not disable environment proxies")
	}
}
