package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/auth"
	"github.com/caoyanyi/k8s-panel/internal/domain"
	"github.com/caoyanyi/k8s-panel/internal/kubernetes"
	"github.com/caoyanyi/k8s-panel/internal/platform"
	"github.com/caoyanyi/k8s-panel/internal/resourceguard"
	"github.com/caoyanyi/k8s-panel/internal/secure"
	"github.com/caoyanyi/k8s-panel/internal/store"
)

func TestServerAuthenticationAndClusterLifecycle(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want 401", unauthorized.Code)
	}
	assertErrorCode(t, unauthorized.Body.Bytes(), "unauthorized")
	if unauthorized.Header().Get("X-Request-ID") == "" {
		t.Error("unauthorized response has no request ID")
	}
	unauthorizedNetwork := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedNetwork, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/clu_1/services", nil))
	if unauthorizedNetwork.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized network status = %d, want 401", unauthorizedNetwork.Code)
	}
	unauthorizedEndpointSlice := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedEndpointSlice, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/clu_1/endpoint-slices", nil))
	if unauthorizedEndpointSlice.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized endpoint slice status = %d, want 401", unauthorizedEndpointSlice.Code)
	}
	unauthorizedNetworkPolicy := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedNetworkPolicy, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/clu_1/network-policies", nil))
	if unauthorizedNetworkPolicy.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized network policy status = %d, want 401", unauthorizedNetworkPolicy.Code)
	}
	unauthorizedCRDs := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedCRDs, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/clu_1/custom-resource-definitions", nil))
	if unauthorizedCRDs.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized CRD status = %d, want 401", unauthorizedCRDs.Code)
	}
	unauthorizedCSRs := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedCSRs, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/clu_1/certificate-signing-requests", nil))
	if unauthorizedCSRs.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized CSR status = %d, want 401", unauthorizedCSRs.Code)
	}
	unauthorizedPriorityClasses := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedPriorityClasses, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/clu_1/priority-classes", nil))
	if unauthorizedPriorityClasses.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized PriorityClass status = %d, want 401", unauthorizedPriorityClasses.Code)
	}
	unauthorizedRuntimeClasses := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedRuntimeClasses, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/clu_1/runtime-classes", nil))
	if unauthorizedRuntimeClasses.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized RuntimeClass status = %d, want 401", unauthorizedRuntimeClasses.Code)
	}
	unauthorizedAPIServices := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedAPIServices, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/clu_1/api-services", nil))
	if unauthorizedAPIServices.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized APIService status = %d, want 401", unauthorizedAPIServices.Code)
	}
	unauthorizedAdmissionWebhooks := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedAdmissionWebhooks, httptest.NewRequest(
		http.MethodGet, "/api/v1/clusters/clu_1/admission-webhook-configurations?kind=validating", nil,
	))
	if unauthorizedAdmissionWebhooks.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized admission webhook status = %d, want 401", unauthorizedAdmissionWebhooks.Code)
	}
	unauthorizedAdmissionPolicies := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedAdmissionPolicies, httptest.NewRequest(
		http.MethodGet, "/api/v1/clusters/clu_1/validating-admission-policies", nil,
	))
	if unauthorizedAdmissionPolicies.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized admission policy status = %d, want 401", unauthorizedAdmissionPolicies.Code)
	}
	unauthorizedAdmissionPolicyBindings := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedAdmissionPolicyBindings, httptest.NewRequest(
		http.MethodGet, "/api/v1/clusters/clu_1/validating-admission-policy-bindings", nil,
	))
	if unauthorizedAdmissionPolicyBindings.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized admission policy binding status = %d, want 401", unauthorizedAdmissionPolicyBindings.Code)
	}
	unauthorizedPodSecurityAdmission := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedPodSecurityAdmission, httptest.NewRequest(
		http.MethodGet, "/api/v1/clusters/clu_1/pod-security-admission/namespaces", nil,
	))
	if unauthorizedPodSecurityAdmission.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized pod security admission status = %d, want 401", unauthorizedPodSecurityAdmission.Code)
	}
	unauthorizedNodeVersionSkew := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedNodeVersionSkew, httptest.NewRequest(
		http.MethodGet, "/api/v1/clusters/clu_1/upgrade-readiness/node-versions", nil,
	))
	if unauthorizedNodeVersionSkew.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized node version skew status = %d, want 401", unauthorizedNodeVersionSkew.Code)
	}
	unauthorizedDeprecatedAPIs := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedDeprecatedAPIs, httptest.NewRequest(
		http.MethodGet, "/api/v1/clusters/clu_1/upgrade-readiness/deprecated-apis", nil,
	))
	if unauthorizedDeprecatedAPIs.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized deprecated API status = %d, want 401", unauthorizedDeprecatedAPIs.Code)
	}
	unauthorizedEndpointCertificate := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedEndpointCertificate, httptest.NewRequest(
		http.MethodGet, "/api/v1/clusters/clu_1/upgrade-readiness/endpoint-certificate", nil,
	))
	if unauthorizedEndpointCertificate.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized endpoint certificate status = %d, want 401", unauthorizedEndpointCertificate.Code)
	}
	unauthorizedAPIServerReadiness := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedAPIServerReadiness, httptest.NewRequest(
		http.MethodGet, "/api/v1/clusters/clu_1/control-plane/readiness", nil,
	))
	if unauthorizedAPIServerReadiness.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized API Server readiness status = %d, want 401", unauthorizedAPIServerReadiness.Code)
	}
	unauthorizedDisruptionBudgets := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedDisruptionBudgets, httptest.NewRequest(
		http.MethodGet, "/api/v1/clusters/clu_1/upgrade-readiness/disruption-budgets", nil,
	))
	if unauthorizedDisruptionBudgets.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized disruption budgets status = %d, want 401", unauthorizedDisruptionBudgets.Code)
	}
	unauthorizedConfiguration := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedConfiguration, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/clu_1/configmaps", nil))
	if unauthorizedConfiguration.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized configuration status = %d, want 401", unauthorizedConfiguration.Code)
	}
	unauthorizedStorage := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedStorage, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/clu_1/persistent-volumes", nil))
	if unauthorizedStorage.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized storage status = %d, want 401", unauthorizedStorage.Code)
	}
	unauthorizedVolumeAttachments := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedVolumeAttachments, httptest.NewRequest(
		http.MethodGet, "/api/v1/clusters/clu_1/volume-attachments", nil,
	))
	if unauthorizedVolumeAttachments.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized volume attachments status = %d, want 401", unauthorizedVolumeAttachments.Code)
	}
	unauthorizedCSIStorageCapacities := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedCSIStorageCapacities, httptest.NewRequest(
		http.MethodGet, "/api/v1/clusters/clu_1/csi-storage-capacities", nil,
	))
	if unauthorizedCSIStorageCapacities.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized CSI storage capacities status = %d, want 401", unauthorizedCSIStorageCapacities.Code)
	}
	unauthorizedCSIDrivers := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedCSIDrivers, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/clu_1/csi-drivers", nil))
	if unauthorizedCSIDrivers.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized CSIDriver status = %d, want 401", unauthorizedCSIDrivers.Code)
	}
	unauthorizedCSINodes := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedCSINodes, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/clu_1/csi-nodes", nil))
	if unauthorizedCSINodes.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized CSINode status = %d, want 401", unauthorizedCSINodes.Code)
	}
	unauthorizedEvents := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedEvents, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/clu_1/events", nil))
	if unauthorizedEvents.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized events status = %d, want 401", unauthorizedEvents.Code)
	}
	unauthorizedAccess := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedAccess, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/clu_1/access-resources?kind=clusterroles", nil))
	if unauthorizedAccess.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized access resources status = %d, want 401", unauthorizedAccess.Code)
	}
	unauthorizedAccessReview := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedAccessReview, httptest.NewRequest(
		http.MethodPost, "/api/v1/clusters/clu_1/service-account-access-reviews", strings.NewReader(`{}`),
	))
	if unauthorizedAccessReview.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized service account access review status = %d, want 401", unauthorizedAccessReview.Code)
	}

	cookie := login(t, handler)
	createBody := `{
		"name":"production-east",
		"environment":"production",
		"server":"https://api.example.com:6443",
		"ca_cert":"test-ca",
		"bearer_token":"plain-service-account-token"
	}`
	create := authenticatedRequest(t, handler, cookie, http.MethodPost, "/api/v1/clusters", createBody)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", create.Code, create.Body.String())
	}
	if create.Header().Get("Location") == "" {
		t.Error("create response has no Location header")
	}
	if strings.Contains(create.Body.String(), "plain-service-account-token") || strings.Contains(create.Body.String(), "test-ca") {
		t.Fatal("create response leaked cluster credentials")
	}
	var created struct {
		Data platform.ClusterView `json:"data"`
	}
	decodeTestJSON(t, create.Body.Bytes(), &created)
	if created.Data.Status != domain.ClusterConnected || !created.Data.CredentialsConfigured {
		t.Errorf("created cluster = %#v", created.Data)
	}
	rotationPath := "/api/v1/clusters/" + created.Data.ID + "/credential-rotations"
	unconfirmedRotation := authenticatedRequest(t, handler, cookie, http.MethodPost, rotationPath, `{
		"ca_cert":"new-test-ca","bearer_token":"new-service-account-token","confirmation":"wrong-cluster"
	}`)
	if unconfirmedRotation.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unconfirmed rotation status = %d, body = %s", unconfirmedRotation.Code, unconfirmedRotation.Body.String())
	}
	assertErrorField(t, unconfirmedRotation.Body.Bytes(), "confirmation")
	malformedRotation := authenticatedRequest(t, handler, cookie, http.MethodPost, rotationPath, `{"bearer_token":`)
	if malformedRotation.Code != http.StatusBadRequest {
		t.Fatalf("malformed rotation status = %d, body = %s", malformedRotation.Code, malformedRotation.Body.String())
	}
	assertErrorCode(t, malformedRotation.Body.Bytes(), "invalid_json")
	rotation := authenticatedRequest(t, handler, cookie, http.MethodPost, rotationPath, `{
		"ca_cert":"new-test-ca","bearer_token":"new-service-account-token","confirmation":"production-east"
	}`)
	if rotation.Code != http.StatusOK {
		t.Fatalf("rotation status = %d, body = %s", rotation.Code, rotation.Body.String())
	}
	if strings.Contains(rotation.Body.String(), "new-service-account-token") || strings.Contains(rotation.Body.String(), "new-test-ca") {
		t.Fatal("rotation response leaked candidate credentials")
	}
	capabilitiesPath := "/api/v1/clusters/" + created.Data.ID + "/capabilities"
	missingCapabilityNamespace := authenticatedRequest(t, handler, cookie, http.MethodGet, capabilitiesPath, "")
	if missingCapabilityNamespace.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing capability namespace status = %d, body = %s", missingCapabilityNamespace.Code, missingCapabilityNamespace.Body.String())
	}
	assertErrorField(t, missingCapabilityNamespace.Body.Bytes(), "namespace")
	capabilities := authenticatedRequest(t, handler, cookie, http.MethodGet, capabilitiesPath+"?namespace=payments", "")
	if capabilities.Code != http.StatusOK || !strings.Contains(capabilities.Body.String(), `"namespace":"payments"`) ||
		!strings.Contains(capabilities.Body.String(), `"key":"namespaces.list"`) || strings.Contains(capabilities.Body.String(), "internal role") {
		t.Fatalf("capabilities status = %d, body = %s", capabilities.Code, capabilities.Body.String())
	}
	servicesPath := "/api/v1/clusters/" + created.Data.ID + "/services"
	services := authenticatedRequest(t, handler, cookie, http.MethodGet, servicesPath+"?namespace=payments", "")
	if services.Code != http.StatusOK || !strings.Contains(services.Body.String(), `"name":"gateway"`) ||
		!strings.Contains(services.Body.String(), `"type":"ClusterIP"`) {
		t.Fatalf("services status = %d, body = %s", services.Code, services.Body.String())
	}
	ingresses := authenticatedRequest(t, handler, cookie, http.MethodGet, "/api/v1/clusters/"+created.Data.ID+"/ingresses", "")
	if ingresses.Code != http.StatusOK || !strings.Contains(ingresses.Body.String(), `"class_name":"nginx"`) ||
		!strings.Contains(ingresses.Body.String(), `"host_count":1`) {
		t.Fatalf("ingresses status = %d, body = %s", ingresses.Code, ingresses.Body.String())
	}
	endpointSlicesPath := "/api/v1/clusters/" + created.Data.ID + "/endpoint-slices"
	endpointSlices := authenticatedRequest(t, handler, cookie, http.MethodGet, endpointSlicesPath+"?namespace=payments", "")
	if endpointSlices.Code != http.StatusOK || !strings.Contains(endpointSlices.Body.String(), `"name":"gateway-ipv4"`) ||
		!strings.Contains(endpointSlices.Body.String(), `"service_name":"gateway"`) ||
		!strings.Contains(endpointSlices.Body.String(), `"ready_endpoint_count":2`) ||
		strings.Contains(endpointSlices.Body.String(), "10.42.0.10") {
		t.Fatalf("endpoint slices status = %d, body = %s", endpointSlices.Code, endpointSlices.Body.String())
	}
	crdPath := "/api/v1/clusters/" + created.Data.ID + "/custom-resource-definitions"
	crds := authenticatedRequest(t, handler, cookie, http.MethodGet, crdPath, "")
	if crds.Code != http.StatusOK || !strings.Contains(crds.Body.String(), `"name":"widgets.platform.example.com"`) ||
		!strings.Contains(crds.Body.String(), `"resource":"widgets"`) ||
		!strings.Contains(crds.Body.String(), `"group":"platform.example.com"`) {
		t.Fatalf("CRDs status = %d, body = %s", crds.Code, crds.Body.String())
	}
	crdDetail := authenticatedRequest(t, handler, cookie, http.MethodGet, crdPath+"/widgets.platform.example.com", "")
	if crdDetail.Code != http.StatusOK || !strings.Contains(crdDetail.Body.String(), `"scope":"Namespaced"`) ||
		!strings.Contains(crdDetail.Body.String(), `"conversion_strategy":"None"`) ||
		!strings.Contains(crdDetail.Body.String(), `"version_count":1`) ||
		strings.Contains(crdDetail.Body.String(), "private-schema-field") {
		t.Fatalf("CRD detail status = %d, body = %s", crdDetail.Code, crdDetail.Body.String())
	}
	invalidCRDName := authenticatedRequest(t, handler, cookie, http.MethodGet, crdPath+"/..%2Fcustomresourcedefinitions", "")
	if invalidCRDName.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid CRD name status = %d, body = %s", invalidCRDName.Code, invalidCRDName.Body.String())
	}
	assertErrorField(t, invalidCRDName.Body.Bytes(), "name")
	csrPath := "/api/v1/clusters/" + created.Data.ID + "/certificate-signing-requests"
	csrs := authenticatedRequest(t, handler, cookie, http.MethodGet, csrPath, "")
	if csrs.Code != http.StatusOK || !strings.Contains(csrs.Body.String(), `"name":"worker-01"`) ||
		strings.Contains(csrs.Body.String(), "private-pkcs10") {
		t.Fatalf("CSRs status = %d, body = %s", csrs.Code, csrs.Body.String())
	}
	csrDetail := authenticatedRequest(t, handler, cookie, http.MethodGet, csrPath+"/worker-01", "")
	if csrDetail.Code != http.StatusOK ||
		!strings.Contains(csrDetail.Body.String(), `"requester":"system:node:worker-01"`) ||
		!strings.Contains(csrDetail.Body.String(), `"signer_name":"example.com/node-client"`) ||
		!strings.Contains(csrDetail.Body.String(), `"state":"approved"`) ||
		strings.Contains(csrDetail.Body.String(), "private-pkcs10") ||
		strings.Contains(csrDetail.Body.String(), "private-certificate") {
		t.Fatalf("CSR detail status = %d, body = %s", csrDetail.Code, csrDetail.Body.String())
	}
	invalidCSRName := authenticatedRequest(t, handler, cookie, http.MethodGet, csrPath+"/..%2Fcertificatesigningrequests", "")
	if invalidCSRName.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid CSR name status = %d, body = %s", invalidCSRName.Code, invalidCSRName.Body.String())
	}
	assertErrorField(t, invalidCSRName.Body.Bytes(), "name")
	priorityClassPath := "/api/v1/clusters/" + created.Data.ID + "/priority-classes"
	priorityClasses := authenticatedRequest(t, handler, cookie, http.MethodGet, priorityClassPath, "")
	if priorityClasses.Code != http.StatusOK || !strings.Contains(priorityClasses.Body.String(), `"name":"workload-high"`) ||
		strings.Contains(priorityClasses.Body.String(), "private scheduling guidance") {
		t.Fatalf("PriorityClasses status = %d, body = %s", priorityClasses.Code, priorityClasses.Body.String())
	}
	priorityClassDetail := authenticatedRequest(t, handler, cookie, http.MethodGet, priorityClassPath+"/workload-high", "")
	if priorityClassDetail.Code != http.StatusOK ||
		!strings.Contains(priorityClassDetail.Body.String(), `"value":1000000`) ||
		!strings.Contains(priorityClassDetail.Body.String(), `"preemption_policy":"PreemptLowerPriority"`) ||
		strings.Contains(priorityClassDetail.Body.String(), "private scheduling guidance") ||
		strings.Contains(priorityClassDetail.Body.String(), "description") {
		t.Fatalf("PriorityClass detail status = %d, body = %s", priorityClassDetail.Code, priorityClassDetail.Body.String())
	}
	invalidPriorityClassName := authenticatedRequest(t, handler, cookie, http.MethodGet, priorityClassPath+"/..%2Fpriorityclasses", "")
	if invalidPriorityClassName.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid PriorityClass name status = %d, body = %s", invalidPriorityClassName.Code, invalidPriorityClassName.Body.String())
	}
	assertErrorField(t, invalidPriorityClassName.Body.Bytes(), "name")
	runtimeClassPath := "/api/v1/clusters/" + created.Data.ID + "/runtime-classes"
	runtimeClasses := authenticatedRequest(t, handler, cookie, http.MethodGet, runtimeClassPath, "")
	if runtimeClasses.Code != http.StatusOK || !strings.Contains(runtimeClasses.Body.String(), `"name":"kata-containers"`) ||
		strings.Contains(runtimeClasses.Body.String(), "private-runtime-selector") {
		t.Fatalf("RuntimeClasses status = %d, body = %s", runtimeClasses.Code, runtimeClasses.Body.String())
	}
	missingRuntimeClasses := authenticatedRequest(
		t, handler, cookie, http.MethodGet, "/api/v1/clusters/clu_missing/runtime-classes", "",
	)
	if missingRuntimeClasses.Code != http.StatusNotFound {
		t.Fatalf("missing cluster RuntimeClasses status = %d, body = %s", missingRuntimeClasses.Code, missingRuntimeClasses.Body.String())
	}
	runtimeClassDetail := authenticatedRequest(t, handler, cookie, http.MethodGet, runtimeClassPath+"/kata-containers", "")
	if runtimeClassDetail.Code != http.StatusOK ||
		!strings.Contains(runtimeClassDetail.Body.String(), `"handler":"kata-fc"`) ||
		!strings.Contains(runtimeClassDetail.Body.String(), `"pod_overhead_cpu":"250m"`) ||
		!strings.Contains(runtimeClassDetail.Body.String(), `"node_selector_count":2`) ||
		strings.Contains(runtimeClassDetail.Body.String(), "private-runtime-selector") ||
		strings.Contains(runtimeClassDetail.Body.String(), `"nodeSelector"`) ||
		strings.Contains(runtimeClassDetail.Body.String(), `"tolerations"`) {
		t.Fatalf("RuntimeClass detail status = %d, body = %s", runtimeClassDetail.Code, runtimeClassDetail.Body.String())
	}
	invalidRuntimeClassName := authenticatedRequest(t, handler, cookie, http.MethodGet, runtimeClassPath+"/..%2Fruntimeclasses", "")
	if invalidRuntimeClassName.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid RuntimeClass name status = %d, body = %s", invalidRuntimeClassName.Code, invalidRuntimeClassName.Body.String())
	}
	assertErrorField(t, invalidRuntimeClassName.Body.Bytes(), "name")
	apiServicesPath := "/api/v1/clusters/" + created.Data.ID + "/api-services"
	apiServices := authenticatedRequest(t, handler, cookie, http.MethodGet, apiServicesPath, "")
	if apiServices.Code != http.StatusOK || !strings.Contains(apiServices.Body.String(), `"name":"v1beta1.metrics.k8s.io"`) ||
		!strings.Contains(apiServices.Body.String(), `"availability_status":"False"`) ||
		!strings.Contains(apiServices.Body.String(), `"service_name":"metrics-server"`) ||
		strings.Contains(apiServices.Body.String(), "private availability message") {
		t.Fatalf("API services status = %d, body = %s", apiServices.Code, apiServices.Body.String())
	}
	admissionWebhooksPath := "/api/v1/clusters/" + created.Data.ID + "/admission-webhook-configurations"
	admissionWebhooks := authenticatedRequest(t, handler, cookie, http.MethodGet, admissionWebhooksPath+"?kind=validating", "")
	if admissionWebhooks.Code != http.StatusOK ||
		!strings.Contains(admissionWebhooks.Body.String(), `"name":"policy.platform.example.com"`) ||
		!strings.Contains(admissionWebhooks.Body.String(), `"kind":"validating"`) {
		t.Fatalf("admission webhooks status = %d, body = %s", admissionWebhooks.Code, admissionWebhooks.Body.String())
	}
	admissionWebhookDetail := authenticatedRequest(
		t, handler, cookie, http.MethodGet,
		admissionWebhooksPath+"/policy.platform.example.com?kind=validating", "",
	)
	if admissionWebhookDetail.Code != http.StatusOK ||
		!strings.Contains(admissionWebhookDetail.Body.String(), `"target_type":"service"`) ||
		!strings.Contains(admissionWebhookDetail.Body.String(), `"failure_policy":"Fail"`) ||
		!strings.Contains(admissionWebhookDetail.Body.String(), `"webhook_count":1`) ||
		strings.Contains(admissionWebhookDetail.Body.String(), "private-webhook-path") ||
		strings.Contains(admissionWebhookDetail.Body.String(), "private-ca-bundle") {
		t.Fatalf("admission webhook detail status = %d, body = %s", admissionWebhookDetail.Code, admissionWebhookDetail.Body.String())
	}
	invalidAdmissionWebhookKind := authenticatedRequest(t, handler, cookie, http.MethodGet, admissionWebhooksPath+"?kind=Validating", "")
	if invalidAdmissionWebhookKind.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid admission webhook kind status = %d, body = %s", invalidAdmissionWebhookKind.Code, invalidAdmissionWebhookKind.Body.String())
	}
	assertErrorField(t, invalidAdmissionWebhookKind.Body.Bytes(), "kind")
	invalidAdmissionWebhookName := authenticatedRequest(
		t, handler, cookie, http.MethodGet,
		admissionWebhooksPath+"/..%2Fvalidatingwebhookconfigurations?kind=validating", "",
	)
	if invalidAdmissionWebhookName.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid admission webhook name status = %d, body = %s", invalidAdmissionWebhookName.Code, invalidAdmissionWebhookName.Body.String())
	}
	assertErrorField(t, invalidAdmissionWebhookName.Body.Bytes(), "name")
	admissionPoliciesPath := "/api/v1/clusters/" + created.Data.ID + "/validating-admission-policies"
	admissionPolicies := authenticatedRequest(t, handler, cookie, http.MethodGet, admissionPoliciesPath, "")
	if admissionPolicies.Code != http.StatusOK ||
		!strings.Contains(admissionPolicies.Body.String(), `"name":"replica-policy.example.com"`) ||
		!strings.Contains(admissionPolicies.Body.String(), `"kind":"policy"`) {
		t.Fatalf("admission policies status = %d, body = %s", admissionPolicies.Code, admissionPolicies.Body.String())
	}
	admissionPolicyDetail := authenticatedRequest(
		t, handler, cookie, http.MethodGet, admissionPoliciesPath+"/replica-policy.example.com", "",
	)
	if admissionPolicyDetail.Code != http.StatusOK ||
		!strings.Contains(admissionPolicyDetail.Body.String(), `"failure_policy":"Fail"`) ||
		!strings.Contains(admissionPolicyDetail.Body.String(), `"validation_count":2`) ||
		strings.Contains(admissionPolicyDetail.Body.String(), "private CEL expression") ||
		strings.Contains(admissionPolicyDetail.Body.String(), "private warning") {
		t.Fatalf("admission policy detail status = %d, body = %s", admissionPolicyDetail.Code, admissionPolicyDetail.Body.String())
	}
	invalidAdmissionPolicyName := authenticatedRequest(
		t, handler, cookie, http.MethodGet, admissionPoliciesPath+"/..%2Fvalidatingadmissionpolicies", "",
	)
	if invalidAdmissionPolicyName.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid admission policy name status = %d, body = %s", invalidAdmissionPolicyName.Code, invalidAdmissionPolicyName.Body.String())
	}
	assertErrorField(t, invalidAdmissionPolicyName.Body.Bytes(), "name")
	admissionPolicyBindingsPath := "/api/v1/clusters/" + created.Data.ID + "/validating-admission-policy-bindings"
	admissionPolicyBindings := authenticatedRequest(t, handler, cookie, http.MethodGet, admissionPolicyBindingsPath, "")
	if admissionPolicyBindings.Code != http.StatusOK ||
		!strings.Contains(admissionPolicyBindings.Body.String(), `"name":"replica-binding.example.com"`) ||
		!strings.Contains(admissionPolicyBindings.Body.String(), `"kind":"binding"`) {
		t.Fatalf("admission policy bindings status = %d, body = %s", admissionPolicyBindings.Code, admissionPolicyBindings.Body.String())
	}
	admissionPolicyBindingDetail := authenticatedRequest(
		t, handler, cookie, http.MethodGet, admissionPolicyBindingsPath+"/replica-binding.example.com", "",
	)
	if admissionPolicyBindingDetail.Code != http.StatusOK ||
		!strings.Contains(admissionPolicyBindingDetail.Body.String(), `"policy_name":"replica-policy.example.com"`) ||
		!strings.Contains(admissionPolicyBindingDetail.Body.String(), `"validation_actions":["Deny","Audit"]`) ||
		strings.Contains(admissionPolicyBindingDetail.Body.String(), "private-param-name") ||
		strings.Contains(admissionPolicyBindingDetail.Body.String(), "private-selector") {
		t.Fatalf("admission policy binding detail status = %d, body = %s", admissionPolicyBindingDetail.Code, admissionPolicyBindingDetail.Body.String())
	}
	invalidAdmissionPolicyBindingName := authenticatedRequest(
		t, handler, cookie, http.MethodGet, admissionPolicyBindingsPath+"/..%2Fvalidatingadmissionpolicybindings", "",
	)
	if invalidAdmissionPolicyBindingName.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid admission policy binding name status = %d, body = %s", invalidAdmissionPolicyBindingName.Code, invalidAdmissionPolicyBindingName.Body.String())
	}
	assertErrorField(t, invalidAdmissionPolicyBindingName.Body.Bytes(), "name")
	networkPoliciesPath := "/api/v1/clusters/" + created.Data.ID + "/network-policies"
	networkPolicies := authenticatedRequest(t, handler, cookie, http.MethodGet, networkPoliciesPath+"?namespace=payments", "")
	if networkPolicies.Code != http.StatusOK || !strings.Contains(networkPolicies.Body.String(), `"name":"gateway-policy"`) ||
		!strings.Contains(networkPolicies.Body.String(), `"policy_types":["Ingress","Egress"]`) ||
		strings.Contains(networkPolicies.Body.String(), "private-selector-value") {
		t.Fatalf("network policies status = %d, body = %s", networkPolicies.Code, networkPolicies.Body.String())
	}
	invalidNetworkNamespace := authenticatedRequest(t, handler, cookie, http.MethodGet, servicesPath+"?namespace=bad%2Fnamespace", "")
	if invalidNetworkNamespace.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid network namespace status = %d, body = %s", invalidNetworkNamespace.Code, invalidNetworkNamespace.Body.String())
	}
	assertErrorField(t, invalidNetworkNamespace.Body.Bytes(), "namespace")
	invalidEndpointSliceNamespace := authenticatedRequest(t, handler, cookie, http.MethodGet, endpointSlicesPath+"?namespace=bad%2Fnamespace", "")
	if invalidEndpointSliceNamespace.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid endpoint slice namespace status = %d, body = %s", invalidEndpointSliceNamespace.Code, invalidEndpointSliceNamespace.Body.String())
	}
	assertErrorField(t, invalidEndpointSliceNamespace.Body.Bytes(), "namespace")
	invalidNetworkPolicyNamespace := authenticatedRequest(t, handler, cookie, http.MethodGet, networkPoliciesPath+"?namespace=bad%2Fnamespace", "")
	if invalidNetworkPolicyNamespace.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid network policy namespace status = %d, body = %s", invalidNetworkPolicyNamespace.Code, invalidNetworkPolicyNamespace.Body.String())
	}
	assertErrorField(t, invalidNetworkPolicyNamespace.Body.Bytes(), "namespace")
	configMapsPath := "/api/v1/clusters/" + created.Data.ID + "/configmaps"
	configMaps := authenticatedRequest(t, handler, cookie, http.MethodGet, configMapsPath, "")
	if configMaps.Code != http.StatusOK || !strings.Contains(configMaps.Body.String(), `"name":"settings"`) ||
		!strings.Contains(configMaps.Body.String(), `"data_count":3`) {
		t.Fatalf("configmaps status = %d, body = %s", configMaps.Code, configMaps.Body.String())
	}
	invalidConfigMapNamespace := authenticatedRequest(t, handler, cookie, http.MethodGet, configMapsPath+"?namespace=bad%2Fnamespace", "")
	if invalidConfigMapNamespace.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid configmap namespace status = %d, body = %s", invalidConfigMapNamespace.Code, invalidConfigMapNamespace.Body.String())
	}
	assertErrorField(t, invalidConfigMapNamespace.Body.Bytes(), "namespace")
	secretsPath := "/api/v1/clusters/" + created.Data.ID + "/secrets"
	missingSecretNamespace := authenticatedRequest(t, handler, cookie, http.MethodGet, secretsPath, "")
	if missingSecretNamespace.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing secret namespace status = %d, body = %s", missingSecretNamespace.Code, missingSecretNamespace.Body.String())
	}
	assertErrorField(t, missingSecretNamespace.Body.Bytes(), "namespace")
	secrets := authenticatedRequest(t, handler, cookie, http.MethodGet, secretsPath+"?namespace=payments", "")
	if secrets.Code != http.StatusOK || !strings.Contains(secrets.Body.String(), `"name":"registry"`) ||
		!strings.Contains(secrets.Body.String(), `"type":"kubernetes.io/dockerconfigjson"`) ||
		strings.Contains(secrets.Body.String(), "registry-password") {
		t.Fatalf("secrets status = %d, body = %s", secrets.Code, secrets.Body.String())
	}
	claimsPath := "/api/v1/clusters/" + created.Data.ID + "/persistent-volume-claims"
	claims := authenticatedRequest(t, handler, cookie, http.MethodGet, claimsPath+"?namespace=payments", "")
	if claims.Code != http.StatusOK || !strings.Contains(claims.Body.String(), `"name":"data"`) ||
		!strings.Contains(claims.Body.String(), `"volume":"pv-data"`) {
		t.Fatalf("persistent volume claims status = %d, body = %s", claims.Code, claims.Body.String())
	}
	invalidClaimNamespace := authenticatedRequest(t, handler, cookie, http.MethodGet, claimsPath+"?namespace=bad%2Fnamespace", "")
	if invalidClaimNamespace.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid claim namespace status = %d, body = %s", invalidClaimNamespace.Code, invalidClaimNamespace.Body.String())
	}
	assertErrorField(t, invalidClaimNamespace.Body.Bytes(), "namespace")
	volumes := authenticatedRequest(t, handler, cookie, http.MethodGet, "/api/v1/clusters/"+created.Data.ID+"/persistent-volumes", "")
	if volumes.Code != http.StatusOK || !strings.Contains(volumes.Body.String(), `"name":"pv-data"`) ||
		!strings.Contains(volumes.Body.String(), `"reclaim_policy":"Delete"`) || strings.Contains(volumes.Body.String(), "volume-handle") {
		t.Fatalf("persistent volumes status = %d, body = %s", volumes.Code, volumes.Body.String())
	}
	classes := authenticatedRequest(t, handler, cookie, http.MethodGet, "/api/v1/clusters/"+created.Data.ID+"/storage-classes", "")
	if classes.Code != http.StatusOK || !strings.Contains(classes.Body.String(), `"name":"standard"`) ||
		!strings.Contains(classes.Body.String(), `"default":true`) || strings.Contains(classes.Body.String(), "storage-account") {
		t.Fatalf("storage classes status = %d, body = %s", classes.Code, classes.Body.String())
	}
	volumeAttachments := authenticatedRequest(
		t, handler, cookie, http.MethodGet, "/api/v1/clusters/"+created.Data.ID+"/volume-attachments", "",
	)
	if volumeAttachments.Code != http.StatusOK ||
		!strings.Contains(volumeAttachments.Body.String(), `"name":"attach-data"`) ||
		!strings.Contains(volumeAttachments.Body.String(), `"persistent_volume":"pv-data"`) ||
		!strings.Contains(volumeAttachments.Body.String(), `"status":"attached"`) ||
		strings.Contains(volumeAttachments.Body.String(), "private-attach-error") ||
		strings.Contains(volumeAttachments.Body.String(), "attachmentMetadata") {
		t.Fatalf("volume attachments status = %d, body = %s", volumeAttachments.Code, volumeAttachments.Body.String())
	}
	missingVolumeAttachmentCluster := authenticatedRequest(
		t, handler, cookie, http.MethodGet, "/api/v1/clusters/clu_missing/volume-attachments", "",
	)
	if missingVolumeAttachmentCluster.Code != http.StatusNotFound {
		t.Fatalf("missing cluster volume attachments status = %d, body = %s",
			missingVolumeAttachmentCluster.Code, missingVolumeAttachmentCluster.Body.String())
	}
	csiStorageCapacityPath := "/api/v1/clusters/" + created.Data.ID + "/csi-storage-capacities"
	csiStorageCapacities := authenticatedRequest(
		t, handler, cookie, http.MethodGet, csiStorageCapacityPath+"?namespace=storage-system", "",
	)
	if csiStorageCapacities.Code != http.StatusOK ||
		!strings.Contains(csiStorageCapacities.Body.String(), `"namespace":"storage-system"`) ||
		!strings.Contains(csiStorageCapacities.Body.String(), `"name":"capacity-a"`) ||
		!strings.Contains(csiStorageCapacities.Body.String(), `"storage_class":"standard"`) ||
		!strings.Contains(csiStorageCapacities.Body.String(), `"capacity":"80Gi"`) ||
		strings.Contains(csiStorageCapacities.Body.String(), "private-topology") ||
		strings.Contains(csiStorageCapacities.Body.String(), "maximumVolumeSize") {
		t.Fatalf("CSI storage capacities status = %d, body = %s",
			csiStorageCapacities.Code, csiStorageCapacities.Body.String())
	}
	invalidCSIStorageCapacityNamespace := authenticatedRequest(
		t, handler, cookie, http.MethodGet, csiStorageCapacityPath+"?namespace=bad%2Fnamespace", "",
	)
	if invalidCSIStorageCapacityNamespace.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid CSI storage capacity namespace status = %d, body = %s",
			invalidCSIStorageCapacityNamespace.Code, invalidCSIStorageCapacityNamespace.Body.String())
	}
	assertErrorField(t, invalidCSIStorageCapacityNamespace.Body.Bytes(), "namespace")
	missingCSIStorageCapacityCluster := authenticatedRequest(
		t, handler, cookie, http.MethodGet, "/api/v1/clusters/clu_missing/csi-storage-capacities", "",
	)
	if missingCSIStorageCapacityCluster.Code != http.StatusNotFound {
		t.Fatalf("missing cluster CSI storage capacities status = %d, body = %s",
			missingCSIStorageCapacityCluster.Code, missingCSIStorageCapacityCluster.Body.String())
	}
	csiDriverPath := "/api/v1/clusters/" + created.Data.ID + "/csi-drivers"
	csiDrivers := authenticatedRequest(t, handler, cookie, http.MethodGet, csiDriverPath, "")
	if csiDrivers.Code != http.StatusOK || !strings.Contains(csiDrivers.Body.String(), `"name":"ebs.csi.example.com"`) ||
		strings.Contains(csiDrivers.Body.String(), "private-audience") {
		t.Fatalf("CSI drivers status = %d, body = %s", csiDrivers.Code, csiDrivers.Body.String())
	}
	missingCSIDrivers := authenticatedRequest(t, handler, cookie, http.MethodGet, "/api/v1/clusters/clu_missing/csi-drivers", "")
	if missingCSIDrivers.Code != http.StatusNotFound {
		t.Fatalf("missing cluster CSI drivers status = %d, body = %s", missingCSIDrivers.Code, missingCSIDrivers.Body.String())
	}
	csiDriverDetail := authenticatedRequest(t, handler, cookie, http.MethodGet, csiDriverPath+"/ebs.csi.example.com", "")
	if csiDriverDetail.Code != http.StatusOK ||
		!strings.Contains(csiDriverDetail.Body.String(), `"attach_required":true`) ||
		!strings.Contains(csiDriverDetail.Body.String(), `"fs_group_policy":"File"`) ||
		!strings.Contains(csiDriverDetail.Body.String(), `"token_request_count":2`) ||
		strings.Contains(csiDriverDetail.Body.String(), "private-audience") ||
		strings.Contains(csiDriverDetail.Body.String(), `"tokenRequests"`) {
		t.Fatalf("CSI driver detail status = %d, body = %s", csiDriverDetail.Code, csiDriverDetail.Body.String())
	}
	invalidCSIDriverName := authenticatedRequest(t, handler, cookie, http.MethodGet, csiDriverPath+"/..%2Fcsidrivers", "")
	if invalidCSIDriverName.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid CSI driver name status = %d, body = %s", invalidCSIDriverName.Code, invalidCSIDriverName.Body.String())
	}
	assertErrorField(t, invalidCSIDriverName.Body.Bytes(), "name")
	csiNodePath := "/api/v1/clusters/" + created.Data.ID + "/csi-nodes"
	csiNodes := authenticatedRequest(t, handler, cookie, http.MethodGet, csiNodePath, "")
	if csiNodes.Code != http.StatusOK || !strings.Contains(csiNodes.Body.String(), `"name":"worker-01"`) ||
		!strings.Contains(csiNodes.Body.String(), `"driver_count":2`) || strings.Contains(csiNodes.Body.String(), "private-node-id") {
		t.Fatalf("CSI nodes status = %d, body = %s", csiNodes.Code, csiNodes.Body.String())
	}
	missingCSINodes := authenticatedRequest(t, handler, cookie, http.MethodGet, "/api/v1/clusters/clu_missing/csi-nodes", "")
	if missingCSINodes.Code != http.StatusNotFound {
		t.Fatalf("missing cluster CSI nodes status = %d, body = %s", missingCSINodes.Code, missingCSINodes.Body.String())
	}
	csiNodeDetail := authenticatedRequest(t, handler, cookie, http.MethodGet, csiNodePath+"/worker-01", "")
	if csiNodeDetail.Code != http.StatusOK ||
		!strings.Contains(csiNodeDetail.Body.String(), `"name":"ebs.csi.example.com"`) ||
		!strings.Contains(csiNodeDetail.Body.String(), `"allocatable_count":12`) ||
		!strings.Contains(csiNodeDetail.Body.String(), `"topology_key_count":2`) ||
		strings.Contains(csiNodeDetail.Body.String(), "node_id") ||
		strings.Contains(csiNodeDetail.Body.String(), "topology_keys") {
		t.Fatalf("CSI node detail status = %d, body = %s", csiNodeDetail.Code, csiNodeDetail.Body.String())
	}
	invalidCSINodeName := authenticatedRequest(t, handler, cookie, http.MethodGet, csiNodePath+"/..%2Fnodes", "")
	if invalidCSINodeName.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid CSI node name status = %d, body = %s", invalidCSINodeName.Code, invalidCSINodeName.Body.String())
	}
	assertErrorField(t, invalidCSINodeName.Body.Bytes(), "name")
	accessPath := "/api/v1/clusters/" + created.Data.ID + "/access-resources"
	clusterRoles := authenticatedRequest(t, handler, cookie, http.MethodGet, accessPath+"?kind=clusterroles", "")
	if clusterRoles.Code != http.StatusOK || !strings.Contains(clusterRoles.Body.String(), `"kind":"ClusterRole"`) ||
		!strings.Contains(clusterRoles.Body.String(), `"name":"view"`) {
		t.Fatalf("cluster roles status = %d, body = %s", clusterRoles.Code, clusterRoles.Body.String())
	}
	missingAccessNamespace := authenticatedRequest(t, handler, cookie, http.MethodGet, accessPath+"?kind=roles", "")
	if missingAccessNamespace.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing access namespace status = %d, body = %s", missingAccessNamespace.Code, missingAccessNamespace.Body.String())
	}
	assertErrorField(t, missingAccessNamespace.Body.Bytes(), "namespace")
	ambiguousClusterScope := authenticatedRequest(t, handler, cookie, http.MethodGet, accessPath+"?kind=clusterroles&namespace=payments", "")
	if ambiguousClusterScope.Code != http.StatusUnprocessableEntity {
		t.Fatalf("ambiguous cluster access scope status = %d, body = %s", ambiguousClusterScope.Code, ambiguousClusterScope.Body.String())
	}
	assertErrorField(t, ambiguousClusterScope.Body.Bytes(), "namespace")
	unknownAccessKind := authenticatedRequest(t, handler, cookie, http.MethodGet, accessPath+"?kind=secrets&namespace=payments", "")
	if unknownAccessKind.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown access kind status = %d, body = %s", unknownAccessKind.Code, unknownAccessKind.Body.String())
	}
	assertErrorField(t, unknownAccessKind.Body.Bytes(), "kind")
	roleBindings := authenticatedRequest(t, handler, cookie, http.MethodGet, accessPath+"?kind=rolebindings&namespace=payments", "")
	if roleBindings.Code != http.StatusOK || !strings.Contains(roleBindings.Body.String(), `"name":"gateway-readers"`) {
		t.Fatalf("role bindings status = %d, body = %s", roleBindings.Code, roleBindings.Body.String())
	}
	bindingDetail := authenticatedRequest(
		t, handler, cookie, http.MethodGet,
		accessPath+"/rolebindings/gateway-readers?namespace=payments", "",
	)
	if bindingDetail.Code != http.StatusOK || !strings.Contains(bindingDetail.Body.String(), `"role_ref":{"kind":"Role","name":"gateway-reader"}`) ||
		!strings.Contains(bindingDetail.Body.String(), `"subject_count":1`) || strings.Contains(bindingDetail.Body.String(), "private-subject-field") {
		t.Fatalf("role binding detail status = %d, body = %s", bindingDetail.Code, bindingDetail.Body.String())
	}
	invalidAccessName := authenticatedRequest(
		t, handler, cookie, http.MethodGet,
		accessPath+"/rolebindings/..%2Fbinding?namespace=payments", "",
	)
	if invalidAccessName.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid access name status = %d, body = %s", invalidAccessName.Code, invalidAccessName.Body.String())
	}
	assertErrorField(t, invalidAccessName.Body.Bytes(), "name")
	accessReviewPath := "/api/v1/clusters/" + created.Data.ID + "/service-account-access-reviews"
	accessReviewBody := `{
		"service_account":{"namespace":"payments","name":"gateway"},
		"resource_attributes":{"group":"apps","resource":"deployments","subresource":"scale","verb":"patch","namespace":"payments","name":"gateway-api"}
	}`
	accessReview := authenticatedRequest(t, handler, cookie, http.MethodPost, accessReviewPath, accessReviewBody)
	if accessReview.Code != http.StatusOK ||
		!strings.Contains(accessReview.Body.String(), `"service_account":{"namespace":"payments","name":"gateway"}`) ||
		!strings.Contains(accessReview.Body.String(), `"state":"denied"`) ||
		!strings.Contains(accessReview.Body.String(), `"checked_at":"2026-07-24T08:00:00Z"`) ||
		strings.Contains(accessReview.Body.String(), "authorizer") {
		t.Fatalf("service account access review status = %d, body = %s", accessReview.Code, accessReview.Body.String())
	}
	invalidAccessReview := authenticatedRequest(t, handler, cookie, http.MethodPost, accessReviewPath, `{
		"service_account":{"namespace":"payments","name":"gateway"},
		"resource_attributes":{"resource":"*","verb":"get"}
	}`)
	if invalidAccessReview.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid service account access review status = %d, body = %s", invalidAccessReview.Code, invalidAccessReview.Body.String())
	}
	assertErrorField(t, invalidAccessReview.Body.Bytes(), "resource_attributes.resource")
	unknownAccessReviewField := authenticatedRequest(t, handler, cookie, http.MethodPost, accessReviewPath, `{
		"service_account":{"namespace":"payments","name":"gateway"},
		"resource_attributes":{"resource":"pods","verb":"get"},"reason":"private"
	}`)
	if unknownAccessReviewField.Code != http.StatusBadRequest {
		t.Fatalf("unknown service account access review field status = %d, body = %s", unknownAccessReviewField.Code, unknownAccessReviewField.Body.String())
	}
	crossOriginRequest := httptest.NewRequest(http.MethodPost, accessReviewPath, strings.NewReader(accessReviewBody))
	crossOriginRequest.Header.Set("Content-Type", "application/json")
	crossOriginRequest.Header.Set("Origin", "https://attacker.example")
	crossOriginRequest.AddCookie(cookie)
	crossOriginAccessReview := httptest.NewRecorder()
	handler.ServeHTTP(crossOriginAccessReview, crossOriginRequest)
	if crossOriginAccessReview.Code != http.StatusForbidden {
		t.Fatalf("cross-origin service account access review status = %d, body = %s", crossOriginAccessReview.Code, crossOriginAccessReview.Body.String())
	}
	eventsPath := "/api/v1/clusters/" + created.Data.ID + "/events"
	events := authenticatedRequest(t, handler, cookie, http.MethodGet, eventsPath+"?namespace=payments&type=Warning&limit=100", "")
	if events.Code != http.StatusOK || !strings.Contains(events.Body.String(), `"name":"gateway-warning"`) ||
		!strings.Contains(events.Body.String(), `"namespace":"payments"`) ||
		!strings.Contains(events.Body.String(), `"object_kind":"Pod"`) || strings.Contains(events.Body.String(), "private-event-field") {
		t.Fatalf("events status = %d, body = %s", events.Code, events.Body.String())
	}
	invalidEventType := authenticatedRequest(t, handler, cookie, http.MethodGet, eventsPath+"?type=warning", "")
	if invalidEventType.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid event type status = %d, body = %s", invalidEventType.Code, invalidEventType.Body.String())
	}
	assertErrorField(t, invalidEventType.Body.Bytes(), "type")
	invalidEventLimit := authenticatedRequest(t, handler, cookie, http.MethodGet, eventsPath+"?limit=501", "")
	if invalidEventLimit.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid event limit status = %d, body = %s", invalidEventLimit.Code, invalidEventLimit.Body.String())
	}
	assertErrorField(t, invalidEventLimit.Body.Bytes(), "limit")

	list := authenticatedRequest(t, handler, cookie, http.MethodGet, "/api/v1/clusters", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", list.Code, list.Body.String())
	}
	if !strings.Contains(list.Body.String(), "production-east") {
		t.Errorf("list response = %s", list.Body.String())
	}

	duplicate := authenticatedRequest(t, handler, cookie, http.MethodPost, "/api/v1/clusters", createBody)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d, body = %s", duplicate.Code, duplicate.Body.String())
	}
	assertErrorCode(t, duplicate.Body.Bytes(), "conflict")

	logout := authenticatedRequest(t, handler, cookie, http.MethodDelete, "/api/v1/session", "")
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d", logout.Code)
	}
	afterLogout := authenticatedRequest(t, handler, cookie, http.MethodGet, "/api/v1/clusters", "")
	if afterLogout.Code != http.StatusUnauthorized {
		t.Fatalf("after logout status = %d", afterLogout.Code)
	}
}

