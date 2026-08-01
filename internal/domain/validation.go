package domain

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"
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
	return validateClusterCredentials(input.CACert, input.BearerToken)
}

func ValidateClusterCredentialRotationInput(input ClusterCredentialRotationInput) error {
	if err := validateClusterCredentials(input.CACert, input.BearerToken); err != nil {
		return err
	}
	if !resourceNamePattern.MatchString(input.Confirmation) {
		return Invalid("confirmation", "must be a valid cluster name")
	}
	return nil
}

func validateClusterCredentials(caCert, bearerToken string) error {
	if bearerToken == "" || len(bearerToken) > MaxClusterBearerTokenBytes || bearerToken != strings.TrimSpace(bearerToken) ||
		strings.IndexFunc(bearerToken, func(value rune) bool { return unicode.IsSpace(value) || unicode.IsControl(value) }) >= 0 {
		return Invalid("bearer_token", "must be non-empty, no longer than 64 KiB and contain no whitespace or control characters")
	}
	if len(caCert) > MaxClusterCACertBytes {
		return Invalid("ca_cert", "must not exceed 256 KiB")
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

func ValidateHelmReleaseReference(namespace, releaseName string) error {
	if !validDNSLabel(namespace) {
		return Invalid("namespace", "must be a valid Kubernetes namespace")
	}
	if !validDNSLabel(releaseName) {
		return Invalid("release_name", "must be a valid Helm release name")
	}
	return nil
}

func ValidateWorkloadReference(reference WorkloadReference) error {
	kind := strings.ToLower(strings.TrimSpace(reference.Kind))
	if !validWorkloadKind(kind) {
		return Invalid("kind", "must be deployment, statefulset, daemonset, job, cronjob or pod")
	}
	if !validDNSLabel(reference.Namespace) {
		return Invalid("namespace", "must be a valid Kubernetes namespace")
	}
	if !validDNSSubdomain(reference.Name) {
		return Invalid("name", "must be a valid Kubernetes resource name")
	}
	return nil
}

func ValidateDeploymentReference(reference WorkloadReference) error {
	if err := ValidateWorkloadReference(reference); err != nil {
		return err
	}
	if strings.ToLower(strings.TrimSpace(reference.Kind)) != "deployment" {
		return Invalid("kind", "must be deployment")
	}
	return nil
}

func ValidateWorkloadList(namespace, kind string) error {
	if namespace != "" {
		if err := ValidateNamespace(namespace); err != nil {
			return err
		}
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != "" && !validWorkloadKind(kind) {
		return Invalid("kind", "must be deployment, statefulset, daemonset, job, cronjob or pod")
	}
	return nil
}

func validWorkloadKind(kind string) bool {
	switch kind {
	case "deployment", "statefulset", "daemonset", "job", "cronjob", "pod":
		return true
	default:
		return false
	}
}

func ValidateNamespace(namespace string) error {
	if !validDNSLabel(namespace) {
		return Invalid("namespace", "must be a valid Kubernetes namespace")
	}
	return nil
}

func ValidateAccessResourceScope(kind KubernetesAccessResourceKind, namespace string) error {
	switch kind {
	case AccessResourceServiceAccounts, AccessResourceRoles, AccessResourceRoleBindings:
		return ValidateNamespace(namespace)
	case AccessResourceClusterRoles, AccessResourceClusterRoleBindings:
		if namespace != "" {
			return Invalid("namespace", "must be empty for a cluster-scoped access resource")
		}
		return nil
	default:
		return Invalid("kind", "must be serviceaccounts, roles, rolebindings, clusterroles or clusterrolebindings")
	}
}

func ValidateAccessResourceReference(reference KubernetesAccessResourceReference) error {
	if err := ValidateAccessResourceScope(reference.Kind, reference.Namespace); err != nil {
		return err
	}
	validName := validRBACResourceName(reference.Name)
	if reference.Kind == AccessResourceServiceAccounts {
		validName = validDNSSubdomain(reference.Name)
	}
	if !validName {
		return Invalid("name", "must be a valid Kubernetes resource name")
	}
	return nil
}

func ValidateServiceAccountAccessReviewInput(input KubernetesServiceAccountAccessReviewInput) error {
	if !validDNSLabel(input.ServiceAccount.Namespace) {
		return Invalid("service_account.namespace", "must be a valid Kubernetes namespace")
	}
	if !validDNSSubdomain(input.ServiceAccount.Name) {
		return Invalid("service_account.name", "must be a valid Kubernetes resource name")
	}
	attributes := input.ResourceAttributes
	if attributes.Group != "" && !validDNSSubdomain(attributes.Group) {
		return Invalid("resource_attributes.group", "must be empty or a valid Kubernetes API group")
	}
	if !validDNSLabel(attributes.Resource) {
		return Invalid("resource_attributes.resource", "must be a valid Kubernetes API resource")
	}
	if attributes.Subresource != "" && !validDNSLabel(attributes.Subresource) {
		return Invalid("resource_attributes.subresource", "must be empty or a valid Kubernetes subresource")
	}
	if !validAccessReviewVerb(attributes.Verb) {
		return Invalid("resource_attributes.verb", "must be an allowed Kubernetes resource verb")
	}
	if attributes.Namespace != "" && !validDNSLabel(attributes.Namespace) {
		return Invalid("resource_attributes.namespace", "must be empty or a valid Kubernetes namespace")
	}
	if attributes.Name != "" && !validRBACResourceName(attributes.Name) {
		return Invalid("resource_attributes.name", "must be empty or a safe Kubernetes resource name")
	}
	return nil
}

func validAccessReviewVerb(value string) bool {
	switch value {
	case "get", "list", "watch", "create", "update", "patch", "delete", "deletecollection",
		"proxy", "use", "bind", "escalate", "impersonate", "approve", "sign":
		return true
	default:
		return false
	}
}

func validRBACResourceName(value string) bool {
	return value != "" && len(value) <= 253 && value != "." && value != ".." &&
		value == strings.TrimSpace(value) && !strings.ContainsAny(value, "/%") &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

func ValidateKubernetesEventList(namespace, eventType string, limit int) error {
	if namespace != "" {
		if err := ValidateNamespace(namespace); err != nil {
			return err
		}
	}
	if eventType != "" && eventType != "Normal" && eventType != "Warning" {
		return Invalid("type", "must be Normal or Warning")
	}
	if limit < 1 || limit > MaxClusterEventLimit {
		return Invalid("limit", "must be between 1 and 500")
	}
	return nil
}

func ValidateWorkloadOperationInput(kind OperationKind, input WorkloadOperationInput) error {
	if kind != OperationWorkloadScale && kind != OperationWorkloadRestart {
		return Invalid("kind", "must be workload.scale or workload.restart")
	}
	if strings.TrimSpace(input.ClusterID) == "" {
		return Invalid("cluster_id", "is required")
	}
	if err := ValidateWorkloadReference(input.Reference); err != nil {
		return err
	}
	if strings.ToLower(strings.TrimSpace(input.Reference.Kind)) != "deployment" {
		return Invalid("kind", "only Deployment operations are supported")
	}
	resourceVersion := strings.TrimSpace(input.ResourceVersion)
	if resourceVersion == "" || len(resourceVersion) > 256 || strings.IndexFunc(resourceVersion, unicode.IsControl) >= 0 {
		return Invalid("resource_version", "must be a valid value no longer than 256 characters")
	}
	if kind == OperationWorkloadScale {
		if input.Replicas == nil {
			return Invalid("replicas", "is required")
		}
		if *input.Replicas < 0 || *input.Replicas > MaxWorkloadReplicas {
			return Invalid("replicas", "must be between 0 and 1000")
		}
		return nil
	}
	if input.Replicas != nil {
		return Invalid("replicas", "must not be provided for restart")
	}
	return nil
}

func ValidateWorkloadImageOperationInput(input WorkloadImageOperationInput) error {
	if strings.TrimSpace(input.ClusterID) == "" {
		return Invalid("cluster_id", "is required")
	}
	return ValidateWorkloadImageChange(input.Change)
}

func ValidateOperationID(id string) error {
	if !strings.HasPrefix(id, "op_") || !resourceNamePattern.MatchString(id) {
		return Invalid("operation_id", "must be a valid operation identifier")
	}
	return nil
}

func ValidateWorkloadImageChange(change WorkloadImageChange) error {
	if err := ValidateWorkloadReference(change.Reference); err != nil {
		return err
	}
	if strings.ToLower(strings.TrimSpace(change.Reference.Kind)) != "deployment" {
		return Invalid("kind", "only Deployment image updates are supported")
	}
	resourceVersion := strings.TrimSpace(change.ResourceVersion)
	if resourceVersion == "" || len(resourceVersion) > 256 || strings.IndexFunc(resourceVersion, unicode.IsControl) >= 0 {
		return Invalid("resource_version", "must be a valid value no longer than 256 characters")
	}
	if !validDNSLabel(change.Container) {
		return Invalid("container", "must be a valid Kubernetes container name")
	}
	if err := validateContainerImage("current_image", change.CurrentImage); err != nil {
		return err
	}
	if err := validateContainerImage("image", change.Image); err != nil {
		return err
	}
	if change.CurrentImage == change.Image {
		return Invalid("image", "must differ from the current image")
	}
	return nil
}

func validateContainerImage(field, image string) error {
	if image == "" || len(image) > MaxContainerImageBytes || image != strings.TrimSpace(image) ||
		strings.IndexFunc(image, func(value rune) bool { return unicode.IsSpace(value) || unicode.IsControl(value) }) >= 0 {
		return Invalid(field, "must be a non-empty image reference no longer than 1024 bytes without whitespace")
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

func ValidateCustomResourceDefinitionName(name string) error {
	resource, group, found := strings.Cut(name, ".")
	if !found || !validDNSSubdomain(name) || !validDNSLabel(resource) || !validDNSSubdomain(group) {
		return Invalid("name", "must be a valid Kubernetes CustomResourceDefinition name")
	}
	return nil
}

func ValidateCertificateSigningRequestName(name string) error {
	if !validDNSSubdomain(name) {
		return Invalid("name", "must be a valid Kubernetes CertificateSigningRequest name")
	}
	return nil
}

func ValidatePriorityClassName(name string) error {
	if !validDNSSubdomain(name) {
		return Invalid("name", "must be a valid Kubernetes PriorityClass name")
	}
	return nil
}

func ValidateRuntimeClassName(name string) error {
	if !validDNSSubdomain(name) {
		return Invalid("name", "must be a valid Kubernetes RuntimeClass name")
	}
	return nil
}

func ValidateRuntimeClassHandler(handler string) error {
	if !validDNSLabel(handler) {
		return Invalid("handler", "must be a valid Kubernetes RuntimeClass handler")
	}
	return nil
}

func ValidateCSIDriverName(name string) error {
	if !validDNSSubdomain(name) {
		return Invalid("name", "must be a valid Kubernetes CSIDriver name")
	}
	return nil
}

func ValidateVolumeAttachmentName(name string) error {
	if !validDNSSubdomain(name) {
		return Invalid("name", "must be a valid Kubernetes VolumeAttachment name")
	}
	return nil
}

func ValidatePersistentVolumeName(name string) error {
	if !validDNSSubdomain(name) {
		return Invalid("name", "must be a valid Kubernetes PersistentVolume name")
	}
	return nil
}

func ValidateStorageClassName(name string) error {
	if !validDNSSubdomain(name) {
		return Invalid("name", "must be a valid Kubernetes StorageClass name")
	}
	return nil
}

func ValidateCSIStorageCapacityName(name string) error {
	if !validDNSSubdomain(name) {
		return Invalid("name", "must be a valid Kubernetes CSIStorageCapacity name")
	}
	return nil
}

func ValidateVolumeAttributesClassName(name string) error {
	if !validDNSSubdomain(name) {
		return Invalid("name", "must be a valid Kubernetes VolumeAttributesClass name")
	}
	return nil
}

func ValidateAPIServiceName(name string) error {
	version, group, found := strings.Cut(name, ".")
	if !found || len(name) > 253 || !validDNSLabel(version) || (group != "" && !validDNSSubdomain(group)) {
		return Invalid("name", "must be a valid Kubernetes APIService name")
	}
	return nil
}

func ValidateAdmissionWebhookConfigurationKind(kind KubernetesAdmissionWebhookConfigurationKind) error {
	if kind != AdmissionWebhookConfigurationValidating && kind != AdmissionWebhookConfigurationMutating {
		return Invalid("kind", "must be validating or mutating")
	}
	return nil
}

func ValidateAdmissionWebhookConfigurationName(name string) error {
	if !validDNSSubdomain(name) {
		return Invalid("name", "must be a valid Kubernetes admission webhook configuration name")
	}
	return nil
}

func ValidateAdmissionPolicyResourceName(name string) error {
	if !validDNSSubdomain(name) {
		return Invalid("name", "must be a valid Kubernetes admission policy resource name")
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
