package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestClientReadsBoundedAPIServerReadiness(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.RequestURI() != "/readyz?verbose" {
			t.Errorf("request = %s %s, want GET /readyz?verbose", r.Method, r.URL.RequestURI())
		}
		if got := r.Header.Get("Accept"); got != "text/plain" {
			t.Errorf("Accept = %q, want text/plain", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want bearer token", got)
		}
		_, _ = io.WriteString(w, "[+]ping ok\n[+]poststarthook/start-kube-apiserver-admission-initializer ok\nreadyz check passed\n")
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	startedAt := time.Now().UTC()
	evidence, err := client.APIServerReadiness(context.Background())
	finishedAt := time.Now().UTC()
	if err != nil {
		t.Fatalf("APIServerReadiness() error = %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
	if !evidence.Ready || evidence.PassedChecks != 2 || evidence.FailedChecks != 0 || len(evidence.Checks) != 2 {
		t.Fatalf("APIServerReadiness() = %#v", evidence)
	}
	if evidence.Checks[0].Name != "ping" || evidence.Checks[0].Status != domain.APIServerReadinessCheckPassed ||
		evidence.Checks[1].Name != "poststarthook/start-kube-apiserver-admission-initializer" ||
		evidence.Checks[1].Status != domain.APIServerReadinessCheckPassed {
		t.Fatalf("checks = %#v", evidence.Checks)
	}
	if evidence.ObservedAt.Before(startedAt) || evidence.ObservedAt.After(finishedAt) {
		t.Fatalf("observed_at = %s, want between %s and %s", evidence.ObservedAt, startedAt, finishedAt)
	}
}

func TestClientProjectsFailedAPIServerReadinessWithoutDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "[+]ping ok\n[-]etcd failed: private-etcd.example.com:2379 dial timeout\nreadyz check failed\n")
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	evidence, err := client.APIServerReadiness(context.Background())
	if err != nil {
		t.Fatalf("APIServerReadiness() error = %v", err)
	}
	if evidence.Ready || evidence.PassedChecks != 1 || evidence.FailedChecks != 1 || len(evidence.Checks) != 2 ||
		evidence.Checks[1].Name != "etcd" || evidence.Checks[1].Status != domain.APIServerReadinessCheckFailed {
		t.Fatalf("APIServerReadiness() = %#v", evidence)
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	for _, forbidden := range []string{"private-etcd", "2379", "timeout", "failed:"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("readiness evidence leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestParseAPIServerReadinessRejectsUntrustedOutput(t *testing.T) {
	t.Parallel()

	tooManyChecks := strings.Builder{}
	for index := 0; index <= apiServerReadinessMaxChecks; index++ {
		fmt.Fprintf(&tooManyChecks, "[+]check-%03d ok\n", index)
	}
	tooManyChecks.WriteString("readyz check passed\n")

	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{name: "empty response", statusCode: http.StatusOK},
		{name: "missing summary", statusCode: http.StatusOK, body: "[+]ping ok\n"},
		{name: "missing checks", statusCode: http.StatusOK, body: "readyz check passed\n"},
		{name: "duplicate check", statusCode: http.StatusOK, body: "[+]ping ok\n[+]ping ok\nreadyz check passed\n"},
		{name: "duplicate summary", statusCode: http.StatusOK, body: "[+]ping ok\nreadyz check passed\nreadyz check passed\n"},
		{name: "unexpected line", statusCode: http.StatusOK, body: "[+]ping ok\nprivate response\nreadyz check passed\n"},
		{name: "ready response with failure", statusCode: http.StatusOK, body: "[-]etcd failed: private\nreadyz check passed\n"},
		{name: "failed response without failure", statusCode: http.StatusInternalServerError, body: "[+]ping ok\nreadyz check failed\n"},
		{name: "failed status with passed summary", statusCode: http.StatusInternalServerError, body: "[-]etcd failed: private\nreadyz check passed\n"},
		{name: "invalid status", statusCode: http.StatusServiceUnavailable, body: "[-]etcd failed: private\nreadyz check failed\n"},
		{name: "whitespace in name", statusCode: http.StatusInternalServerError, body: "[-]etcd connection failed: private\nreadyz check failed\n"},
		{name: "non ascii name", statusCode: http.StatusOK, body: "[+]检查 ok\nreadyz check passed\n"},
		{name: "traversal name", statusCode: http.StatusOK, body: "[+]poststarthook/../private ok\nreadyz check passed\n"},
		{name: "empty path segment", statusCode: http.StatusOK, body: "[+]poststarthook//private ok\nreadyz check passed\n"},
		{name: "line too long", statusCode: http.StatusInternalServerError, body: "[-]etcd failed:" + strings.Repeat("x", apiServerReadinessMaxLineBytes) + "\nreadyz check failed\n"},
		{name: "too many checks", statusCode: http.StatusOK, body: tooManyChecks.String()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseAPIServerReadiness([]byte(tt.body), tt.statusCode, time.Now().UTC())
			if !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("parseAPIServerReadiness() error = %v, want upstream error", err)
			}
			if strings.Contains(fmt.Sprint(err), "private") {
				t.Fatalf("error leaked response content: %v", err)
			}
		})
	}
}

func TestParseAPIServerReadinessAcceptsCRLFAndCompatibleSummary(t *testing.T) {
	t.Parallel()

	evidence, err := parseAPIServerReadiness(
		[]byte("[+]ping ok\r\n[-]etcd failed: private detail\r\nhealthz check failed\r\n"),
		http.StatusInternalServerError,
		time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC),
	)
	if err != nil || evidence.Ready || evidence.PassedChecks != 1 || evidence.FailedChecks != 1 {
		t.Fatalf("parseAPIServerReadiness() = %#v, %v", evidence, err)
	}
}

func TestClientBoundsAPIServerReadinessResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", int(apiServerReadinessMaxResponseBytes)+1))
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)

	if _, err := client.APIServerReadiness(context.Background()); !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("APIServerReadiness() error = %v, want upstream error", err)
	}
}

func TestClientMapsAPIServerReadinessHTTPFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status  int
		wantErr error
	}{
		{status: http.StatusUnauthorized, wantErr: domain.ErrUnauthorized},
		{status: http.StatusForbidden, wantErr: domain.ErrForbidden},
		{status: http.StatusNotFound, wantErr: domain.ErrNotFound},
		{status: http.StatusTooManyRequests, wantErr: domain.ErrUpstream},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "private upstream response", tt.status)
			}))
			t.Cleanup(server.Close)
			client := newNetworkTestClient(t, server)

			_, err := client.APIServerReadiness(context.Background())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("APIServerReadiness() error = %v, want %v", err, tt.wantErr)
			}
			if strings.Contains(fmt.Sprint(err), "private upstream response") {
				t.Fatalf("error leaked upstream response: %v", err)
			}
		})
	}
}

func TestClientMapsAPIServerReadinessTransportFailures(t *testing.T) {
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
			client := newAPIServerReadinessTransportClient(apiServerReadinessRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, tt.failure
			}))

			_, err := client.APIServerReadiness(context.Background())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("APIServerReadiness() error = %v, want %v", err, tt.wantErr)
			}
			if strings.Contains(fmt.Sprint(err), "private network address") {
				t.Fatalf("error leaked transport details: %v", err)
			}
		})
	}
}

type apiServerReadinessRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn apiServerReadinessRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func newAPIServerReadinessTransportClient(transport http.RoundTripper) *Client {
	baseURL, _ := url.Parse("https://api.example.com")
	return &Client{
		baseURL: baseURL,
		token:   "test-token",
		http:    &http.Client{Transport: transport},
	}
}