func TestServerRejectsUnknownFieldsAndReturnsSecurityHeaders(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	cookie := login(t, handler)
	response := authenticatedRequest(
		t,
		handler,
		cookie,
		http.MethodPost,
		"/api/v1/clusters",
		`{"name":"cluster","environment":"development","server":"https://api.example.com","bearer_token":"token","insecure":true}`,
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	assertErrorCode(t, response.Body.Bytes(), "invalid_json")
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if got := response.Header().Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'self'") {
		t.Errorf("Content-Security-Policy = %q", got)
	}
}

func TestServerValidatesRequestAndPreservesSafeRequestID(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	cookie := login(t, handler)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/clusters", strings.NewReader(`{"name":""}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "req_external-123")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("X-Request-ID"); got != "req_external-123" {
		t.Errorf("X-Request-ID = %q", got)
	}
	assertErrorCode(t, response.Body.Bytes(), "validation_error")
}

func TestHealthEndpointsArePublic(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	for _, path := range []string{"/healthz", "/readyz"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Errorf("GET %s status = %d", path, response.Code)
		}
	}
}

func TestServerRejectsExcessConcurrentAPIRequests(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	cookie := login(t, handler)
	server, ok := handler.(*Server)
	if !ok {
		t.Fatalf("handler type = %T", handler)
	}
	if cap(server.requestSlots) != 16 {
		t.Fatalf("default request limit = %d", cap(server.requestSlots))
	}
	for range cap(server.requestSlots) {
		server.requestSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for range cap(server.requestSlots) {
			<-server.requestSlots
		}
	})

	response := authenticatedRequest(t, handler, cookie, http.MethodGet, "/api/v1/clusters", "")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	assertErrorCode(t, response.Body.Bytes(), "server_busy")

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d", health.Code)
	}
}

