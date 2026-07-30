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

func TestValidateClusterCredentialRotationInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     ClusterCredentialRotationInput
		wantField string
	}{
		{
			name: "valid",
			input: ClusterCredentialRotationInput{
				CACert:       "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----",
				BearerToken:  "new-service-account-token",
				Confirmation: "production-east",
			},
		},
		{name: "empty token", input: ClusterCredentialRotationInput{Confirmation: "production-east"}, wantField: "bearer_token"},
		{
			name:      "token with surrounding whitespace",
			input:     ClusterCredentialRotationInput{BearerToken: " token ", Confirmation: "production-east"},
			wantField: "bearer_token",
		},
		{
			name:      "token with control character",
			input:     ClusterCredentialRotationInput{BearerToken: "token\nvalue", Confirmation: "production-east"},
			wantField: "bearer_token",
		},
		{
			name:      "oversized token",
			input:     ClusterCredentialRotationInput{BearerToken: strings.Repeat("a", 64*1024+1), Confirmation: "production-east"},
			wantField: "bearer_token",
		},
		{
			name:      "oversized CA",
			input:     ClusterCredentialRotationInput{CACert: strings.Repeat("a", 256*1024+1), BearerToken: "token", Confirmation: "production-east"},
			wantField: "ca_cert",
		},
		{name: "empty confirmation", input: ClusterCredentialRotationInput{BearerToken: "token"}, wantField: "confirmation"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateClusterCredentialRotationInput(tt.input)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("ValidateClusterCredentialRotationInput() error = %v", err)
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

func TestValidateNamespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		namespace string
		valid     bool
	}{
		{name: "valid", namespace: "payments", valid: true},
		{name: "valid with digits", namespace: "team-2", valid: true},
		{name: "empty", namespace: ""},
		{name: "uppercase", namespace: "Payments"},
		{name: "dot", namespace: "team.prod"},
		{name: "too long", namespace: strings.Repeat("a", 64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateNamespace(tt.namespace)
			if tt.valid && err != nil {
				t.Fatalf("ValidateNamespace() error = %v", err)
			}
			if !tt.valid {
				var validationErr *ValidationError
				if !errors.As(err, &validationErr) || validationErr.Field != "namespace" {
					t.Fatalf("ValidateNamespace() error = %v, want namespace validation error", err)
				}
			}
		})
	}
}

func TestValidateAccessResourceScopeAndReference(t *testing.T) {
	t.Parallel()

	validScopes := []KubernetesAccessResourceReference{
		{Kind: AccessResourceServiceAccounts, Namespace: "payments"},
		{Kind: AccessResourceRoles, Namespace: "payments"},
		{Kind: AccessResourceRoleBindings, Namespace: "payments"},
		{Kind: AccessResourceClusterRoles},
		{Kind: AccessResourceClusterRoleBindings},
	}
	for _, reference := range validScopes {
		if err := ValidateAccessResourceScope(reference.Kind, reference.Namespace); err != nil {
			t.Errorf("ValidateAccessResourceScope(%q, %q) error = %v", reference.Kind, reference.Namespace, err)
		}
	}

	tests := []struct {
		name      string
		reference KubernetesAccessResourceReference
		wantField string
	}{
		{name: "unknown kind", reference: KubernetesAccessResourceReference{Kind: "secrets", Namespace: "payments"}, wantField: "kind"},
		{name: "missing namespace", reference: KubernetesAccessResourceReference{Kind: AccessResourceRoles}, wantField: "namespace"},
		{name: "invalid namespace", reference: KubernetesAccessResourceReference{Kind: AccessResourceRoleBindings, Namespace: "bad/namespace"}, wantField: "namespace"},
		{name: "cluster scope with namespace", reference: KubernetesAccessResourceReference{Kind: AccessResourceClusterRoles, Namespace: "payments"}, wantField: "namespace"},
		{name: "missing name", reference: KubernetesAccessResourceReference{Kind: AccessResourceRoles, Namespace: "payments"}, wantField: "name"},
		{name: "invalid name", reference: KubernetesAccessResourceReference{Kind: AccessResourceClusterRoleBindings, Name: "../binding"}, wantField: "name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateAccessResourceReference(tt.reference)
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || validationErr.Field != tt.wantField {
				t.Fatalf("ValidateAccessResourceReference() error = %v, want field %q", err, tt.wantField)
			}
		})
	}

	validDetail := KubernetesAccessResourceReference{
		Kind: AccessResourceRoleBindings, Namespace: "payments", Name: "gateway-readers",
	}
	if err := ValidateAccessResourceReference(validDetail); err != nil {
		t.Fatalf("ValidateAccessResourceReference() error = %v", err)
	}
	if err := ValidateAccessResourceReference(KubernetesAccessResourceReference{
		Kind: AccessResourceClusterRoles, Name: "system:discovery",
	}); err != nil {
		t.Fatalf("ValidateAccessResourceReference(system RBAC name) error = %v", err)
	}
}

