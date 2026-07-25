package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateClusterInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     ClusterInput
		wantField string
	}{
		{
			name: "valid",
			input: ClusterInput{
				Name:        "production-east",
				Environment: EnvironmentProduction,
				Server:      "https://api.example.com:6443",
				BearerToken: "a-service-account-token",
			},
		},
		{
			name: "empty name",
			input: ClusterInput{
				Environment: EnvironmentDevelopment,
				Server:      "https://api.example.com",
				BearerToken: "token",
			},
			wantField: "name",
		},
		{
			name: "unsupported environment",
			input: ClusterInput{
				Name:        "cluster",
				Environment: "sandbox",
				Server:      "https://api.example.com",
				BearerToken: "token",
			},
			wantField: "environment",
		},
		{
			name: "http server",
			input: ClusterInput{
				Name:        "cluster",
				Environment: EnvironmentStaging,
				Server:      "http://api.example.com",
				BearerToken: "token",
			},
			wantField: "server",
		},
		{
			name: "url with credentials",
			input: ClusterInput{
				Name:        "cluster",
				Environment: EnvironmentStaging,
				Server:      "https://user:pass@api.example.com",
				BearerToken: "token",
			},
			wantField: "server",
		},
		{
			name: "url with path",
			input: ClusterInput{
				Name:        "cluster",
				Environment: EnvironmentStaging,
				Server:      "https://api.example.com/kubernetes",
				BearerToken: "token",
			},
			wantField: "server",
		},
		{
			name: "empty token",
			input: ClusterInput{
				Name:        "cluster",
				Environment: EnvironmentStaging,
				Server:      "https://api.example.com",
			},
			wantField: "bearer_token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateClusterInput(tt.input)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("ValidateClusterInput() error = %v", err)
				}
				return
			}

			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("expected ValidationError, got %T (%v)", err, err)
			}
			if validationErr.Field != tt.wantField {
				t.Errorf("field = %q, want %q", validationErr.Field, tt.wantField)
			}
		})
	}
}

func TestValidateRepositoryInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     RepositoryInput
		wantField string
	}{
		{
			name:  "valid",
			input: RepositoryInput{Name: "stable", URL: "https://charts.example.com"},
		},
		{
			name:      "http denied",
			input:     RepositoryInput{Name: "stable", URL: "http://charts.example.com"},
			wantField: "url",
		},
		{
			name:      "query denied",
			input:     RepositoryInput{Name: "stable", URL: "https://charts.example.com?token=secret"},
			wantField: "url",
		},
		{
			name:      "password without username",
			input:     RepositoryInput{Name: "stable", URL: "https://charts.example.com", Password: "secret"},
			wantField: "username",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateRepositoryInput(tt.input)
			if tt.wantField == "" && err != nil {
				t.Fatalf("ValidateRepositoryInput() error = %v", err)
			}
			if tt.wantField == "" {
				return
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || validationErr.Field != tt.wantField {
				t.Fatalf("expected validation field %q, got %v", tt.wantField, err)
			}
		})
	}
}

func TestValidateHelmOperationInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     HelmOperationInput
		wantField string
	}{
		{
			name: "repository chart",
			input: HelmOperationInput{
				ClusterID:    "clu_123",
				Namespace:    "payments",
				ReleaseName:  "gateway",
				Chart:        "gateway",
				RepositoryID: "repo_123",
				Version:      "1.2.3",
			},
		},
		{
			name: "oci chart",
			input: HelmOperationInput{
				ClusterID:   "clu_123",
				Namespace:   "payments",
				ReleaseName: "gateway",
				Chart:       "oci://registry.example.com/charts/gateway",
				Version:     "1.2.3",
			},
		},
		{
			name: "path traversal chart",
			input: HelmOperationInput{
				ClusterID:    "clu_123",
				Namespace:    "payments",
				ReleaseName:  "gateway",
				Chart:        "../../local-chart",
				RepositoryID: "repo_123",
			},
			wantField: "chart",
		},
		{
			name: "local file chart",
			input: HelmOperationInput{
				ClusterID:   "clu_123",
				Namespace:   "payments",
				ReleaseName: "gateway",
				Chart:       "file:///tmp/chart",
			},
			wantField: "chart",
		},
		{
			name: "values too large",
			input: HelmOperationInput{
				ClusterID:    "clu_123",
				Namespace:    "payments",
				ReleaseName:  "gateway",
				Chart:        "gateway",
				RepositoryID: "repo_123",
				Values:       strings.Repeat("x", MaxHelmValuesBytes+1),
			},
			wantField: "values",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateHelmOperationInput(tt.input)
			if tt.wantField == "" && err != nil {
				t.Fatalf("ValidateHelmOperationInput() error = %v", err)
			}
			if tt.wantField == "" {
				return
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || validationErr.Field != tt.wantField {
				t.Fatalf("expected validation field %q, got %v", tt.wantField, err)
			}
		})
	}
}

func TestValidateWorkloadReference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     WorkloadReference
		wantField string
	}{
		{name: "deployment", input: WorkloadReference{Kind: "deployment", Namespace: "payments", Name: "gateway-api"}},
		{name: "pod", input: WorkloadReference{Kind: "pod", Namespace: "payments", Name: "gateway-api-6f778d8b4f-k7c2w"}},
		{name: "unknown kind", input: WorkloadReference{Kind: "secret", Namespace: "payments", Name: "credentials"}, wantField: "kind"},
		{name: "invalid namespace", input: WorkloadReference{Kind: "pod", Namespace: "../system", Name: "gateway"}, wantField: "namespace"},
		{name: "uppercase name", input: WorkloadReference{Kind: "pod", Namespace: "payments", Name: "Gateway"}, wantField: "name"},
		{name: "empty name", input: WorkloadReference{Kind: "pod", Namespace: "payments"}, wantField: "name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateWorkloadReference(tt.input)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("ValidateWorkloadReference() error = %v", err)
				}
				return
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || validationErr.Field != tt.wantField {
				t.Fatalf("expected validation field %q, got %v", tt.wantField, err)
			}
		})
	}
}

func TestValidatePodLogRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     PodLogRequest
		wantField string
	}{
		{name: "valid", input: PodLogRequest{Namespace: "payments", Pod: "gateway-0", Container: "app", TailLines: 200}},
		{name: "missing container", input: PodLogRequest{Namespace: "payments", Pod: "gateway-0", TailLines: 200}, wantField: "container"},
		{name: "invalid container", input: PodLogRequest{Namespace: "payments", Pod: "gateway-0", Container: "APP", TailLines: 200}, wantField: "container"},
		{name: "zero tail", input: PodLogRequest{Namespace: "payments", Pod: "gateway-0", Container: "app"}, wantField: "tail_lines"},
		{name: "excessive tail", input: PodLogRequest{Namespace: "payments", Pod: "gateway-0", Container: "app", TailLines: MaxPodLogTailLines + 1}, wantField: "tail_lines"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidatePodLogRequest(tt.input)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("ValidatePodLogRequest() error = %v", err)
				}
				return
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || validationErr.Field != tt.wantField {
				t.Fatalf("expected validation field %q, got %v", tt.wantField, err)
			}
		})
	}
}