func TestBusyErrorIsRetryable(t *testing.T) {
	t.Parallel()

	response := httptest.NewRecorder()
	writeError(response, httptest.NewRequest(http.MethodGet, "/api/v1/clusters/clu_1/summary", nil), domain.ErrBusy)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if retryAfter := response.Header().Get("Retry-After"); retryAfter != "2" {
		t.Fatalf("Retry-After = %q, want 2", retryAfter)
	}
	assertErrorCode(t, response.Body.Bytes(), "server_busy")
}

func TestLoginRateLimitBlocksRepeatedFailures(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	for attempt := 1; attempt <= 5; attempt++ {
		response := loginRequest(handler, "wrong-password")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, body = %s", attempt, response.Code, response.Body.String())
		}
	}

	blocked := loginRequest(handler, "admin-password")
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked status = %d, body = %s", blocked.Code, blocked.Body.String())
	}
	if blocked.Header().Get("Retry-After") == "" {
		t.Error("rate-limited response has no Retry-After header")
	}
	assertErrorCode(t, blocked.Body.Bytes(), "rate_limited")
}

func TestLoginRejectsNonJSONContentType(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(`{"username":"admin","password":"admin-password"}`))
	request.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	assertErrorCode(t, response.Body.Bytes(), "invalid_json")
}