func TestValidateServiceAccountAccessReviewInput(t *testing.T) {
	t.Parallel()

	valid := KubernetesServiceAccountAccessReviewInput{
		ServiceAccount: KubernetesServiceAccountReference{Namespace: "payments", Name: "gateway"},
		ResourceAttributes: KubernetesResourceAttributes{
			Group: "apps", Resource: "deployments", Subresource: "scale", Verb: "patch",
			Namespace: "payments", Name: "gateway-api",
		},
	}
	if err := ValidateServiceAccountAccessReviewInput(valid); err != nil {
		t.Fatalf("ValidateServiceAccountAccessReviewInput() error = %v", err)
	}
	clusterScoped := valid
	clusterScoped.ResourceAttributes = KubernetesResourceAttributes{Resource: "nodes", Verb: "list"}
	if err := ValidateServiceAccountAccessReviewInput(clusterScoped); err != nil {
		t.Fatalf("ValidateServiceAccountAccessReviewInput(cluster scoped) error = %v", err)
	}

	tests := []struct {
		name      string
		mutate    func(*KubernetesServiceAccountAccessReviewInput)
		wantField string
	}{
		{name: "invalid service account namespace", mutate: func(input *KubernetesServiceAccountAccessReviewInput) {
			input.ServiceAccount.Namespace = "bad/namespace"
		}, wantField: "service_account.namespace"},
		{name: "invalid service account name", mutate: func(input *KubernetesServiceAccountAccessReviewInput) {
			input.ServiceAccount.Name = "Gateway"
		}, wantField: "service_account.name"},
		{name: "wildcard group", mutate: func(input *KubernetesServiceAccountAccessReviewInput) {
			input.ResourceAttributes.Group = "*"
		}, wantField: "resource_attributes.group"},
		{name: "wildcard resource", mutate: func(input *KubernetesServiceAccountAccessReviewInput) {
			input.ResourceAttributes.Resource = "*"
		}, wantField: "resource_attributes.resource"},
		{name: "invalid subresource", mutate: func(input *KubernetesServiceAccountAccessReviewInput) {
			input.ResourceAttributes.Subresource = "pod/log"
		}, wantField: "resource_attributes.subresource"},
		{name: "unknown verb", mutate: func(input *KubernetesServiceAccountAccessReviewInput) {
			input.ResourceAttributes.Verb = "admin"
		}, wantField: "resource_attributes.verb"},
		{name: "invalid target namespace", mutate: func(input *KubernetesServiceAccountAccessReviewInput) {
			input.ResourceAttributes.Namespace = "Payments"
		}, wantField: "resource_attributes.namespace"},
		{name: "unsafe object name", mutate: func(input *KubernetesServiceAccountAccessReviewInput) {
			input.ResourceAttributes.Name = "../secret"
		}, wantField: "resource_attributes.name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := valid
			tt.mutate(&input)
			err := ValidateServiceAccountAccessReviewInput(input)
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || validationErr.Field != tt.wantField {
				t.Fatalf("ValidateServiceAccountAccessReviewInput() error = %v, want field %q", err, tt.wantField)
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
		{name: "job", input: WorkloadReference{Kind: "job", Namespace: "payments", Name: "daily-settlement"}},
		{name: "cronjob", input: WorkloadReference{Kind: "cronjob", Namespace: "payments", Name: "nightly-report"}},
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

func TestValidateWorkloadList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		namespace string
		kind      string
		wantField string
	}{
		{name: "all kinds and namespaces"},
		{name: "Job", namespace: "payments", kind: "job"},
		{name: "CronJob", namespace: "payments", kind: "cronjob"},
		{name: "invalid namespace", namespace: "../system", kind: "job", wantField: "namespace"},
		{name: "invalid kind", namespace: "payments", kind: "secret", wantField: "kind"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateWorkloadList(tt.namespace, tt.kind)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("ValidateWorkloadList() error = %v", err)
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

func TestValidateCustomResourceDefinitionName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     string
		wantField string
	}{
		{name: "valid", value: "widgets.platform.example.com"},
		{name: "missing group", value: "widgets", wantField: "name"},
		{name: "missing resource", value: ".platform.example.com", wantField: "name"},
		{name: "uppercase", value: "Widgets.platform.example.com", wantField: "name"},
		{name: "path traversal", value: "../customresourcedefinitions", wantField: "name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateCustomResourceDefinitionName(tt.value)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("ValidateCustomResourceDefinitionName() error = %v", err)
				}
				return
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || validationErr.Field != tt.wantField {
				t.Fatalf("ValidateCustomResourceDefinitionName() error = %v, want field %q", err, tt.wantField)
			}
		})
	}
}

func TestValidateCertificateSigningRequestName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     string
		wantField string
	}{
		{name: "dns label", value: "worker-01"},
		{name: "dns subdomain", value: "worker-01.example.com"},
		{name: "empty", value: "", wantField: "name"},
		{name: "uppercase", value: "Worker-01", wantField: "name"},
		{name: "path traversal", value: "../certificatesigningrequests", wantField: "name"},
		{name: "too long", value: strings.Repeat("a", 254), wantField: "name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateCertificateSigningRequestName(tt.value)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("ValidateCertificateSigningRequestName() error = %v", err)
				}
				return
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || validationErr.Field != tt.wantField {
				t.Fatalf("ValidateCertificateSigningRequestName() error = %v, want field %q", err, tt.wantField)
			}
		})
	}
}

