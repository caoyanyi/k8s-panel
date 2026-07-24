package httpapi

import (
	"errors"
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

func (s *Server) clusterSummary(w http.ResponseWriter, r *http.Request) {
	item, err := s.service.Summary(r.Context(), r.PathValue("id"))
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

func (s *Server) listWorkloads(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.Workloads(r.Context(), r.PathValue("id"), r.URL.Query().Get("namespace"), r.URL.Query().Get("kind"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
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