func TestServerExposesAuthenticatedWorkloadDiagnostics(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	cookie := login(t, handler)
	create := authenticatedRequest(t, handler, cookie, http.MethodPost, "/api/v1/clusters", `{
		"name":"development","environment":"development","server":"https://api.example.com","bearer_token":"token"
	}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", create.Code, create.Body.String())
	}
	var created struct {
		Data platform.ClusterView `json:"data"`
	}
	decodeTestJSON(t, create.Body.Bytes(), &created)
	base := "/api/v1/clusters/" + created.Data.ID
	batch := authenticatedRequest(t, handler, cookie, http.MethodGet, base+"/workloads?namespace=payments&kind=job", "")
	if batch.Code != http.StatusOK || !strings.Contains(batch.Body.String(), `"kind":"Job"`) ||
		!strings.Contains(batch.Body.String(), `"name":"daily-settlement"`) {
		t.Fatalf("batch workloads status = %d, body = %s", batch.Code, batch.Body.String())
	}
	invalidKind := authenticatedRequest(t, handler, cookie, http.MethodGet, base+"/workloads?namespace=payments&kind=secret", "")
	if invalidKind.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid workload kind status = %d, body = %s", invalidKind.Code, invalidKind.Body.String())
	}
	assertErrorField(t, invalidKind.Body.Bytes(), "kind")

	detail := authenticatedRequest(t, handler, cookie, http.MethodGet, base+"/workloads/pod/payments/gateway-0", "")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"uid":"uid-gateway-0"`) {
		t.Fatalf("detail status = %d, body = %s", detail.Code, detail.Body.String())
	}
	events := authenticatedRequest(t, handler, cookie, http.MethodGet, base+"/workloads/pod/payments/gateway-0/events?limit=10", "")
	if events.Code != http.StatusOK || !strings.Contains(events.Body.String(), `"reason":"BackOff"`) {
		t.Fatalf("events status = %d, body = %s", events.Code, events.Body.String())
	}
	if strings.Contains(events.Body.String(), "0001-01-01") {
		t.Fatalf("events contain zero timestamps: %s", events.Body.String())
	}
	logs := authenticatedRequest(t, handler, cookie, http.MethodGet, base+"/pods/payments/gateway-0/logs?container=app&tail_lines=250&previous=true", "")
	if logs.Code != http.StatusOK || !strings.Contains(logs.Body.String(), `"tail_lines":250`) {
		t.Fatalf("logs status = %d, body = %s", logs.Code, logs.Body.String())
	}

	invalid := authenticatedRequest(t, handler, cookie, http.MethodGet, base+"/pods/payments/gateway-0/logs?container=app&tail_lines=99999", "")
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid logs status = %d, body = %s", invalid.Code, invalid.Body.String())
	}
	assertErrorCode(t, invalid.Body.Bytes(), "validation_error")
	invalidBool := authenticatedRequest(t, handler, cookie, http.MethodGet, base+"/pods/payments/gateway-0/logs?container=app&previous=sometimes", "")
	if invalidBool.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid boolean status = %d, body = %s", invalidBool.Code, invalidBool.Body.String())
	}
	assertErrorCode(t, invalidBool.Body.Bytes(), "validation_error")

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, base+"/workloads/pod/payments/gateway-0", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized detail status = %d", unauthorized.Code)
	}
}

