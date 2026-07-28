package kubernetes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

func TestClientReviewsServiceAccountAccessWithCanonicalIdentity(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/apis/authorization.k8s.io/v1/subjectaccessreviews" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Accept") != "application/json" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("review headers = Accept %q, Content-Type %q", r.Header.Get("Accept"), r.Header.Get("Content-Type"))
		}
		var body struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
			Spec       struct {
				User               string                              `json:"user"`
				Groups             []string                            `json:"groups"`
				ResourceAttributes domain.KubernetesResourceAttributes `json:"resourceAttributes"`
			} `json:"spec"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode SubjectAccessReview: %v", err)
		}
		if body.APIVersion != "authorization.k8s.io/v1" || body.Kind != "SubjectAccessReview" {
			t.Errorf("review type = %s/%s", body.APIVersion, body.Kind)
		}
		if body.Spec.User != "system:serviceaccount:payments:gateway" {
			t.Errorf("review user = %q", body.Spec.User)
		}
		wantGroups := []string{"system:serviceaccounts", "system:serviceaccounts:payments", "system:authenticated"}
		if len(body.Spec.Groups) != len(wantGroups) {
			t.Fatalf("review groups = %#v", body.Spec.Groups)
		}
		for index := range wantGroups {
			if body.Spec.Groups[index] != wantGroups[index] {
				t.Errorf("review groups = %#v, want %#v", body.Spec.Groups, wantGroups)
			}
		}
		wantAttributes := domain.KubernetesResourceAttributes{
			Group: "apps", Resource: "deployments", Subresource: "scale", Verb: "patch",
			Namespace: "payments", Name: "gateway-api",
		}
		if body.Spec.ResourceAttributes != wantAttributes {
			t.Errorf("resource attributes = %#v, want %#v", body.Spec.ResourceAttributes, wantAttributes)
		}
		writeTestJSON(t, w, map[string]any{
			"apiVersion": "authorization.k8s.io/v1", "kind": "SubjectAccessReview",
			"status": map[string]any{
				"allowed": true, "denied": false, "reason": "private role details",
				"evaluationError": "private authorizer details",
			},
		})
	}))
	t.Cleanup(server.Close)
	client := newNetworkTestClient(t, server)
	input := domain.KubernetesServiceAccountAccessReviewInput{
		ServiceAccount: domain.KubernetesServiceAccountReference{Namespace: "payments", Name: "gateway"},
		ResourceAttributes: domain.KubernetesResourceAttributes{
			Group: "apps", Resource: "deployments", Subresource: "scale", Verb: "patch",
			Namespace: "payments", Name: "gateway-api",
		},
	}

	state, err := client.ReviewServiceAccountAccess(context.Background(), input)
	if err != nil || state != domain.KubernetesCapabilityAllowed {
		t.Fatalf("ReviewServiceAccountAccess() = %q, %v", state, err)
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal access review state: %v", err)
	}
	if strings.Contains(string(encoded), "private") {
		t.Fatalf("access review leaked authorizer details: %s", encoded)
	}
}

func TestClientMapsServiceAccountAccessReviewStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status map[string]any
		want   domain.KubernetesCapabilityState
	}{
		{name: "allowed", status: map[string]any{"allowed": true}, want: domain.KubernetesCapabilityAllowed},
		{name: "denied", status: map[string]any{"allowed": false, "denied": true}, want: domain.KubernetesCapabilityDenied},
		{name: "no opinion", status: map[string]any{"allowed": false, "denied": false}, want: domain.KubernetesCapabilityIndeterminate},
		{name: "evaluation error", status: map[string]any{"allowed": false, "evaluationError": "private"}, want: domain.KubernetesCapabilityIndeterminate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeTestJSON(t, w, map[string]any{
					"apiVersion": "authorization.k8s.io/v1", "kind": "SubjectAccessReview", "status": tt.status,
				})
			}))
			t.Cleanup(server.Close)
			state, err := newNetworkTestClient(t, server).ReviewServiceAccountAccess(context.Background(), validAccessReviewInput())
			if err != nil || state != tt.want {
				t.Fatalf("ReviewServiceAccountAccess() = %q, %v, want %q", state, err, tt.want)
			}
		})
	}
}

func TestClientRejectsInvalidServiceAccountAccessReviewWithoutRequest(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	t.Cleanup(server.Close)
	input := validAccessReviewInput()
	input.ResourceAttributes.Resource = "*"
	if _, err := newNetworkTestClient(t, server).ReviewServiceAccountAccess(context.Background(), input); err == nil {
		t.Fatal("ReviewServiceAccountAccess() accepted a wildcard resource")
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("invalid access review made %d requests", got)
	}
}

func TestClientBoundsAndValidatesServiceAccountAccessReviewResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{name: "oversized", handler: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", int(maxCapabilityReviewBytes+1))))
		}},
		{name: "missing allowed", handler: func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, map[string]any{
				"apiVersion": "authorization.k8s.io/v1", "kind": "SubjectAccessReview",
				"status": map[string]any{"denied": true},
			})
		}},
		{name: "contradictory", handler: func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, map[string]any{
				"apiVersion": "authorization.k8s.io/v1", "kind": "SubjectAccessReview",
				"status": map[string]any{"allowed": true, "denied": true},
			})
		}},
		{name: "wrong kind", handler: func(w http.ResponseWriter, _ *http.Request) {
			writeTestJSON(t, w, map[string]any{
				"apiVersion": "authorization.k8s.io/v1", "kind": "SelfSubjectAccessReview",
				"status": map[string]any{"allowed": true},
			})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(tt.handler)
			t.Cleanup(server.Close)
			if _, err := newNetworkTestClient(t, server).ReviewServiceAccountAccess(context.Background(), validAccessReviewInput()); !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("ReviewServiceAccountAccess() error = %v, want ErrUpstream", err)
			}
		})
	}
}

func validAccessReviewInput() domain.KubernetesServiceAccountAccessReviewInput {
	return domain.KubernetesServiceAccountAccessReviewInput{
		ServiceAccount:     domain.KubernetesServiceAccountReference{Namespace: "payments", Name: "gateway"},
		ResourceAttributes: domain.KubernetesResourceAttributes{Resource: "pods", Verb: "get", Namespace: "payments"},
	}
}