func TestValidatePriorityClassName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     string
		wantField string
	}{
		{name: "dns label", value: "workload-high"},
		{name: "dns subdomain", value: "batch.platform.example.com"},
		{name: "system class", value: "system-cluster-critical"},
		{name: "empty", value: "", wantField: "name"},
		{name: "uppercase", value: "Workload-High", wantField: "name"},
		{name: "path traversal", value: "../priorityclasses", wantField: "name"},
		{name: "too long", value: strings.Repeat("a", 254), wantField: "name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidatePriorityClassName(tt.value)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("ValidatePriorityClassName() error = %v", err)
				}
				return
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || validationErr.Field != tt.wantField {
				t.Fatalf("ValidatePriorityClassName() error = %v, want field %q", err, tt.wantField)
			}
		})
	}
}

func TestValidateAPIServiceName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     string
		wantField string
	}{
		{name: "aggregated API", value: "v1beta1.metrics.k8s.io"},
		{name: "legacy core API", value: "v1."},
		{name: "missing separator", value: "v1", wantField: "name"},
		{name: "missing version", value: ".metrics.k8s.io", wantField: "name"},
		{name: "empty group label", value: "v1..metrics.k8s.io", wantField: "name"},
		{name: "uppercase", value: "V1.metrics.k8s.io", wantField: "name"},
		{name: "path traversal", value: "../apiservices", wantField: "name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateAPIServiceName(tt.value)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("ValidateAPIServiceName() error = %v", err)
				}
				return
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || validationErr.Field != tt.wantField {
				t.Fatalf("ValidateAPIServiceName() error = %v, want field %q", err, tt.wantField)
			}
		})
	}
}

func TestValidateAdmissionWebhookConfigurationKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     KubernetesAdmissionWebhookConfigurationKind
		wantField string
	}{
		{name: "validating", value: AdmissionWebhookConfigurationValidating},
		{name: "mutating", value: AdmissionWebhookConfigurationMutating},
		{name: "empty", wantField: "kind"},
		{name: "uppercase", value: "Validating", wantField: "kind"},
		{name: "unknown", value: "policy", wantField: "kind"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateAdmissionWebhookConfigurationKind(tt.value)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("ValidateAdmissionWebhookConfigurationKind() error = %v", err)
				}
				return
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || validationErr.Field != tt.wantField {
				t.Fatalf("ValidateAdmissionWebhookConfigurationKind() error = %v, want field %q", err, tt.wantField)
			}
		})
	}
}

func TestValidateAdmissionWebhookConfigurationName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     string
		wantField string
	}{
		{name: "valid", value: "policy.platform.example.com"},
		{name: "single label", value: "policy"},
		{name: "empty", wantField: "name"},
		{name: "uppercase", value: "Policy.platform.example.com", wantField: "name"},
		{name: "path traversal", value: "../validatingwebhookconfigurations", wantField: "name"},
		{name: "too long", value: strings.Repeat("a", 254), wantField: "name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateAdmissionWebhookConfigurationName(tt.value)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("ValidateAdmissionWebhookConfigurationName() error = %v", err)
				}
				return
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || validationErr.Field != tt.wantField {
				t.Fatalf("ValidateAdmissionWebhookConfigurationName() error = %v, want field %q", err, tt.wantField)
			}
		})
	}
}

func TestValidateAdmissionPolicyResourceName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     string
		wantError bool
	}{
		{name: "qualified name", value: "replica-policy.platform.example.com"},
		{name: "single label", value: "policy"},
		{name: "empty", value: "", wantError: true},
		{name: "path traversal", value: "../validatingadmissionpolicies", wantError: true},
		{name: "uppercase", value: "Policy.example.com", wantError: true},
		{name: "too long", value: strings.Repeat("a", 254), wantError: true},
	}

	for _, tt := range tests {
		test := tt
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateAdmissionPolicyResourceName(test.value)
			if test.wantError {
				if err == nil {
					t.Fatalf("ValidateAdmissionPolicyResourceName(%q) accepted invalid value", test.value)
				}
				var validationErr *ValidationError
				if !errors.As(err, &validationErr) || validationErr.Field != "name" {
					t.Fatalf("ValidateAdmissionPolicyResourceName() error = %v, want name validation error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateAdmissionPolicyResourceName(%q) error = %v", test.value, err)
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