func TestServerExposesAuthenticatedClusterResources(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	cookie := login(t, handler)
	create := authenticatedRequest(t, handler, cookie, http.MethodPost, "/api/v1/clusters", `{
		"name":"development","environment":"development","server":"https://api.example.com","bearer_token":"token"
	}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", create.Code, create.Body.String())
	}
	var created struct {
		Data platform.ClusterView `json:"data"`
	}
	decodeTestJSON(t, create.Body.Bytes(), &created)
	base := "/api/v1/clusters/" + created.Data.ID

	namespaces := authenticatedRequest(t, handler, cookie, http.MethodGet, base+"/namespaces", "")
	if namespaces.Code != http.StatusOK || !strings.Contains(namespaces.Body.String(), `"name":"payments"`) {
		t.Fatalf("namespaces status = %d, body = %s", namespaces.Code, namespaces.Body.String())
	}
	quotas := authenticatedRequest(t, handler, cookie, http.MethodGet, base+"/resource-quotas?namespace=payments", "")
	if quotas.Code != http.StatusOK || !strings.Contains(quotas.Body.String(), `"name":"compute-quota"`) ||
		!strings.Contains(quotas.Body.String(), `"hard":"4"`) {
		t.Fatalf("resource quotas status = %d, body = %s", quotas.Code, quotas.Body.String())
	}
	limitRanges := authenticatedRequest(t, handler, cookie, http.MethodGet, base+"/limit-ranges?namespace=payments", "")
	if limitRanges.Code != http.StatusOK || !strings.Contains(limitRanges.Body.String(), `"name":"namespace-defaults"`) ||
		!strings.Contains(limitRanges.Body.String(), `"default":"500m"`) {
		t.Fatalf("limit ranges status = %d, body = %s", limitRanges.Code, limitRanges.Body.String())
	}
	autoscalers := authenticatedRequest(t, handler, cookie, http.MethodGet, base+"/horizontal-pod-autoscalers?namespace=payments", "")
	if autoscalers.Code != http.StatusOK || !strings.Contains(autoscalers.Body.String(), `"name":"gateway-autoscaler"`) ||
		!strings.Contains(autoscalers.Body.String(), `"desired_replicas":5`) {
		t.Fatalf("horizontal pod autoscalers status = %d, body = %s", autoscalers.Code, autoscalers.Body.String())
	}
	budgets := authenticatedRequest(t, handler, cookie, http.MethodGet, base+"/pod-disruption-budgets?namespace=payments", "")
	if budgets.Code != http.StatusOK || !strings.Contains(budgets.Body.String(), `"name":"gateway-budget"`) ||
		!strings.Contains(budgets.Body.String(), `"disruptions_allowed":1`) {
		t.Fatalf("pod disruption budgets status = %d, body = %s", budgets.Code, budgets.Body.String())
	}
	podSecurityAdmission := authenticatedRequest(t, handler, cookie, http.MethodGet, base+"/pod-security-admission/namespaces", "")
	if podSecurityAdmission.Code != http.StatusOK ||
		!strings.Contains(podSecurityAdmission.Body.String(), `"name":"payments"`) ||
		!strings.Contains(podSecurityAdmission.Body.String(), `"status":"configured"`) ||
		!strings.Contains(podSecurityAdmission.Body.String(), `"level":"restricted"`) {
		t.Fatalf("pod security admission status = %d, body = %s", podSecurityAdmission.Code, podSecurityAdmission.Body.String())
	}
	nodeVersionSkew := authenticatedRequest(t, handler, cookie, http.MethodGet, base+"/upgrade-readiness/node-versions", "")
	if nodeVersionSkew.Code != http.StatusOK ||
		!strings.Contains(nodeVersionSkew.Body.String(), `"api_server_version":"v1.36.2"`) ||
		!strings.Contains(nodeVersionSkew.Body.String(), `"name":"worker-01"`) ||
		!strings.Contains(nodeVersionSkew.Body.String(), `"status":"upgrade-blocking"`) {
		t.Fatalf("node version skew status = %d, body = %s", nodeVersionSkew.Code, nodeVersionSkew.Body.String())
	}
	deprecatedAPIs := authenticatedRequest(t, handler, cookie, http.MethodGet, base+"/upgrade-readiness/deprecated-apis", "")
	if deprecatedAPIs.Code != http.StatusOK ||
		!strings.Contains(deprecatedAPIs.Body.String(), `"group":"extensions"`) ||
		!strings.Contains(deprecatedAPIs.Body.String(), `"resource":"ingresses"`) ||
		!strings.Contains(deprecatedAPIs.Body.String(), `"removed_release":"1.22"`) {
		t.Fatalf("deprecated API status = %d, body = %s", deprecatedAPIs.Code, deprecatedAPIs.Body.String())
	}
	endpointCertificate := authenticatedRequest(t, handler, cookie, http.MethodGet, base+"/upgrade-readiness/endpoint-certificate", "")
	if endpointCertificate.Code != http.StatusOK ||
		!strings.Contains(endpointCertificate.Body.String(), `"observed_at":"2026-07-29T08:00:00Z"`) ||
		!strings.Contains(endpointCertificate.Body.String(), `"not_after":"2026-08-28T08:00:00Z"`) ||
		!strings.Contains(endpointCertificate.Body.String(), `"remaining_seconds":2592000`) ||
		!strings.Contains(endpointCertificate.Body.String(), `"status":"expiring"`) {
		t.Fatalf("endpoint certificate status = %d, body = %s", endpointCertificate.Code, endpointCertificate.Body.String())
	}
	if strings.Contains(strings.ToLower(endpointCertificate.Body.String()), "subject") ||
		strings.Contains(strings.ToLower(endpointCertificate.Body.String()), "issuer") {
		t.Fatalf("endpoint certificate response exposed identity fields: %s", endpointCertificate.Body.String())
	}
	apiServerReadiness := authenticatedRequest(t, handler, cookie, http.MethodGet, base+"/control-plane/readiness", "")
	if apiServerReadiness.Code != http.StatusOK ||
		!strings.Contains(apiServerReadiness.Body.String(), `"observed_at":"2026-07-31T08:00:00Z"`) ||
		!strings.Contains(apiServerReadiness.Body.String(), `"ready":false`) ||
		!strings.Contains(apiServerReadiness.Body.String(), `"passed_checks":1`) ||
		!strings.Contains(apiServerReadiness.Body.String(), `"failed_checks":1`) ||
		!strings.Contains(apiServerReadiness.Body.String(), `"name":"etcd","status":"failed"`) ||
		strings.Contains(apiServerReadiness.Body.String(), "private-etcd") {
		t.Fatalf("API Server readiness status = %d, body = %s", apiServerReadiness.Code, apiServerReadiness.Body.String())
	}
	disruptionBudgets := authenticatedRequest(t, handler, cookie, http.MethodGet, base+"/upgrade-readiness/disruption-budgets", "")
	if disruptionBudgets.Code != http.StatusOK ||
		!strings.Contains(disruptionBudgets.Body.String(), `"namespace":"payments"`) ||
		!strings.Contains(disruptionBudgets.Body.String(), `"name":"gateway-budget"`) ||
		!strings.Contains(disruptionBudgets.Body.String(), `"disruption_status":"blocked"`) ||
		strings.Contains(disruptionBudgets.Body.String(), "private-selector") ||
		strings.Contains(disruptionBudgets.Body.String(), "disruptedPods") {
		t.Fatalf("disruption budgets status = %d, body = %s", disruptionBudgets.Code, disruptionBudgets.Body.String())
	}
	missingDisruptionBudgetCluster := authenticatedRequest(
		t, handler, cookie, http.MethodGet, "/api/v1/clusters/clu_missing/upgrade-readiness/disruption-budgets", "",
	)
	if missingDisruptionBudgetCluster.Code != http.StatusNotFound {
		t.Fatalf("missing disruption budget cluster status = %d, body = %s",
			missingDisruptionBudgetCluster.Code, missingDisruptionBudgetCluster.Body.String())
	}
	assertErrorCode(t, missingDisruptionBudgetCluster.Body.Bytes(), "not_found")
	missingNamespace := authenticatedRequest(t, handler, cookie, http.MethodGet, base+"/resource-quotas", "")
	if missingNamespace.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing governance namespace status = %d, body = %s", missingNamespace.Code, missingNamespace.Body.String())
	}
	assertErrorField(t, missingNamespace.Body.Bytes(), "namespace")
	for _, path := range []string{"/horizontal-pod-autoscalers", "/pod-disruption-budgets"} {
		response := authenticatedRequest(t, handler, cookie, http.MethodGet, base+path, "")
		if response.Code != http.StatusUnprocessableEntity {
			t.Errorf("missing namespace for %s status = %d, body = %s", path, response.Code, response.Body.String())
		}
		assertErrorField(t, response.Body.Bytes(), "namespace")
	}
	nodes := authenticatedRequest(t, handler, cookie, http.MethodGet, base+"/nodes", "")
	if nodes.Code != http.StatusOK || !strings.Contains(nodes.Body.String(), `"name":"worker-01"`) {
		t.Fatalf("nodes status = %d, body = %s", nodes.Code, nodes.Body.String())
	}
	detail := authenticatedRequest(t, handler, cookie, http.MethodGet, base+"/nodes/worker-01", "")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), `"uid":"uid-worker-01"`) {
		t.Fatalf("node detail status = %d, body = %s", detail.Code, detail.Body.String())
	}
	events := authenticatedRequest(t, handler, cookie, http.MethodGet, base+"/nodes/worker-01/events?limit=10", "")
	if events.Code != http.StatusOK || !strings.Contains(events.Body.String(), `"reason":"NodeNotReady"`) {
		t.Fatalf("node events status = %d, body = %s", events.Code, events.Body.String())
	}
	invalid := authenticatedRequest(t, handler, cookie, http.MethodGet, base+"/nodes/worker-01/events?limit=1000", "")
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid node event limit status = %d, body = %s", invalid.Code, invalid.Body.String())
	}
	assertErrorCode(t, invalid.Body.Bytes(), "validation_error")

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, base+"/nodes", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized nodes status = %d", unauthorized.Code)
	}
}

func TestServerSubmitsControlledWorkloadOperationsAndExposesCapacity(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	cookie := login(t, handler)
	capacity := authenticatedRequest(t, handler, cookie, http.MethodGet, "/api/v1/system/resources", "")
	if capacity.Code != http.StatusOK || !strings.Contains(capacity.Body.String(), `"operation_limit":2`) ||
		!strings.Contains(capacity.Body.String(), `"queue_capacity":16`) ||
		!strings.Contains(capacity.Body.String(), `"kubernetes_reads":{"adaptive":false,"pressure":"normal","active":0,"limit":4,"maximum":4}`) ||
		!strings.Contains(capacity.Body.String(), `"kubernetes_clients":{"entries":0,"capacity":8,"maximum":8,"building":0}`) {
		t.Fatalf("capacity status = %d, body = %s", capacity.Code, capacity.Body.String())
	}
	unauthorizedCapacity := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedCapacity, httptest.NewRequest(http.MethodGet, "/api/v1/system/resources", nil))
	if unauthorizedCapacity.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized capacity status = %d", unauthorizedCapacity.Code)
	}

	create := authenticatedRequest(t, handler, cookie, http.MethodPost, "/api/v1/clusters", `{
		"name":"production-east","environment":"production","server":"https://api.example.com","bearer_token":"token"
	}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", create.Code, create.Body.String())
	}
	var created struct {
		Data platform.ClusterView `json:"data"`
	}
	decodeTestJSON(t, create.Body.Bytes(), &created)
	base := "/api/v1/clusters/" + created.Data.ID + "/workloads/deployment/payments/gateway"

	rejected := authenticatedRequest(t, handler, cookie, http.MethodPost, base+"/scales", `{
		"replicas":5,"resource_version":"42"
	}`)
	if rejected.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unconfirmed scale status = %d, body = %s", rejected.Code, rejected.Body.String())
	}
	assertErrorField(t, rejected.Body.Bytes(), "confirmation")

	scale := authenticatedRequest(t, handler, cookie, http.MethodPost, base+"/scales", `{
		"replicas":5,"resource_version":"42","confirmation":"production-east"
	}`)
	if scale.Code != http.StatusAccepted || scale.Header().Get("Location") == "" ||
		!strings.Contains(scale.Body.String(), `"kind":"workload.scale"`) {
		t.Fatalf("scale status = %d, body = %s", scale.Code, scale.Body.String())
	}
	var scaleOperation struct {
		Data domain.Operation `json:"data"`
	}
	decodeTestJSON(t, scale.Body.Bytes(), &scaleOperation)
	invalidCancel := authenticatedRequest(
		t, handler, cookie, http.MethodPost,
		"/api/v1/operations/"+scaleOperation.Data.ID+"/cancellations", `{"unexpected":true}`,
	)
	if invalidCancel.Code != http.StatusBadRequest {
		t.Fatalf("invalid cancel status = %d, body = %s", invalidCancel.Code, invalidCancel.Body.String())
	}
	canceled := authenticatedRequest(
		t, handler, cookie, http.MethodPost,
		"/api/v1/operations/"+scaleOperation.Data.ID+"/cancellations", `{}`,
	)
	if canceled.Code != http.StatusOK || !strings.Contains(canceled.Body.String(), `"state":"canceled"`) {
		t.Fatalf("cancel status = %d, body = %s", canceled.Code, canceled.Body.String())
	}
	duplicateCancel := authenticatedRequest(
		t, handler, cookie, http.MethodPost,
		"/api/v1/operations/"+scaleOperation.Data.ID+"/cancellations", `{}`,
	)
	if duplicateCancel.Code != http.StatusConflict {
		t.Fatalf("duplicate cancel status = %d, body = %s", duplicateCancel.Code, duplicateCancel.Body.String())
	}
	assertErrorCode(t, duplicateCancel.Body.Bytes(), "invalid_state")
	restart := authenticatedRequest(t, handler, cookie, http.MethodPost, base+"/restarts", `{
		"resource_version":"43","confirmation":"production-east"
	}`)
	if restart.Code != http.StatusAccepted || !strings.Contains(restart.Body.String(), `"kind":"workload.restart"`) {
		t.Fatalf("restart status = %d, body = %s", restart.Code, restart.Body.String())
	}
	preview := authenticatedRequest(t, handler, cookie, http.MethodPost, base+"/image-previews", `{
		"container":"app","current_image":"gateway:1.4.0","image":"gateway:1.5.0","resource_version":"44"
	}`)
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), `"field":"spec.template.spec.containers[name=app].image"`) ||
		!strings.Contains(preview.Body.String(), `"before":"gateway:1.4.0"`) {
		t.Fatalf("image preview status = %d, body = %s", preview.Code, preview.Body.String())
	}
	stalePreview := authenticatedRequest(t, handler, cookie, http.MethodPost, base+"/image-previews", `{
		"container":"app","current_image":"gateway:1.4.0","image":"gateway:1.5.0","resource_version":"stale"
	}`)
	if stalePreview.Code != http.StatusConflict {
		t.Fatalf("stale image preview status = %d, body = %s", stalePreview.Code, stalePreview.Body.String())
	}
	assertErrorCode(t, stalePreview.Body.Bytes(), "conflict")
	invalidPreview := authenticatedRequest(t, handler, cookie, http.MethodPost, base+"/image-previews", `{
		"container":"app","current_image":"gateway:1.4.0","image":"gateway:1.5.0","resource_version":"44","unknown":true
	}`)
	if invalidPreview.Code != http.StatusBadRequest {
		t.Fatalf("invalid image preview status = %d, body = %s", invalidPreview.Code, invalidPreview.Body.String())
	}
	unconfirmedImage := authenticatedRequest(t, handler, cookie, http.MethodPost, base+"/image-updates", `{
		"container":"app","current_image":"gateway:1.4.0","image":"gateway:1.5.0","resource_version":"44"
	}`)
	if unconfirmedImage.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unconfirmed image update status = %d, body = %s", unconfirmedImage.Code, unconfirmedImage.Body.String())
	}
	assertErrorField(t, unconfirmedImage.Body.Bytes(), "confirmation")
	imageUpdate := authenticatedRequest(t, handler, cookie, http.MethodPost, base+"/image-updates", `{
		"container":"app","current_image":"gateway:1.4.0","image":"gateway:1.5.0","resource_version":"44","confirmation":"production-east"
	}`)
	if imageUpdate.Code != http.StatusAccepted || imageUpdate.Header().Get("Location") == "" ||
		!strings.Contains(imageUpdate.Body.String(), `"kind":"workload.image_update"`) {
		t.Fatalf("image update status = %d, body = %s", imageUpdate.Code, imageUpdate.Body.String())
	}

	unsupported := authenticatedRequest(
		t, handler, cookie, http.MethodPost,
		"/api/v1/clusters/"+created.Data.ID+"/workloads/statefulset/payments/gateway/scales",
		`{"replicas":3,"resource_version":"42","confirmation":"production-east"}`,
	)
	if unsupported.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unsupported scale status = %d, body = %s", unsupported.Code, unsupported.Body.String())
	}
	assertErrorField(t, unsupported.Body.Bytes(), "kind")
}

