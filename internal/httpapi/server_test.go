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
	invalidNetworkNamespace := authenticatedRequest(t, handler, cookie, http.MethodGet, servicesPath+"?namespace=bad%2Fnamespace", "")
	if invalidNetworkNamespace.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid network namespace status = %d, body = %s", invalidNetworkNamespace.Code, invalidNetworkNamespace.Body.String())
	}
	assertErrorField(t, invalidNetworkNamespace.Body.Bytes(), "namespace")
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
func (testKube) Nodes(context.Context) ([]domain.Node, error) {
	return []domain.Node{{Name: "worker-01", Status: "Ready"}}, nil
}
func (testKube) NodeDetail(_ context.Context, name string) (domain.NodeDetail, error) {
	return domain.NodeDetail{Node: domain.Node{Name: name, Status: "Ready"}, UID: "uid-" + name}, nil
}
func (testKube) NodeEvents(context.Context, string, int) ([]domain.KubernetesEvent, error) {
	return []domain.KubernetesEvent{{Type: "Warning", Reason: "NodeNotReady"}}, nil
}
func (testKube) Events(_ context.Context, namespace, eventType string, _ int) ([]domain.KubernetesEvent, error) {
	return []domain.KubernetesEvent{{
		Namespace: namespace, Name: "gateway-warning", Type: eventType, Reason: "BackOff",
		ObjectKind: "Pod", ObjectName: "gateway-0", Message: "Back-off restarting container",
		Source: "kubelet", Count: 3,
	}}, nil
}
func (testKube) Workloads(context.Context, string, string) ([]domain.Workload, error) {
	return nil, nil
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
func (testKube) WorkloadDetail(_ context.Context, reference domain.WorkloadReference) (domain.WorkloadDetail, error) {
	return domain.WorkloadDetail{
		Workload: domain.Workload{Kind: "Pod", Namespace: reference.Namespace, Name: reference.Name, Status: "Ready"},
		UID:      "uid-gateway-0", ResourceVersion: "42", YAML: "apiVersion: v1\nkind: Pod\n",
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
