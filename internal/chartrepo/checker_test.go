package chartrepo

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/caoyanyi/k8s-panel/internal/outbound"
	"github.com/caoyanyi/k8s-panel/internal/platform"
)

func TestCheckerValidatesIndexAndBasicAuth(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "charts" || password != "repo-password" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/stable/index.yaml" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte("apiVersion: v1\nentries:\n  gateway:\n    - version: 1.2.3\n"))
	}))
	t.Cleanup(server.Close)

	checker := NewChecker(
		outbound.NewPolicy(systemResolver{}, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}),
		server.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs,
	)
	err := checker.Check(context.Background(), platform.RepositoryConnection{
		URL: server.URL + "/stable", Username: "charts", Password: "repo-password",
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
}

func TestCheckerRejectsInvalidIndex(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("apiVersion: v1\nentries: []\n"))
	}))
	t.Cleanup(server.Close)
	checker := NewChecker(
		outbound.NewPolicy(systemResolver{}, []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}),
		server.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs,
	)
	if err := checker.Check(context.Background(), platform.RepositoryConnection{URL: server.URL}); err == nil {
		t.Fatal("Check() accepted an index without chart entries")
	}
}

type systemResolver struct{}

func (systemResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, network, host)
}