func TestServerHelmReleaseHistory(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	path := "/api/v1/helm-releases/gateway/history?cluster_id=clu_1&namespace=payments"
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, path, nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized history status = %d, want 401", unauthorized.Code)
	}

	cookie := login(t, handler)
	create := authenticatedRequest(t, handler, cookie, http.MethodPost, "/api/v1/clusters", `{
		"name":"development","environment":"development","server":"https://api.example.com:6443","bearer_token":"token"
	}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", create.Code, create.Body.String())
	}
	var created struct {
		Data platform.ClusterView `json:"data"`
	}
	decodeTestJSON(t, create.Body.Bytes(), &created)
	path = "/api/v1/helm-releases/gateway/history?cluster_id=" + created.Data.ID + "&namespace=payments"
	history := authenticatedRequest(t, handler, cookie, http.MethodGet, path, "")
	if history.Code != http.StatusOK || history.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(history.Body.String(), `"name":"gateway"`) ||
		!strings.Contains(history.Body.String(), `"revision":4`) ||
		!strings.Contains(history.Body.String(), `"status":"deployed"`) ||
		strings.Contains(history.Body.String(), "private") || strings.Contains(history.Body.String(), "sh.helm.release.v1") {
		t.Fatalf("history status = %d, headers = %#v, body = %s", history.Code, history.Header(), history.Body.String())
	}

	for _, request := range []struct {
		path  string
		field string
	}{
		{path: "/api/v1/helm-releases/gateway/history?namespace=payments", field: "cluster_id"},
		{path: "/api/v1/helm-releases/gateway/history?cluster_id=" + created.Data.ID + "&namespace=PAYMENTS", field: "namespace"},
		{path: "/api/v1/helm-releases/Gateway/history?cluster_id=" + created.Data.ID + "&namespace=payments", field: "release_name"},
	} {
		response := authenticatedRequest(t, handler, cookie, http.MethodGet, request.path, "")
		if response.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s status = %d, body = %s", request.path, response.Code, response.Body.String())
			continue
		}
		assertErrorField(t, response.Body.Bytes(), request.field)
	}
}

func TestServerDeploymentRevisionHistory(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(t)
	path := "/api/v1/clusters/clu_1/workloads/deployment/payments/gateway/revisions"
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, path, nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized revision history status = %d, want 401", unauthorized.Code)
	}

	cookie := login(t, handler)
	create := authenticatedRequest(t, handler, cookie, http.MethodPost, "/api/v1/clusters", `{
		"name":"development","environment":"development","server":"https://api.example.com:6443","bearer_token":"token"
	}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", create.Code, create.Body.String())
	}
	var created struct {
		Data platform.ClusterView `json:"data"`
	}
	decodeTestJSON(t, create.Body.Bytes(), &created)
	path = "/api/v1/clusters/" + created.Data.ID + "/workloads/deployment/payments/gateway/revisions"
	history := authenticatedRequest(t, handler, cookie, http.MethodGet, path, "")
	if history.Code != http.StatusOK || history.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(history.Body.String(), `"current_revision":4`) ||
		!strings.Contains(history.Body.String(), `"replica_set":"gateway-4"`) ||
		!strings.Contains(history.Body.String(), `"current":true`) ||
		strings.Contains(history.Body.String(), "private") || strings.Contains(history.Body.String(), "ownerReferences") ||
		strings.Contains(history.Body.String(), "uid-gateway") {
		t.Fatalf("revision history status = %d, headers = %#v, body = %s", history.Code, history.Header(), history.Body.String())
	}

	for _, request := range []struct {
		path  string
		field string
	}{
		{path: "/api/v1/clusters/" + created.Data.ID + "/workloads/statefulset/payments/gateway/revisions", field: "kind"},
		{path: "/api/v1/clusters/" + created.Data.ID + "/workloads/deployment/PAYMENTS/gateway/revisions", field: "namespace"},
		{path: "/api/v1/clusters/" + created.Data.ID + "/workloads/deployment/payments/Gateway/revisions", field: "name"},
	} {
		response := authenticatedRequest(t, handler, cookie, http.MethodGet, request.path, "")
		if response.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s status = %d, body = %s", request.path, response.Code, response.Body.String())
			continue
		}
		assertErrorField(t, response.Body.Bytes(), request.field)
	}
}

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	now := time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC)
	fileStore, err := store.Open(filepath.Join(t.TempDir(), "panel.json"), func() time.Time { return now })
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	cipher, err := secure.NewCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("secure.NewCipher() error = %v", err)
	}
	var ids atomic.Int64
	service, err := platform.New(platform.Dependencies{
		Store:              fileStore,
		Cipher:             cipher,
		TargetValidator:    testValidator{},
		KubeFactory:        testKubeFactory{},
		RepositoryChecker:  testRepositoryChecker{},
		Helm:               testHelm{},
		OperationGovernor:  testGovernor(t),
		ReadGovernor:       testReadGovernor(t),
		OperationQueueSize: 16,
		Clock:              func() time.Time { return now },
		NewID: func(prefix string) (string, error) {
			return fmt.Sprintf("%s_%d", prefix, ids.Add(1)), nil
		},
	})
	if err != nil {
		t.Fatalf("platform.New() error = %v", err)
	}
	hasher := secure.NewPasswordHasher(secure.PasswordParams{
		MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	hash, err := hasher.Hash("admin-password")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	sessions, err := auth.NewSessionManager("admin", hash, time.Hour, hasher, func() time.Time { return now })
	if err != nil {
		t.Fatalf("auth.NewSessionManager() error = %v", err)
	}
	handler, err := New(Config{Service: service, Sessions: sessions})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}

func login(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()
	response := loginRequest(handler, "admin-password")
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies = %#v", cookies)
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
		t.Errorf("session cookie = %#v", cookie)
	}
	return cookie
}

func loginRequest(handler http.Handler, password string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	body := fmt.Sprintf(`{"username":"admin","password":%q}`, password)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, request)
	return response
}

