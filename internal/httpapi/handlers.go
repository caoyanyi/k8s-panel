package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/auth"
	"github.com/caoyanyi/k8s-panel/internal/domain"
)

const maxJSONBodyBytes = 1024 * 1024

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeData(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) ready(w http.ResponseWriter, _ *http.Request) {
	writeData(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	clientKey := loginClientKey(r)
	if retryAfter, blocked := s.loginLimiter.blocked(clientKey); blocked {
		seconds := int(retryAfter/time.Second) + 1
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		writeErrorStatus(w, r, http.StatusTooManyRequests, "rate_limited", "登录尝试过多，请稍后重试", nil)
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	token, principal, err := s.sessions.Login(input.Username, input.Password)
	if err != nil {
		if errors.Is(err, domain.ErrUnauthorized) {
			s.loginLimiter.failed(clientKey)
		}
		writeError(w, r, err)
		return
	}
	s.loginLimiter.reset(clientKey)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  principal.ExpiresAt,
		HttpOnly: true,
		Secure:   s.secureCookies || r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
	writeData(w, http.StatusOK, principal)
}

func (s *Server) currentSession(w http.ResponseWriter, r *http.Request) {
	writeData(w, http.StatusOK, principal(r))
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		s.sessions.Logout(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: s.secureCookies || r.TLS != nil, SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listClusters(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.ListClusters(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) createCluster(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name        string             `json:"name"`
		Environment domain.Environment `json:"environment"`
		Server      string             `json:"server"`
		CACert      string             `json:"ca_cert"`
		BearerToken string             `json:"bearer_token"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	created, err := s.service.CreateCluster(r.Context(), principal(r).Username, requestID(r), domain.ClusterInput{
		Name: input.Name, Environment: input.Environment, Server: input.Server, CACert: input.CACert, BearerToken: input.BearerToken,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/clusters/"+created.ID)
	writeData(w, http.StatusCreated, created)
}

func (s *Server) getCluster(w http.ResponseWriter, r *http.Request) {
	item, err := s.service.GetCluster(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) patchCluster(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	if input.Enabled == nil {
		writeError(w, r, domain.Invalid("enabled", "is required"))
		return
	}
	item, err := s.service.SetClusterEnabled(r.Context(), principal(r).Username, requestID(r), r.PathValue("id"), *input.Enabled)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) deleteCluster(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Confirmation string `json:"confirmation"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	if err := s.service.DeleteCluster(r.Context(), principal(r).Username, requestID(r), r.PathValue("id"), input.Confirmation); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) testCluster(w http.ResponseWriter, r *http.Request) {
	item, err := s.service.TestClusterConnection(r.Context(), principal(r).Username, requestID(r), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) rotateClusterCredentials(w http.ResponseWriter, r *http.Request) {
	var input struct {
		CACert       string `json:"ca_cert"`
		BearerToken  string `json:"bearer_token"`
		Confirmation string `json:"confirmation"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	item, err := s.service.RotateClusterCredentials(
		r.Context(), principal(r).Username, requestID(r), r.PathValue("id"), domain.ClusterCredentialRotationInput{
			CACert: input.CACert, BearerToken: input.BearerToken, Confirmation: input.Confirmation,
		},
	)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) clusterSummary(w http.ResponseWriter, r *http.Request) {
	item, err := s.service.Summary(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) clusterCapabilities(w http.ResponseWriter, r *http.Request) {
	item, err := s.service.ClusterCapabilities(r.Context(), r.PathValue("id"), r.URL.Query().Get("namespace"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) listNamespaces(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.Namespaces(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listPodSecurityAdmissionNamespaces(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.PodSecurityAdmissionNamespaces(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) nodeVersionSkew(w http.ResponseWriter, r *http.Request) {
	report, err := s.service.NodeVersionSkew(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, report)
}

func (s *Server) listDeprecatedAPIRequests(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.DeprecatedAPIRequests(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) endpointCertificate(w http.ResponseWriter, r *http.Request) {
	evidence, err := s.service.EndpointCertificate(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, evidence)
}

func (s *Server) apiServerReadiness(w http.ResponseWriter, r *http.Request) {
	evidence, err := s.service.APIServerReadiness(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, evidence)
}

func (s *Server) listDisruptionBudgetEvidence(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.DisruptionBudgets(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listNodes(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.Nodes(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) getNodeDetail(w http.ResponseWriter, r *http.Request) {
	item, err := s.service.NodeDetail(r.Context(), r.PathValue("id"), r.PathValue("name"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) listNodeEvents(w http.ResponseWriter, r *http.Request) {
	limit, err := parseBoundedInt(r.URL.Query().Get("limit"), 50, 1, domain.MaxNodeEventLimit, "limit")
	if err != nil {
		writeError(w, r, err)
		return
	}
	items, err := s.service.NodeEvents(r.Context(), r.PathValue("id"), r.PathValue("name"), limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listCustomResourceDefinitions(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.CustomResourceDefinitions(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) getCustomResourceDefinition(w http.ResponseWriter, r *http.Request) {
	item, err := s.service.CustomResourceDefinition(r.Context(), r.PathValue("id"), r.PathValue("name"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) listCertificateSigningRequests(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.CertificateSigningRequests(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) getCertificateSigningRequest(w http.ResponseWriter, r *http.Request) {
	item, err := s.service.CertificateSigningRequest(r.Context(), r.PathValue("id"), r.PathValue("name"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) listPriorityClasses(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.PriorityClasses(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) getPriorityClass(w http.ResponseWriter, r *http.Request) {
	item, err := s.service.PriorityClass(r.Context(), r.PathValue("id"), r.PathValue("name"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) listRuntimeClasses(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.RuntimeClasses(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) getRuntimeClass(w http.ResponseWriter, r *http.Request) {
	item, err := s.service.RuntimeClass(r.Context(), r.PathValue("id"), r.PathValue("name"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) listAPIServices(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.APIServices(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listAdmissionWebhookConfigurations(w http.ResponseWriter, r *http.Request) {
	kind := domain.KubernetesAdmissionWebhookConfigurationKind(r.URL.Query().Get("kind"))
	items, err := s.service.AdmissionWebhookConfigurations(r.Context(), r.PathValue("id"), kind)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) getAdmissionWebhookConfiguration(w http.ResponseWriter, r *http.Request) {
	kind := domain.KubernetesAdmissionWebhookConfigurationKind(r.URL.Query().Get("kind"))
	item, err := s.service.AdmissionWebhookConfiguration(
		r.Context(), r.PathValue("id"), kind, r.PathValue("name"),
	)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) listValidatingAdmissionPolicies(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.ValidatingAdmissionPolicies(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) getValidatingAdmissionPolicy(w http.ResponseWriter, r *http.Request) {
	item, err := s.service.ValidatingAdmissionPolicy(r.Context(), r.PathValue("id"), r.PathValue("name"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) listValidatingAdmissionPolicyBindings(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.ValidatingAdmissionPolicyBindings(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) getValidatingAdmissionPolicyBinding(w http.ResponseWriter, r *http.Request) {
	item, err := s.service.ValidatingAdmissionPolicyBinding(r.Context(), r.PathValue("id"), r.PathValue("name"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	limit, err := parseBoundedInt(r.URL.Query().Get("limit"), 200, 1, domain.MaxClusterEventLimit, "limit")
	if err != nil {
		writeError(w, r, err)
		return
	}
	items, err := s.service.Events(
		r.Context(), r.PathValue("id"), r.URL.Query().Get("namespace"), r.URL.Query().Get("type"), limit,
	)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listWorkloads(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.Workloads(r.Context(), r.PathValue("id"), r.URL.Query().Get("namespace"), r.URL.Query().Get("kind"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listServices(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.Services(r.Context(), r.PathValue("id"), r.URL.Query().Get("namespace"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listIngresses(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.Ingresses(r.Context(), r.PathValue("id"), r.URL.Query().Get("namespace"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listEndpointSlices(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.EndpointSlices(r.Context(), r.PathValue("id"), r.URL.Query().Get("namespace"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listNetworkPolicies(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.NetworkPolicies(r.Context(), r.PathValue("id"), r.URL.Query().Get("namespace"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listConfigMaps(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.ConfigMaps(r.Context(), r.PathValue("id"), r.URL.Query().Get("namespace"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listSecrets(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.Secrets(
		r.Context(), principal(r).Username, requestID(r), r.PathValue("id"), r.URL.Query().Get("namespace"),
	)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listPersistentVolumeClaims(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.PersistentVolumeClaims(r.Context(), r.PathValue("id"), r.URL.Query().Get("namespace"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listPersistentVolumes(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.PersistentVolumes(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listStorageClasses(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.StorageClasses(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listVolumeAttachments(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.VolumeAttachments(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listCSIDrivers(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.CSIDrivers(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) getCSIDriver(w http.ResponseWriter, r *http.Request) {
	item, err := s.service.CSIDriver(r.Context(), r.PathValue("id"), r.PathValue("name"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) listCSINodes(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.CSINodes(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) getCSINode(w http.ResponseWriter, r *http.Request) {
	item, err := s.service.CSINode(r.Context(), r.PathValue("id"), r.PathValue("name"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) listResourceQuotas(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.ResourceQuotas(r.Context(), r.PathValue("id"), r.URL.Query().Get("namespace"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listLimitRanges(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.LimitRanges(r.Context(), r.PathValue("id"), r.URL.Query().Get("namespace"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listHorizontalPodAutoscalers(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.HorizontalPodAutoscalers(r.Context(), r.PathValue("id"), r.URL.Query().Get("namespace"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listPodDisruptionBudgets(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.PodDisruptionBudgets(r.Context(), r.PathValue("id"), r.URL.Query().Get("namespace"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listAccessResources(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.AccessResources(
		r.Context(),
		r.PathValue("id"),
		domain.KubernetesAccessResourceKind(r.URL.Query().Get("kind")),
		r.URL.Query().Get("namespace"),
	)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) getAccessResourceDetail(w http.ResponseWriter, r *http.Request) {
	item, err := s.service.AccessResourceDetail(r.Context(), r.PathValue("id"), domain.KubernetesAccessResourceReference{
		Kind:      domain.KubernetesAccessResourceKind(r.PathValue("kind")),
		Namespace: r.URL.Query().Get("namespace"),
		Name:      r.PathValue("name"),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) reviewServiceAccountAccess(w http.ResponseWriter, r *http.Request) {
	var input domain.KubernetesServiceAccountAccessReviewInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	review, err := s.service.ReviewServiceAccountAccess(
		r.Context(), principal(r).Username, requestID(r), r.PathValue("id"), input,
	)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, review)
}

func (s *Server) getWorkloadDetail(w http.ResponseWriter, r *http.Request) {
	item, err := s.service.WorkloadDetail(r.Context(), r.PathValue("id"), workloadReference(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) getDeploymentRevisionHistory(w http.ResponseWriter, r *http.Request) {
	item, err := s.service.DeploymentRevisionHistory(r.Context(), r.PathValue("id"), workloadReference(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) listWorkloadEvents(w http.ResponseWriter, r *http.Request) {
	limit, err := parseBoundedInt(r.URL.Query().Get("limit"), 50, 1, domain.MaxWorkloadEventLimit, "limit")
	if err != nil {
		writeError(w, r, err)
		return
	}
	items, err := s.service.WorkloadEvents(r.Context(), r.PathValue("id"), workloadReference(r), limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) getPodLogs(w http.ResponseWriter, r *http.Request) {
	tailLines, err := parseBoundedInt(
		r.URL.Query().Get("tail_lines"), 200, 1, domain.MaxPodLogTailLines, "tail_lines",
	)
	if err != nil {
		writeError(w, r, err)
		return
	}
	previous, err := parseOptionalBool(r.URL.Query().Get("previous"), false, "previous")
	if err != nil {
		writeError(w, r, err)
		return
	}
	timestamps, err := parseOptionalBool(r.URL.Query().Get("timestamps"), true, "timestamps")
	if err != nil {
		writeError(w, r, err)
		return
	}
	item, err := s.service.PodLogs(r.Context(), principal(r).Username, requestID(r), r.PathValue("id"), domain.PodLogRequest{
		Namespace:  r.PathValue("namespace"),
		Pod:        r.PathValue("name"),
		Container:  r.URL.Query().Get("container"),
		TailLines:  tailLines,
		Previous:   previous,
		Timestamps: timestamps,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) scaleWorkload(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Replicas        *int32 `json:"replicas"`
		ResourceVersion string `json:"resource_version"`
		Confirmation    string `json:"confirmation"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	s.submitWorkloadOperation(w, r, domain.OperationWorkloadScale, domain.WorkloadOperationInput{
		Replicas: input.Replicas, ResourceVersion: input.ResourceVersion, Confirmation: input.Confirmation,
	})
}

func (s *Server) restartWorkload(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ResourceVersion string `json:"resource_version"`
		Confirmation    string `json:"confirmation"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	s.submitWorkloadOperation(w, r, domain.OperationWorkloadRestart, domain.WorkloadOperationInput{
		ResourceVersion: input.ResourceVersion, Confirmation: input.Confirmation,
	})
}

func (s *Server) previewWorkloadImage(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Container       string `json:"container"`
		CurrentImage    string `json:"current_image"`
		Image           string `json:"image"`
		ResourceVersion string `json:"resource_version"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	preview, err := s.service.PreviewWorkloadImage(
		r.Context(), principal(r).Username, requestID(r), workloadImageOperationInput(
			r, input.Container, input.CurrentImage, input.Image, input.ResourceVersion, "",
		),
	)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, preview)
}

func (s *Server) updateWorkloadImage(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Container       string `json:"container"`
		CurrentImage    string `json:"current_image"`
		Image           string `json:"image"`
		ResourceVersion string `json:"resource_version"`
		Confirmation    string `json:"confirmation"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	operation, err := s.service.SubmitWorkloadImageUpdate(
		r.Context(), principal(r).Username, requestID(r), workloadImageOperationInput(
			r, input.Container, input.CurrentImage, input.Image, input.ResourceVersion, input.Confirmation,
		),
	)
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/operations/"+operation.ID)
	writeData(w, http.StatusAccepted, operation)
}

func workloadImageOperationInput(
	r *http.Request,
	container, currentImage, image, resourceVersion, confirmation string,
) domain.WorkloadImageOperationInput {
	return domain.WorkloadImageOperationInput{
		ClusterID: r.PathValue("id"),
		Change: domain.WorkloadImageChange{
			Reference: workloadReference(r), ResourceVersion: resourceVersion,
			Container: container, CurrentImage: currentImage, Image: image,
		},
		Confirmation: confirmation,
	}
}

func (s *Server) submitWorkloadOperation(
	w http.ResponseWriter,
	r *http.Request,
	kind domain.OperationKind,
	input domain.WorkloadOperationInput,
) {
	input.ClusterID = r.PathValue("id")
	input.Reference = domain.WorkloadReference{
		Kind: r.PathValue("kind"), Namespace: r.PathValue("namespace"), Name: r.PathValue("name"),
	}
	operation, err := s.service.SubmitWorkloadOperation(
		r.Context(), principal(r).Username, requestID(r), kind, input,
	)
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/operations/"+operation.ID)
	writeData(w, http.StatusAccepted, operation)
}

func workloadReference(r *http.Request) domain.WorkloadReference {
	return domain.WorkloadReference{
		Kind: r.PathValue("kind"), Namespace: r.PathValue("namespace"), Name: r.PathValue("name"),
	}
}

func parseBoundedInt(raw string, defaultValue, minimum, maximum int, field string) (int, error) {
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		return 0, domain.Invalid(field, fmt.Sprintf("must be between %d and %d", minimum, maximum))
	}
	return value, nil
}

func parseOptionalBool(raw string, defaultValue bool, field string) (bool, error) {
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, domain.Invalid(field, "must be true or false")
	}
	return value, nil
}

func (s *Server) listRepositories(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.ListRepositories(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) createRepository(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name     string `json:"name"`
		URL      string `json:"url"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	created, err := s.service.CreateRepository(r.Context(), principal(r).Username, requestID(r), domain.RepositoryInput{
		Name: input.Name, URL: input.URL, Username: input.Username, Password: input.Password,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/chart-repositories/"+created.ID)
	writeData(w, http.StatusCreated, created)
}

func (s *Server) patchRepository(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	if input.Enabled == nil {
		writeError(w, r, domain.Invalid("enabled", "is required"))
		return
	}
	item, err := s.service.SetRepositoryEnabled(r.Context(), principal(r).Username, requestID(r), r.PathValue("id"), *input.Enabled)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) deleteRepository(w http.ResponseWriter, r *http.Request) {
	if err := s.service.DeleteRepository(r.Context(), principal(r).Username, requestID(r), r.PathValue("id")); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) testRepository(w http.ResponseWriter, r *http.Request) {
	item, err := s.service.TestRepository(r.Context(), principal(r).Username, requestID(r), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) listHelmReleases(w http.ResponseWriter, r *http.Request) {
	clusterID := r.URL.Query().Get("cluster_id")
	if clusterID == "" {
		writeError(w, r, domain.Invalid("cluster_id", "is required"))
		return
	}
	items, err := s.service.ListHelmReleases(r.Context(), clusterID, r.URL.Query().Get("namespace"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) getHelmReleaseHistory(w http.ResponseWriter, r *http.Request) {
	clusterID := r.URL.Query().Get("cluster_id")
	if clusterID == "" {
		writeError(w, r, domain.Invalid("cluster_id", "is required"))
		return
	}
	item, err := s.service.HelmReleaseHistory(
		r.Context(), clusterID, r.URL.Query().Get("namespace"), r.PathValue("name"),
	)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

type helmWriteInput struct {
	ClusterID    string `json:"cluster_id"`
	Namespace    string `json:"namespace"`
	ReleaseName  string `json:"release_name"`
	Chart        string `json:"chart"`
	RepositoryID string `json:"repository_id"`
	Version      string `json:"version"`
	Values       string `json:"values"`
	Revision     int    `json:"revision"`
}

func (input helmWriteInput) domainInput() domain.HelmOperationInput {
	return domain.HelmOperationInput{
		ClusterID: input.ClusterID, Namespace: input.Namespace, ReleaseName: input.ReleaseName,
		Chart: input.Chart, RepositoryID: input.RepositoryID, Version: input.Version, Values: input.Values, Revision: input.Revision,
	}
}

func (s *Server) installHelmRelease(w http.ResponseWriter, r *http.Request) {
	var input helmWriteInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	s.submitHelm(w, r, domain.OperationHelmInstall, input.domainInput())
}

func (s *Server) upgradeHelmRelease(w http.ResponseWriter, r *http.Request) {
	var input helmWriteInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	input.ReleaseName = r.PathValue("name")
	s.submitHelm(w, r, domain.OperationHelmUpgrade, input.domainInput())
}

func (s *Server) rollbackHelmRelease(w http.ResponseWriter, r *http.Request) {
	var input helmWriteInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	input.ReleaseName = r.PathValue("name")
	s.submitHelm(w, r, domain.OperationHelmRollback, input.domainInput())
}

func (s *Server) uninstallHelmRelease(w http.ResponseWriter, r *http.Request) {
	var input helmWriteInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	input.ReleaseName = r.PathValue("name")
	s.submitHelm(w, r, domain.OperationHelmUninstall, input.domainInput())
}

func (s *Server) submitHelm(w http.ResponseWriter, r *http.Request, kind domain.OperationKind, input domain.HelmOperationInput) {
	operation, err := s.service.SubmitHelmOperation(r.Context(), principal(r).Username, requestID(r), kind, input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/operations/"+operation.ID)
	writeData(w, http.StatusAccepted, operation)
}

func (s *Server) listOperations(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	items, err := s.service.ListOperations(r.Context(), limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) getOperation(w http.ResponseWriter, r *http.Request) {
	item, err := s.service.GetOperation(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) cancelOperation(w http.ResponseWriter, r *http.Request) {
	var input struct{}
	if err := decodeJSON(w, r, &input); err != nil {
		writeInvalidJSON(w, r)
		return
	}
	item, err := s.service.CancelOperation(
		r.Context(), principal(r).Username, requestID(r), r.PathValue("id"),
	)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) listAuditEvents(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	items, err := s.service.ListAuditEvents(r.Context(), limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) systemResources(w http.ResponseWriter, _ *http.Request) {
	writeData(w, http.StatusOK, s.service.OperationCapacity())
}

func parseLimit(raw string) (int, error) {
	if raw == "" {
		return 100, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 100 {
		return 0, domain.Invalid("limit", "must be between 1 and 100")
	}
	return limit, nil
}

func principal(r *http.Request) auth.Principal {
	value, _ := r.Context().Value(principalKey).(auth.Principal)
	return value
}

func requestID(r *http.Request) string {
	value, _ := r.Context().Value(requestIDKey).(string)
	return value
}
