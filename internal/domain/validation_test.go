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

func TestValidateNodeName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     string
		wantField string
	}{
		{name: "hostname", value: "worker-01.example.internal"},
		{name: "empty", wantField: "name"},
		{name: "uppercase", value: "Worker-01", wantField: "name"},
		{name: "path traversal", value: "../nodes", wantField: "name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateNodeName(tt.value)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("ValidateNodeName() error = %v", err)
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

func TestValidateWorkloadOperationInput(t *testing.T) {
	t.Parallel()

	replicas := int32(3)
	valid := WorkloadOperationInput{
		ClusterID:       "clu_one",
		Reference:       WorkloadReference{Kind: "deployment", Namespace: "payments", Name: "gateway"},
		ResourceVersion: "42",
		Replicas:        &replicas,
	}
	tests := []struct {
		name      string
		kind      OperationKind
		input     WorkloadOperationInput
		wantField string
	}{
		{name: "scale", kind: OperationWorkloadScale, input: valid},
		{name: "restart", kind: OperationWorkloadRestart, input: WorkloadOperationInput{
			ClusterID: valid.ClusterID, Reference: valid.Reference, ResourceVersion: valid.ResourceVersion,
		}},
		{name: "unsupported operation", kind: OperationHelmInstall, input: valid, wantField: "kind"},
		{name: "only deployment", kind: OperationWorkloadScale, input: WorkloadOperationInput{
			ClusterID:       valid.ClusterID,
			Reference:       WorkloadReference{Kind: "statefulset", Namespace: "payments", Name: "gateway"},
			ResourceVersion: valid.ResourceVersion, Replicas: &replicas,
		}, wantField: "kind"},
		{name: "missing resource version", kind: OperationWorkloadScale, input: WorkloadOperationInput{
			ClusterID: valid.ClusterID, Reference: valid.Reference, Replicas: &replicas,
		}, wantField: "resource_version"},
		{name: "missing replicas", kind: OperationWorkloadScale, input: WorkloadOperationInput{
			ClusterID: valid.ClusterID, Reference: valid.Reference, ResourceVersion: valid.ResourceVersion,
		}, wantField: "replicas"},
		{name: "excessive replicas", kind: OperationWorkloadScale, input: func() WorkloadOperationInput {
			value := int32(1001)
			input := valid
			input.Replicas = &value
			return input
		}(), wantField: "replicas"},
		{name: "restart rejects replicas", kind: OperationWorkloadRestart, input: valid, wantField: "replicas"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateWorkloadOperationInput(tt.kind, tt.input)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("ValidateWorkloadOperationInput() error = %v", err)
				}
				return
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || validationErr.Field != tt.wantField {
				t.Fatalf("validation error = %v, want field %q", err, tt.wantField)
			}
		})
	}
}

func TestValidateWorkloadImageOperationInput(t *testing.T) {
	t.Parallel()

	valid := WorkloadImageOperationInput{
		ClusterID: "clu_one",
		Change: WorkloadImageChange{
			Reference:       WorkloadReference{Kind: "deployment", Namespace: "payments", Name: "gateway"},
			ResourceVersion: "42",
			Container:       "app",
			CurrentImage:    "registry.example.com/gateway:1.4.0",
			Image:           "registry.example.com/gateway:1.5.0",
		},
	}
	tests := []struct {
		name      string
		input     WorkloadImageOperationInput
		wantField string
	}{
		{name: "valid", input: valid},
		{name: "only deployment", input: func() WorkloadImageOperationInput {
			input := valid
			input.Change.Reference.Kind = "statefulset"
			return input
		}(), wantField: "kind"},
		{name: "missing cluster", input: func() WorkloadImageOperationInput {
			input := valid
			input.ClusterID = ""
			return input
		}(), wantField: "cluster_id"},
		{name: "invalid container", input: func() WorkloadImageOperationInput {
			input := valid
			input.Change.Container = "APP"
			return input
		}(), wantField: "container"},
		{name: "unchanged image", input: func() WorkloadImageOperationInput {
			input := valid
			input.Change.Image = input.Change.CurrentImage
			return input
		}(), wantField: "image"},
		{name: "image whitespace", input: func() WorkloadImageOperationInput {
			input := valid
			input.Change.Image = "registry.example.com/gateway:1.5.0\n"
			return input
		}(), wantField: "image"},
		{name: "oversized image", input: func() WorkloadImageOperationInput {
			input := valid
			input.Change.Image = strings.Repeat("a", MaxContainerImageBytes+1)
			return input
		}(), wantField: "image"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateWorkloadImageOperationInput(tt.input)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("ValidateWorkloadImageOperationInput() error = %v", err)
				}
				return
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || validationErr.Field != tt.wantField {
				t.Fatalf("validation error = %v, want field %q", err, tt.wantField)
			}
		})
	}
}

func TestValidateOperationID(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		id        string
		wantError bool
	}{
		{name: "valid", id: "op_0123456789abcdef"},
		{name: "empty", id: "", wantError: true},
		{name: "path traversal", id: "../op_1", wantError: true},
		{name: "too long", id: strings.Repeat("a", 65), wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateOperationID(test.id)
			if test.wantError {
				var validationErr *ValidationError
				if !errors.As(err, &validationErr) || validationErr.Field != "operation_id" {
					t.Fatalf("ValidateOperationID() error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateOperationID() error = %v", err)
			}
		})
	}
}