func authenticatedRequest(
	t *testing.T,
	handler http.Handler,
	cookie *http.Cookie,
	method string,
	path string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	var payload *bytes.Reader
	if body == "" {
		payload = bytes.NewReader(nil)
	} else {
		payload = bytes.NewReader([]byte(body))
	}
	request := httptest.NewRequest(method, path, payload)
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertErrorCode(t *testing.T, payload []byte, want string) {
	t.Helper()
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeTestJSON(t, payload, &response)
	if response.Error.Code != want {
		t.Errorf("error code = %q, want %q; payload = %s", response.Error.Code, want, payload)
	}
}

func assertErrorField(t *testing.T, payload []byte, want string) {
	t.Helper()
	var response struct {
		Error struct {
			Details []struct {
				Field string `json:"field"`
			} `json:"details"`
		} `json:"error"`
	}
	decodeTestJSON(t, payload, &response)
	if len(response.Error.Details) != 1 || response.Error.Details[0].Field != want {
		t.Errorf("error details = %#v, want field %q; payload = %s", response.Error.Details, want, payload)
	}
}

func decodeTestJSON(t *testing.T, payload []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(payload, target); err != nil {
		t.Fatalf("decode JSON: %v; payload = %s", err, payload)
	}
}

type testValidator struct{}

func (testValidator) Validate(context.Context, string) error { return nil }

type testKubeFactory struct{}

func (testKubeFactory) New(context.Context, kubernetes.Connection) (platform.KubeGateway, error) {
	return testKube{}, nil
}

type testKube struct{}

func (testKube) Probe(context.Context) (domain.ClusterProbe, error) {
	return domain.ClusterProbe{Version: "v1.36.2", NamespaceCount: 1, NodeCount: 1}, nil
}
func (testKube) Capabilities(context.Context, string) ([]domain.KubernetesCapability, error) {
	return []domain.KubernetesCapability{
		{Key: "namespaces.list", State: domain.KubernetesCapabilityAllowed},
		{Key: "pods.logs.get", State: domain.KubernetesCapabilityDenied},
	}, nil
}
func (testKube) Summary(context.Context) (domain.ClusterSummary, error) {
	return domain.ClusterSummary{Version: "v1.36.2"}, nil
}
func (testKube) Namespaces(context.Context) ([]domain.Namespace, error) {
	return []domain.Namespace{{Name: "payments", Status: "Active", Labels: map[string]string{"team": "payments"}}}, nil
}
func (testKube) PodSecurityAdmissionNamespaces(context.Context) ([]domain.KubernetesPodSecurityAdmissionNamespace, error) {
	return []domain.KubernetesPodSecurityAdmissionNamespace{{
		Name: "payments",
		Enforce: domain.KubernetesPodSecurityAdmissionMode{
			Status: domain.PodSecurityAdmissionModeConfigured, Level: "restricted", Version: "latest", VersionDefaulted: true,
		},
		Audit: domain.KubernetesPodSecurityAdmissionMode{Status: domain.PodSecurityAdmissionModeInherited},
		Warn:  domain.KubernetesPodSecurityAdmissionMode{Status: domain.PodSecurityAdmissionModeInherited},
	}}, nil
}
func (testKube) NodeVersionSkew(context.Context) (domain.KubernetesNodeVersionSkewReport, error) {
	return domain.KubernetesNodeVersionSkewReport{
		APIServerVersion: "v1.36.2",
		Nodes: []domain.KubernetesNodeVersionSkew{{
			Name: "worker-01", KubeletVersion: "v1.33.9", Status: domain.NodeVersionUpgradeBlocking,
			MinorSkew: 3, MaximumMinorSkew: 3, MinorSkewComparable: true,
		}},
	}, nil
}
func (testKube) DeprecatedAPIRequests(context.Context) ([]domain.KubernetesDeprecatedAPIRequest, error) {
	return []domain.KubernetesDeprecatedAPIRequest{{
		Group: "extensions", Version: "v1beta1", Resource: "ingresses", RemovedRelease: "1.22",
	}}, nil
}
func (testKube) EndpointCertificate(context.Context) (domain.KubernetesEndpointCertificate, error) {
	observedAt := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	return domain.KubernetesEndpointCertificate{
		ObservedAt: observedAt, NotBefore: observedAt.Add(-24 * time.Hour), NotAfter: observedAt.Add(30 * 24 * time.Hour),
		RemainingSeconds: 2592000, Status: domain.EndpointCertificateExpiring,
	}, nil
}
func (testKube) APIServerReadiness(context.Context) (domain.KubernetesAPIServerReadiness, error) {
	return domain.KubernetesAPIServerReadiness{
		ObservedAt:   time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC),
		Ready:        false,
		PassedChecks: 1,
		FailedChecks: 1,
		Checks: []domain.KubernetesAPIServerReadinessCheck{
			{Name: "ping", Status: domain.APIServerReadinessCheckPassed},
			{Name: "etcd", Status: domain.APIServerReadinessCheckFailed},
		},
	}, nil
}
func (testKube) DisruptionBudgets(context.Context) ([]domain.KubernetesPodDisruptionBudget, error) {
	return []domain.KubernetesPodDisruptionBudget{{
		Namespace: "payments", Name: "gateway-budget", SelectorMode: domain.KubernetesSelectorFiltered,
		MinAvailable: "75%", CurrentHealthy: 3, DesiredHealthy: 3, DisruptionsAllowed: 0, ExpectedPods: 4,
		Observed: true, DisruptionStatus: domain.DisruptionBudgetBlocked,
	}}, nil
}
func (testKube) Nodes(context.Context) ([]domain.Node, error) {
	return []domain.Node{{Name: "worker-01", Status: "Ready"}}, nil
}
func (testKube) NodeDetail(_ context.Context, name string) (domain.NodeDetail, error) {
	return domain.NodeDetail{Node: domain.Node{Name: name, Status: "Ready"}, UID: "uid-" + name}, nil
}
func (testKube) NodeEvents(context.Context, string, int) ([]domain.KubernetesEvent, error) {
	return []domain.KubernetesEvent{{Type: "Warning", Reason: "NodeNotReady"}}, nil
}
func (testKube) CustomResourceDefinitions(context.Context) ([]domain.KubernetesCustomResourceDefinition, error) {
	return []domain.KubernetesCustomResourceDefinition{{
		Name: "widgets.platform.example.com", Resource: "widgets", Group: "platform.example.com",
	}}, nil
}
func (testKube) CustomResourceDefinition(_ context.Context, name string) (domain.KubernetesCustomResourceDefinitionDetail, error) {
	return domain.KubernetesCustomResourceDefinitionDetail{
		KubernetesCustomResourceDefinition: domain.KubernetesCustomResourceDefinition{
			Name: name, Resource: "widgets", Group: "platform.example.com",
		},
		Scope: "Namespaced", Singular: "widget", Kind: "Widget", ListKind: "WidgetList",
		ShortNames: []string{}, Categories: []string{},
		Versions:     []domain.KubernetesCustomResourceDefinitionVersion{{Name: "v1", Served: true, Storage: true}},
		VersionCount: 1, StoredVersions: []string{"v1"}, StoredVersionCount: 1,
		ConversionStrategy: "None", Conditions: []domain.KubernetesCustomResourceDefinitionCondition{},
	}, nil
}
func (testKube) CertificateSigningRequests(context.Context) ([]domain.KubernetesCertificateSigningRequest, error) {
	return []domain.KubernetesCertificateSigningRequest{{Name: "worker-01"}}, nil
}
func (testKube) CertificateSigningRequest(
	_ context.Context,
	name string,
) (domain.KubernetesCertificateSigningRequestDetail, error) {
	return domain.KubernetesCertificateSigningRequestDetail{
		KubernetesCertificateSigningRequest: domain.KubernetesCertificateSigningRequest{Name: name},
		Requester:                           "system:node:worker-01", SignerName: "example.com/node-client",
		Usages: []string{"client auth"}, State: domain.CertificateSigningRequestApproved,
		Conditions:     []domain.KubernetesCertificateSigningRequestCondition{{Type: "Approved", Status: "True"}},
		ConditionCount: 1,
	}, nil
}
func (testKube) PriorityClasses(context.Context) ([]domain.KubernetesPriorityClass, error) {
	return []domain.KubernetesPriorityClass{{Name: "workload-high"}}, nil
}
func (testKube) PriorityClass(_ context.Context, name string) (domain.KubernetesPriorityClassDetail, error) {
	return domain.KubernetesPriorityClassDetail{
		KubernetesPriorityClass: domain.KubernetesPriorityClass{Name: name},
		Value:                   1000000, PreemptionPolicy: domain.PriorityClassPreemptLower,
	}, nil
}
func (testKube) RuntimeClasses(context.Context) ([]domain.KubernetesRuntimeClass, error) {
	return []domain.KubernetesRuntimeClass{{Name: "kata-containers"}}, nil
}
func (testKube) RuntimeClass(_ context.Context, name string) (domain.KubernetesRuntimeClassDetail, error) {
	cpu, memory := "250m", "120Mi"
	return domain.KubernetesRuntimeClassDetail{
		KubernetesRuntimeClass: domain.KubernetesRuntimeClass{Name: name},
		Handler:                "kata-fc",
		OverheadConfigured:     true,
		PodOverheadCPU:         &cpu,
		PodOverheadMemory:      &memory,
		OverheadResourceCount:  3,
		SchedulingConfigured:   true,
		NodeSelectorCount:      2,
		TolerationCount:        2,
	}, nil
}
func (testKube) APIServices(context.Context) ([]domain.KubernetesAPIService, error) {
	return []domain.KubernetesAPIService{{
		Name: "v1beta1.metrics.k8s.io", Group: "metrics.k8s.io", Version: "v1beta1",
		ServiceNamespace: "kube-system", ServiceName: "metrics-server", ServicePort: 443,
		AvailabilityObserved: true, AvailabilityStatus: "False", AvailabilityReason: "FailedDiscoveryCheck",
		ConditionCount: 1, GroupPriorityMinimum: 100, VersionPriority: 100,
	}}, nil
}
func (testKube) AdmissionWebhookConfigurations(
	_ context.Context,
	kind domain.KubernetesAdmissionWebhookConfigurationKind,
) ([]domain.KubernetesAdmissionWebhookConfiguration, error) {
	return []domain.KubernetesAdmissionWebhookConfiguration{{Kind: kind, Name: "policy.platform.example.com"}}, nil
}
func (testKube) AdmissionWebhookConfiguration(
	_ context.Context,
	kind domain.KubernetesAdmissionWebhookConfigurationKind,
	name string,
) (domain.KubernetesAdmissionWebhookConfigurationDetail, error) {
	return domain.KubernetesAdmissionWebhookConfigurationDetail{
		KubernetesAdmissionWebhookConfiguration: domain.KubernetesAdmissionWebhookConfiguration{Kind: kind, Name: name},
		Generation:                              1,
		Webhooks: []domain.KubernetesAdmissionWebhook{{
			Name: "validate.policy.platform.example.com", TargetType: "service",
			ServiceNamespace: "policy-system", ServiceName: "policy-webhook", ServicePort: 443,
			FailurePolicy: "Fail", MatchPolicy: "Equivalent", SideEffects: "None", TimeoutSeconds: 10,
			AdmissionReviewVersions: []string{"v1"},
		}},
		WebhookCount: 1,
	}, nil
}
func (testKube) ValidatingAdmissionPolicies(context.Context) ([]domain.KubernetesAdmissionPolicyResource, error) {
	return []domain.KubernetesAdmissionPolicyResource{{
		Kind: domain.AdmissionPolicyResourcePolicy, Name: "replica-policy.example.com",
	}}, nil
}
func (testKube) ValidatingAdmissionPolicy(
	_ context.Context,
	name string,
) (domain.KubernetesValidatingAdmissionPolicyDetail, error) {
	return domain.KubernetesValidatingAdmissionPolicyDetail{
		KubernetesAdmissionPolicyResource: domain.KubernetesAdmissionPolicyResource{
			Kind: domain.AdmissionPolicyResourcePolicy, Name: name,
		},
		Generation: 2, FailurePolicy: "Fail", ValidationCount: 2,
		Match: domain.KubernetesAdmissionMatchSummary{Configured: true, MatchPolicy: "Equivalent"},
	}, nil
}
func (testKube) ValidatingAdmissionPolicyBindings(context.Context) ([]domain.KubernetesAdmissionPolicyResource, error) {
	return []domain.KubernetesAdmissionPolicyResource{{
		Kind: domain.AdmissionPolicyResourceBinding, Name: "replica-binding.example.com",
	}}, nil
}
func (testKube) ValidatingAdmissionPolicyBinding(
	_ context.Context,
	name string,
) (domain.KubernetesValidatingAdmissionPolicyBindingDetail, error) {
	return domain.KubernetesValidatingAdmissionPolicyBindingDetail{
		KubernetesAdmissionPolicyResource: domain.KubernetesAdmissionPolicyResource{
			Kind: domain.AdmissionPolicyResourceBinding, Name: name,
		},
		Generation: 2, PolicyName: "replica-policy.example.com", ValidationActions: []string{"Deny", "Audit"},
		ParamRefConfigured: true, ParamRefMode: "name", ParameterNotFoundAction: "Deny",
	}, nil
}
func (testKube) Events(_ context.Context, namespace, eventType string, _ int) ([]domain.KubernetesEvent, error) {
	return []domain.KubernetesEvent{{
		Namespace: namespace, Name: "gateway-warning", Type: eventType, Reason: "BackOff",
		ObjectKind: "Pod", ObjectName: "gateway-0", Message: "Back-off restarting container",
		Source: "kubelet", Count: 3,
	}}, nil
}
func (testKube) Workloads(_ context.Context, namespace, kind string) ([]domain.Workload, error) {
	if kind != "job" {
		return nil, nil
	}
	return []domain.Workload{{
		Kind: "Job", Namespace: namespace, Name: "daily-settlement", Ready: 2, Desired: 4,
		Status: "Running", Images: []string{"settlement:1.8.0"},
	}}, nil
}
func (testKube) Services(_ context.Context, namespace string) ([]domain.KubernetesService, error) {
	return []domain.KubernetesService{{
		Namespace: namespace, Name: "gateway", Type: "ClusterIP", ClusterIP: "10.96.0.10",
		Ports: []domain.ServicePort{{Protocol: "TCP", Port: 80}}, PortCount: 1,
	}}, nil
}
func (testKube) Ingresses(context.Context, string) ([]domain.KubernetesIngress, error) {
	return []domain.KubernetesIngress{{
		Namespace: "payments", Name: "gateway", ClassName: "nginx", Hosts: []string{"gateway.example.com"}, HostCount: 1,
	}}, nil
}
func (testKube) EndpointSlices(_ context.Context, namespace string) ([]domain.KubernetesEndpointSlice, error) {
	return []domain.KubernetesEndpointSlice{{
		Namespace: namespace, Name: "gateway-ipv4", ServiceName: "gateway", AddressType: "IPv4",
		EndpointCount: 3, ReadyEndpointCount: 2, ServingEndpointCount: 2, TerminatingEndpointCount: 1,
		ReadyDefaultedCount: 1, ServingDefaultedCount: 1, TerminatingDefaultedCount: 1, PortCount: 1,
	}}, nil
}
func (testKube) NetworkPolicies(_ context.Context, namespace string) ([]domain.KubernetesNetworkPolicy, error) {
	return []domain.KubernetesNetworkPolicy{{
		Namespace: namespace, Name: "gateway-policy", PodSelectorMode: domain.KubernetesSelectorFiltered,
		PodSelectorLabelCount: 1, PolicyTypes: []string{"Ingress", "Egress"},
		IngressRuleCount: 1, IngressPeerCount: 1, IngressPortCount: 1,
	}}, nil
}
func (testKube) ConfigMaps(context.Context, string) ([]domain.KubernetesConfigMap, error) {
	return []domain.KubernetesConfigMap{{Namespace: "payments", Name: "settings", DataCount: 3}}, nil
}
func (testKube) Secrets(_ context.Context, namespace string) ([]domain.KubernetesSecret, error) {
	return []domain.KubernetesSecret{{
		Namespace: namespace, Name: "registry", Type: "kubernetes.io/dockerconfigjson", DataCount: 1,
	}}, nil
}
func (testKube) PersistentVolumeClaims(_ context.Context, namespace string) ([]domain.KubernetesPersistentVolumeClaim, error) {
	return []domain.KubernetesPersistentVolumeClaim{{
		Namespace: namespace, Name: "data", Status: "Bound", Volume: "pv-data", Capacity: "20Gi",
		AccessModes: "RWO", StorageClass: "standard", VolumeMode: "Filesystem",
	}}, nil
}
func (testKube) PersistentVolumes(context.Context) ([]domain.KubernetesPersistentVolume, error) {
	return []domain.KubernetesPersistentVolume{{
		Name: "pv-data", Status: "Bound", Claim: "payments/data", Capacity: "20Gi", AccessModes: "RWO",
		StorageClass: "standard", ReclaimPolicy: "Delete", VolumeMode: "Filesystem",
	}}, nil
}
func (testKube) StorageClasses(context.Context) ([]domain.KubernetesStorageClass, error) {
	return []domain.KubernetesStorageClass{{
		Name: "standard", Provisioner: "csi.example.com", ReclaimPolicy: "Delete",
		VolumeBindingMode: "WaitForFirstConsumer", AllowVolumeExpansion: true, Default: true,
	}}, nil
}
func (testKube) VolumeAttachments(context.Context) ([]domain.KubernetesVolumeAttachment, error) {
	return []domain.KubernetesVolumeAttachment{{
		Name: "attach-data", Attacher: "ebs.csi.example.com", PersistentVolume: "pv-data", Node: "worker-01",
		Status: domain.VolumeAttachmentAttached, CreatedAt: time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC),
	}}, nil
}
func (testKube) CSIStorageCapacities(_ context.Context, namespace string) ([]domain.KubernetesCSIStorageCapacity, error) {
	if namespace == "" {
		namespace = "storage-system"
	}
	return []domain.KubernetesCSIStorageCapacity{{
		Namespace: namespace, Name: "capacity-a", StorageClass: "standard", Capacity: "80Gi",
		CreatedAt: time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC),
	}}, nil
}
func (testKube) CSIDrivers(context.Context) ([]domain.KubernetesCSIDriver, error) {
	return []domain.KubernetesCSIDriver{{Name: "ebs.csi.example.com"}}, nil
}
func (testKube) CSIDriver(_ context.Context, name string) (domain.KubernetesCSIDriverDetail, error) {
	return domain.KubernetesCSIDriverDetail{
		KubernetesCSIDriver: domain.KubernetesCSIDriver{Name: name},
		AttachRequired:      true, PodInfoOnMount: true, StorageCapacity: true, RequiresRepublish: true,
		SELinuxMount: true, FSGroupPolicy: domain.CSIFSGroupPolicyFile,
		VolumeLifecycleModes: []domain.KubernetesCSIVolumeLifecycleMode{
			domain.CSIVolumeLifecyclePersistent, domain.CSIVolumeLifecycleEphemeral,
		},
		TokenRequestCount: 2,
	}, nil
}
func (testKube) CSINodes(context.Context) ([]domain.KubernetesCSINode, error) {
	return []domain.KubernetesCSINode{{
		Name: "worker-01", DriverCount: 2, CreatedAt: time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC),
	}}, nil
}
func (testKube) CSINode(_ context.Context, name string) (domain.KubernetesCSINodeDetail, error) {
	count := int32(12)
	return domain.KubernetesCSINodeDetail{
		KubernetesCSINode: domain.KubernetesCSINode{
			Name: name, DriverCount: 2, CreatedAt: time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC),
		},
		Drivers: []domain.KubernetesCSINodeDriver{
			{Name: "ebs.csi.example.com", AllocatableCount: &count, TopologyKeyCount: 2},
			{Name: "local.csi.example.com", TopologyKeyCount: 0},
		},
	}, nil
}
func (testKube) HelmReleaseHistory(_ context.Context, namespace, name string) (domain.HelmReleaseHistory, error) {
	return domain.HelmReleaseHistory{
		Name: name, Namespace: namespace,
		Revisions: []domain.HelmReleaseRevision{
			{Revision: 4, Status: "deployed", CreatedAt: time.Date(2026, 7, 30, 9, 4, 0, 0, time.UTC)},
			{Revision: 3, Status: "superseded", CreatedAt: time.Date(2026, 7, 30, 9, 3, 0, 0, time.UTC)},
		},
	}, nil
}
func (testKube) ResourceQuotas(_ context.Context, namespace string) ([]domain.KubernetesResourceQuota, error) {
	return []domain.KubernetesResourceQuota{{
		Namespace: namespace, Name: "compute-quota", ResourceCount: 1,
		Resources: []domain.KubernetesQuotaResource{{Name: "requests.cpu", Hard: "4", Used: "2", Observed: true}},
	}}, nil
}
func (testKube) LimitRanges(_ context.Context, namespace string) ([]domain.KubernetesLimitRange, error) {
	return []domain.KubernetesLimitRange{{
		Namespace: namespace, Name: "namespace-defaults", ConstraintCount: 1,
		Constraints: []domain.KubernetesLimitRangeConstraint{{Type: "Container", Resource: "cpu", Default: "500m"}},
	}}, nil
}
func (testKube) HorizontalPodAutoscalers(_ context.Context, namespace string) ([]domain.KubernetesHorizontalPodAutoscaler, error) {
	return []domain.KubernetesHorizontalPodAutoscaler{{
		Namespace: namespace, Name: "gateway-autoscaler", TargetKind: "Deployment", TargetName: "gateway",
		MinReplicas: 2, MaxReplicas: 10, CurrentReplicas: 3, DesiredReplicas: 5, Observed: true,
	}}, nil
}
func (testKube) PodDisruptionBudgets(_ context.Context, namespace string) ([]domain.KubernetesPodDisruptionBudget, error) {
	return []domain.KubernetesPodDisruptionBudget{{
		Namespace: namespace, Name: "gateway-budget", SelectorMode: domain.KubernetesSelectorFiltered,
		MinAvailable: "75%", CurrentHealthy: 3, DesiredHealthy: 3, DisruptionsAllowed: 1, ExpectedPods: 4, Observed: true,
	}}, nil
}
func (testKube) AccessResources(_ context.Context, kind domain.KubernetesAccessResourceKind, namespace string) ([]domain.KubernetesAccessResource, error) {
	if kind == domain.AccessResourceClusterRoles {
		return []domain.KubernetesAccessResource{{Kind: "ClusterRole", Name: "view"}}, nil
	}
	return []domain.KubernetesAccessResource{{Kind: "RoleBinding", Namespace: namespace, Name: "gateway-readers"}}, nil
}
func (testKube) AccessResourceDetail(_ context.Context, reference domain.KubernetesAccessResourceReference) (domain.KubernetesAccessResourceDetail, error) {
	return domain.KubernetesAccessResourceDetail{
		KubernetesAccessResource: domain.KubernetesAccessResource{
			Kind: "RoleBinding", Namespace: reference.Namespace, Name: reference.Name,
		},
		RoleRef:      &domain.KubernetesRoleReference{Kind: "Role", Name: "gateway-reader"},
		Subjects:     []domain.KubernetesAccessSubject{{Kind: "Group", Name: "payments-readers"}},
		SubjectCount: 1,
	}, nil
}
func (testKube) ReviewServiceAccountAccess(context.Context, domain.KubernetesServiceAccountAccessReviewInput) (domain.KubernetesCapabilityState, error) {
	return domain.KubernetesCapabilityDenied, nil
}
func (testKube) WorkloadDetail(_ context.Context, reference domain.WorkloadReference) (domain.WorkloadDetail, error) {
	return domain.WorkloadDetail{
		Workload: domain.Workload{Kind: "Pod", Namespace: reference.Namespace, Name: reference.Name, Status: "Ready"},
		UID:      "uid-gateway-0", ResourceVersion: "42", YAML: "apiVersion: v1\nkind: Pod\n",
	}, nil
}
func (testKube) DeploymentRevisionHistory(_ context.Context, reference domain.WorkloadReference) (domain.DeploymentRevisionHistory, error) {
	return domain.DeploymentRevisionHistory{
		Namespace: reference.Namespace, Name: reference.Name, CurrentRevision: 4,
		Revisions: []domain.DeploymentRevision{{
			Revision: 4, ReplicaSet: "gateway-4", CreatedAt: time.Date(2026, 7, 30, 9, 4, 0, 0, time.UTC), Current: true,
		}},
	}, nil
}
func (testKube) WorkloadEvents(context.Context, domain.WorkloadReference, int) ([]domain.KubernetesEvent, error) {
	return []domain.KubernetesEvent{{Type: "Warning", Reason: "BackOff", Message: "Back-off restarting container"}}, nil
}
func (testKube) PodLogs(_ context.Context, request domain.PodLogRequest) (domain.PodLogs, error) {
	return domain.PodLogs{
		Namespace: request.Namespace, Pod: request.Pod, Container: request.Container, TailLines: request.TailLines,
		Previous: request.Previous, Timestamps: request.Timestamps, Content: "ready\n",
	}, nil
}
func (testKube) ScaleWorkload(_ context.Context, reference domain.WorkloadReference, _ string, replicas int32) (domain.Workload, error) {
	return domain.Workload{Kind: "Deployment", Namespace: reference.Namespace, Name: reference.Name, Desired: replicas}, nil
}
func (testKube) RestartWorkload(_ context.Context, reference domain.WorkloadReference, _ string, _ time.Time) (domain.Workload, error) {
	return domain.Workload{Kind: "Deployment", Namespace: reference.Namespace, Name: reference.Name}, nil
}
func (testKube) PreviewWorkloadImage(_ context.Context, change domain.WorkloadImageChange) (domain.WorkloadImagePreview, error) {
	if change.ResourceVersion == "stale" {
		return domain.WorkloadImagePreview{}, domain.ErrConflict
	}
	return domain.WorkloadImagePreview{
		Kind: "Deployment", Namespace: change.Reference.Namespace, Name: change.Reference.Name,
		Container: change.Container, ResourceVersion: change.ResourceVersion,
		Changes: []domain.WorkloadFieldChange{{
			Field: "spec.template.spec.containers[name=" + change.Container + "].image", Before: change.CurrentImage, After: change.Image,
		}},
	}, nil
}
func (testKube) UpdateWorkloadImage(_ context.Context, change domain.WorkloadImageChange) (domain.Workload, error) {
	return domain.Workload{Kind: "Deployment", Namespace: change.Reference.Namespace, Name: change.Reference.Name, Images: []string{change.Image}}, nil
}

