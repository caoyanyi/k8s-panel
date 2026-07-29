package kubernetes

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

func TestClientReadsVerifiedEndpointCertificateEvidence(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.RequestURI() != "/version" {
			t.Errorf("request = %s %s, want GET /version", r.Method, r.URL.RequestURI())
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want bearer token", got)
		}
		writeTestJSON(t, w, map[string]any{
			"gitVersion": "private-version",
			"private":    "must-not-be-projected",
		})
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	startedAt := time.Now().UTC()
	evidence, err := client.EndpointCertificate(context.Background())
	finishedAt := time.Now().UTC()
	if err != nil {
		t.Fatalf("EndpointCertificate() error = %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
	if evidence.ObservedAt.Before(startedAt) || evidence.ObservedAt.After(finishedAt) {
		t.Errorf("observed_at = %s, want between %s and %s", evidence.ObservedAt, startedAt, finishedAt)
	}
	wantCertificate := server.Certificate()
	if !evidence.NotBefore.Equal(wantCertificate.NotBefore.UTC()) || !evidence.NotAfter.Equal(wantCertificate.NotAfter.UTC()) {
		t.Errorf("certificate bounds = %s..%s, want %s..%s", evidence.NotBefore, evidence.NotAfter, wantCertificate.NotBefore, wantCertificate.NotAfter)
	}
	if evidence.RemainingSeconds <= int64((30*24*time.Hour)/time.Second) || evidence.Status != domain.EndpointCertificateValid {
		t.Errorf("certificate evidence = %#v, want valid with more than 30 days remaining", evidence)
	}

	encoded, err := json.Marshal(evidence)
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	for _, forbidden := range []string{"private-version", "must-not-be-projected", "subject", "issuer", "serial", "san", "pem"} {
		if strings.Contains(strings.ToLower(string(encoded)), forbidden) {
			t.Fatalf("endpoint certificate evidence leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestProjectEndpointCertificateClassifiesFixedThresholds(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		remaining   time.Duration
		wantSeconds int64
		wantStatus  domain.KubernetesEndpointCertificateStatus
	}{
		{name: "more than thirty days", remaining: 31 * 24 * time.Hour, wantSeconds: 2678400, wantStatus: domain.EndpointCertificateValid},
		{name: "exactly thirty days", remaining: 30 * 24 * time.Hour, wantSeconds: 2592000, wantStatus: domain.EndpointCertificateExpiring},
		{name: "more than seven days", remaining: 8 * 24 * time.Hour, wantSeconds: 691200, wantStatus: domain.EndpointCertificateExpiring},
		{name: "exactly seven days", remaining: 7 * 24 * time.Hour, wantSeconds: 604800, wantStatus: domain.EndpointCertificateCritical},
		{name: "positive subsecond rounds up", remaining: 500 * time.Millisecond, wantSeconds: 1, wantStatus: domain.EndpointCertificateCritical},
		{name: "expires now", remaining: 0, wantSeconds: 0, wantStatus: domain.EndpointCertificateExpired},
		{name: "already expired", remaining: -time.Second, wantSeconds: -1, wantStatus: domain.EndpointCertificateExpired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			certificate := &x509.Certificate{
				NotBefore: observedAt.Add(-365 * 24 * time.Hour),
				NotAfter:  observedAt.Add(tt.remaining),
			}
			got := projectEndpointCertificate(certificate, observedAt)
			if got.ObservedAt != observedAt || got.NotBefore != certificate.NotBefore || got.NotAfter != certificate.NotAfter ||
				got.RemainingSeconds != tt.wantSeconds || got.Status != tt.wantStatus {
				t.Fatalf("projectEndpointCertificate() = %#v", got)
			}
		})
	}
}

func TestClientRejectsUnverifiedEndpointCertificateState(t *testing.T) {
	t.Parallel()

	certificate := &x509.Certificate{
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter:  time.Now().Add(time.Hour),
	}
	tests := []struct {
		name  string
		state *tls.ConnectionState
	}{
		{name: "missing TLS state"},
		{name: "incomplete handshake", state: &tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{certificate},
			VerifiedChains:   [][]*x509.Certificate{{certificate}},
		}},
		{name: "missing peer certificate", state: &tls.ConnectionState{HandshakeComplete: true}},
		{name: "missing verified chain", state: &tls.ConnectionState{
			HandshakeComplete: true,
			PeerCertificates:  []*x509.Certificate{certificate},
		}},
		{name: "verified chain leaf mismatch", state: &tls.ConnectionState{
			HandshakeComplete: true,
			PeerCertificates:  []*x509.Certificate{{Raw: []byte("peer-leaf")}},
			VerifiedChains:    [][]*x509.Certificate{{{Raw: []byte("different-leaf")}}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := newEndpointCertificateResponseClient(&http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"private":"response"}`)),
				TLS:        tt.state,
			})
			_, err := client.EndpointCertificate(context.Background())
			if !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("EndpointCertificate() error = %v, want upstream error", err)
			}
			if strings.Contains(err.Error(), "response") {
				t.Fatalf("error leaked response content: %v", err)
			}
		})
	}
}

func TestClientBoundsEndpointCertificateResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := io.WriteString(w, strings.Repeat("x", endpointCertificateMaxResponseBytes+1)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	if _, err := client.EndpointCertificate(context.Background()); !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("EndpointCertificate() error = %v, want upstream error", err)
	}
}

func TestClientMapsEndpointCertificateHTTPFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status  int
		wantErr error
	}{
		{status: http.StatusUnauthorized, wantErr: domain.ErrUnauthorized},
		{status: http.StatusForbidden, wantErr: domain.ErrForbidden},
		{status: http.StatusNotFound, wantErr: domain.ErrNotFound},
		{status: http.StatusConflict, wantErr: domain.ErrConflict},
		{status: http.StatusInternalServerError, wantErr: domain.ErrUpstream},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "private upstream response", tt.status)
			}))
			t.Cleanup(server.Close)
			client := newNetworkTestClient(t, server)

			_, err := client.EndpointCertificate(context.Background())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("EndpointCertificate() error = %v, want %v", err, tt.wantErr)
			}
			if strings.Contains(err.Error(), "private upstream response") {
				t.Fatalf("error leaked upstream response: %v", err)
			}
		})
	}
}

func TestClientMapsEndpointCertificateTransportFailuresWithoutLeakingDetails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		failure error
		wantErr error
	}{
		{name: "canceled", failure: context.Canceled, wantErr: context.Canceled},
		{name: "deadline", failure: context.DeadlineExceeded, wantErr: domain.ErrTimeout},
		{name: "network", failure: errors.New("private network address"), wantErr: domain.ErrUpstream},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := newEndpointCertificateTransportClient(endpointCertificateRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, tt.failure
			}))

			_, err := client.EndpointCertificate(context.Background())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("EndpointCertificate() error = %v, want %v", err, tt.wantErr)
			}
			if strings.Contains(err.Error(), "private network address") {
				t.Fatalf("error leaked transport details: %v", err)
			}
		})
	}
}

func TestClientRejectsEndpointCertificateResponseReadFailure(t *testing.T) {
	t.Parallel()

	certificate := &x509.Certificate{Raw: []byte("verified-leaf")}
	client := newEndpointCertificateResponseClient(&http.Response{
		StatusCode: http.StatusOK,
		Body:       endpointCertificateErrorBody{},
		TLS: &tls.ConnectionState{
			HandshakeComplete: true,
			PeerCertificates:  []*x509.Certificate{certificate},
			VerifiedChains:    [][]*x509.Certificate{{certificate}},
		},
	})

	_, err := client.EndpointCertificate(context.Background())
	if !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("EndpointCertificate() error = %v, want upstream error", err)
	}
	if strings.Contains(err.Error(), "private response body") {
		t.Fatalf("error leaked response read details: %v", err)
	}
}

type endpointCertificateRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn endpointCertificateRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type endpointCertificateErrorBody struct{}

func (endpointCertificateErrorBody) Read([]byte) (int, error) {
	return 0, errors.New("private response body")
}

func (endpointCertificateErrorBody) Close() error { return nil }

func newEndpointCertificateResponseClient(response *http.Response) *Client {
	return newEndpointCertificateTransportClient(endpointCertificateRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return response, nil
	}))
}

func newEndpointCertificateTransportClient(transport http.RoundTripper) *Client {
	baseURL, _ := url.Parse("https://api.example.com")
	return &Client{
		baseURL: baseURL,
		token:   "test-token",
		http:    &http.Client{Transport: transport},
	}
}
