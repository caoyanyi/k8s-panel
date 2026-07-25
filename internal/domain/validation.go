package domain

import (
	"net/url"
	"regexp"
	"strings"
)

const MaxHelmValuesBytes = 256 * 1024

var (
	resourceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	dnsLabelPattern     = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	dnsSubdomainPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)
	chartNamePattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)
)

func ValidateClusterInput(input ClusterInput) error {
	if !resourceNamePattern.MatchString(input.Name) {
		return Invalid("name", "must be 1-64 letters, numbers, dots, underscores or hyphens")
	}
	if input.Environment != EnvironmentDevelopment &&
		input.Environment != EnvironmentStaging &&
		input.Environment != EnvironmentProduction {
		return Invalid("environment", "must be development, staging or production")
	}
	parsed, err := validateHTTPSURL(input.Server, false)
	if err != nil || (parsed.Path != "" && parsed.Path != "/") {
		return Invalid("server", "must be an HTTPS origin without credentials, path, query or fragment")
	}
	if strings.TrimSpace(input.BearerToken) == "" {
		return Invalid("bearer_token", "is required")
	}
	return nil
}

func ValidateRepositoryInput(input RepositoryInput) error {
	if !resourceNamePattern.MatchString(input.Name) {
		return Invalid("name", "must be 1-64 letters, numbers, dots, underscores or hyphens")
	}
	if _, err := validateHTTPSURL(input.URL, true); err != nil {
		return Invalid("url", "must be an HTTPS URL without credentials, query or fragment")
	}
	if input.Password != "" && strings.TrimSpace(input.Username) == "" {
		return Invalid("username", "is required when a password is configured")
	}
	return nil
}

func ValidateHelmOperationInput(input HelmOperationInput) error {
	if strings.TrimSpace(input.ClusterID) == "" {
		return Invalid("cluster_id", "is required")
	}
	if !validDNSLabel(input.Namespace) {
		return Invalid("namespace", "must be a valid Kubernetes namespace")
	}
	if !validDNSLabel(input.ReleaseName) {
		return Invalid("release_name", "must be a valid Helm release name")
	}
	if len(input.Values) > MaxHelmValuesBytes {
		return Invalid("values", "must not exceed 256 KiB")
	}

	chart := strings.TrimSpace(input.Chart)
	if strings.HasPrefix(chart, "oci://") {
		parsed, err := url.Parse(chart)
		if err != nil || parsed.Host == "" || parsed.Path == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return Invalid("chart", "must be a valid OCI chart reference")
		}
		return nil
	}
	if input.RepositoryID == "" || !chartNamePattern.MatchString(chart) ||
		strings.Contains(chart, "..") || strings.HasPrefix(chart, "/") || strings.Contains(chart, `\`) {
		return Invalid("chart", "must be an OCI reference or a safe chart name from a configured repository")
	}
	return nil
}

func ValidateWorkloadReference(reference WorkloadReference) error {
	kind := strings.ToLower(strings.TrimSpace(reference.Kind))
	if kind != "deployment" && kind != "statefulset" && kind != "daemonset" && kind != "pod" {
		return Invalid("kind", "must be deployment, statefulset, daemonset or pod")
	}
	if !validDNSLabel(reference.Namespace) {
		return Invalid("namespace", "must be a valid Kubernetes namespace")
	}
	if !validDNSSubdomain(reference.Name) {
		return Invalid("name", "must be a valid Kubernetes resource name")
	}
	return nil
}

func ValidatePodLogRequest(input PodLogRequest) error {
	if err := ValidateWorkloadReference(WorkloadReference{Kind: "pod", Namespace: input.Namespace, Name: input.Pod}); err != nil {
		return err
	}
	if !validDNSLabel(input.Container) {
		return Invalid("container", "must be a valid Kubernetes container name")
	}
	if input.TailLines < 1 || input.TailLines > MaxPodLogTailLines {
		return Invalid("tail_lines", "must be between 1 and 2000")
	}
	return nil
}

func ValidateNodeName(name string) error {
	if !validDNSSubdomain(name) {
		return Invalid("name", "must be a valid Kubernetes node name")
	}
	return nil
}

func validateHTTPSURL(raw string, allowPath bool) (*url.URL, error) {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, Invalid("url", "invalid HTTPS URL")
	}
	if !allowPath && parsed.Path != "" && parsed.Path != "/" {
		return nil, Invalid("url", "path is not allowed")
	}
	return parsed, nil
}

func validDNSLabel(value string) bool {
	return len(value) > 0 && len(value) <= 63 && dnsLabelPattern.MatchString(value)
}

func validDNSSubdomain(value string) bool {
	if len(value) == 0 || len(value) > 253 || !dnsSubdomainPattern.MatchString(value) {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if !validDNSLabel(label) {
			return false
		}
	}
	return true
}