func testGovernor(t *testing.T) platform.OperationGovernor {
	t.Helper()
	value := 0.10
	governor, err := resourceguard.New(resourceguard.Config{
		Enabled: false, MaxConcurrent: 2, HighWatermark: 0.80, CriticalWatermark: 0.95,
		RetryInterval: time.Millisecond, Sampler: testResourceSampler{sample: resourceguard.Sample{MemoryRatio: &value}},
	})
	if err != nil {
		t.Fatalf("resourceguard.New() error = %v", err)
	}
	return governor
}

func testReadGovernor(t *testing.T) platform.ReadGovernor {
	t.Helper()
	value := 0.10
	governor, err := resourceguard.New(resourceguard.Config{
		Enabled: false, MaxConcurrent: 4, HighWatermark: 0.80, CriticalWatermark: 0.95,
		Sampler: testResourceSampler{sample: resourceguard.Sample{MemoryRatio: &value}},
	})
	if err != nil {
		t.Fatalf("resourceguard.New() error = %v", err)
	}
	return governor
}

type testResourceSampler struct{ sample resourceguard.Sample }

func (s testResourceSampler) Sample() resourceguard.Sample { return s.sample }

type testRepositoryChecker struct{}

func (testRepositoryChecker) Check(context.Context, platform.RepositoryConnection) error { return nil }

type testHelm struct{}

func (testHelm) List(context.Context, kubernetes.Connection, string) ([]domain.HelmRelease, error) {
	return nil, nil
}
func (testHelm) Execute(context.Context, domain.OperationKind, platform.HelmRequest) error {
	return nil
}
