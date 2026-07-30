package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/auth"
	"github.com/caoyanyi/k8s-panel/internal/domain"
	"github.com/caoyanyi/k8s-panel/internal/platform"
	"github.com/caoyanyi/k8s-panel/internal/secure"
)

const sessionCookieName = "panel_session"

type Config struct {
	Service               *platform.Service
	Sessions              *auth.SessionManager
	StaticDir             string
	SecureCookies         bool
	MaxConcurrentRequests int
}

type Server struct {
	service       *platform.Service
	sessions      *auth.SessionManager
	staticDir     string
	secureCookies bool
	loginLimiter  *failureLimiter
	requestSlots  chan struct{}
	mux           *http.ServeMux
}

type contextKey string

const (
	requestIDKey contextKey = "request_id"
	principalKey contextKey = "principal"
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,80}$`)

func New(config Config) (*Server, error) {
	if config.Service == nil || config.Sessions == nil {
		return nil, errors.New("service and session manager are required")
	}
	maxConcurrentRequests := config.MaxConcurrentRequests
	if maxConcurrentRequests == 0 {
		maxConcurrentRequests = 16
	}
	if maxConcurrentRequests < 1 || maxConcurrentRequests > 128 {
		return nil, errors.New("max concurrent requests must be between 1 and 128")
	}
	server := &Server{
		service:       config.Service,
		sessions:      config.Sessions,
		staticDir:     config.StaticDir,
		secureCookies: config.SecureCookies,
		loginLimiter:  newFailureLimiter(5, 5*time.Minute, 10_000, time.Now),
		requestSlots:  make(chan struct{}, maxConcurrentRequests),
		mux:           http.NewServeMux(),
	}
	server.routes()
	return server, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
	if !requestIDPattern.MatchString(requestID) {
		generated, err := secure.RandomID("req")
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		requestID = generated
	}
	w.Header().Set("X-Request-ID", requestID)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; manifest-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	if r.TLS != nil {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Cache-Control", "no-store")
	}
	r = r.WithContext(context.WithValue(r.Context(), requestIDKey, requestID))
	if strings.HasPrefix(r.URL.Path, "/api/") {
		select {
		case s.requestSlots <- struct{}{}:
			defer func() { <-s.requestSlots }()
		default:
			writeErrorStatus(w, r, http.StatusServiceUnavailable, "server_busy", "服务繁忙，请稍后重试", nil)
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.health)
	s.mux.HandleFunc("GET /readyz", s.ready)
	s.mux.HandleFunc("POST /api/v1/session", s.login)
	s.mux.Handle("GET /api/v1/session", s.protected(http.HandlerFunc(s.currentSession)))
	s.mux.Handle("DELETE /api/v1/session", s.protected(http.HandlerFunc(s.logout)))

	s.mux.Handle("GET /api/v1/clusters", s.protected(http.HandlerFunc(s.listClusters)))
	s.mux.Handle("POST /api/v1/clusters", s.protected(http.HandlerFunc(s.createCluster)))
	s.mux.Handle("GET /api/v1/clusters/{id}", s.protected(http.HandlerFunc(s.getCluster)))
	s.mux.Handle("PATCH /api/v1/clusters/{id}", s.protected(http.HandlerFunc(s.patchCluster)))
	s.mux.Handle("DELETE /api/v1/clusters/{id}", s.protected(http.HandlerFunc(s.deleteCluster)))
	s.mux.Handle("POST /api/v1/clusters/{id}/connection-tests", s.protected(http.HandlerFunc(s.testCluster)))
	s.mux.Handle("POST /api/v1/clusters/{id}/credential-rotations", s.protected(http.HandlerFunc(s.rotateClusterCredentials)))
	s.mux.Handle("GET /api/v1/clusters/{id}/capabilities", s.protected(http.HandlerFunc(s.clusterCapabilities)))
	s.mux.Handle("GET /api/v1/clusters/{id}/summary", s.protected(http.HandlerFunc(s.clusterSummary)))
	s.mux.Handle("GET /api/v1/clusters/{id}/namespaces", s.protected(http.HandlerFunc(s.listNamespaces)))
	s.mux.Handle("GET /api/v1/clusters/{id}/pod-security-admission/namespaces", s.protected(http.HandlerFunc(s.listPodSecurityAdmissionNamespaces)))
	s.mux.Handle("GET /api/v1/clusters/{id}/upgrade-readiness/node-versions", s.protected(http.HandlerFunc(s.nodeVersionSkew)))
	s.mux.Handle("GET /api/v1/clusters/{id}/upgrade-readiness/deprecated-apis", s.protected(http.HandlerFunc(s.listDeprecatedAPIRequests)))
	s.mux.Handle("GET /api/v1/clusters/{id}/upgrade-readiness/endpoint-certificate", s.protected(http.HandlerFunc(s.endpointCertificate)))
	s.mux.Handle("GET /api/v1/clusters/{id}/upgrade-readiness/disruption-budgets", s.protected(http.HandlerFunc(s.listDisruptionBudgetEvidence)))
	s.mux.Handle("GET /api/v1/clusters/{id}/nodes", s.protected(http.HandlerFunc(s.listNodes)))
	s.mux.Handle("GET /api/v1/clusters/{id}/nodes/{name}", s.protected(http.HandlerFunc(s.getNodeDetail)))
	s.mux.Handle("GET /api/v1/clusters/{id}/nodes/{name}/events", s.protected(http.HandlerFunc(s.listNodeEvents)))
	s.mux.Handle("GET /api/v1/clusters/{id}/custom-resource-definitions", s.protected(http.HandlerFunc(s.listCustomResourceDefinitions)))
	s.mux.Handle("GET /api/v1/clusters/{id}/custom-resource-definitions/{name}", s.protected(http.HandlerFunc(s.getCustomResourceDefinition)))
	s.mux.Handle("GET /api/v1/clusters/{id}/certificate-signing-requests", s.protected(http.HandlerFunc(s.listCertificateSigningRequests)))
	s.mux.Handle("GET /api/v1/clusters/{id}/certificate-signing-requests/{name}", s.protected(http.HandlerFunc(s.getCertificateSigningRequest)))
	s.mux.Handle("GET /api/v1/clusters/{id}/priority-classes", s.protected(http.HandlerFunc(s.listPriorityClasses)))
	s.mux.Handle("GET /api/v1/clusters/{id}/priority-classes/{name}", s.protected(http.HandlerFunc(s.getPriorityClass)))
	s.mux.Handle("GET /api/v1/clusters/{id}/api-services", s.protected(http.HandlerFunc(s.listAPIServices)))
	s.mux.Handle("GET /api/v1/clusters/{id}/admission-webhook-configurations", s.protected(http.HandlerFunc(s.listAdmissionWebhookConfigurations)))
	s.mux.Handle("GET /api/v1/clusters/{id}/admission-webhook-configurations/{name}", s.protected(http.HandlerFunc(s.getAdmissionWebhookConfiguration)))
	s.mux.Handle("GET /api/v1/clusters/{id}/validating-admission-policies", s.protected(http.HandlerFunc(s.listValidatingAdmissionPolicies)))
	s.mux.Handle("GET /api/v1/clusters/{id}/validating-admission-policies/{name}", s.protected(http.HandlerFunc(s.getValidatingAdmissionPolicy)))
	s.mux.Handle("GET /api/v1/clusters/{id}/validating-admission-policy-bindings", s.protected(http.HandlerFunc(s.listValidatingAdmissionPolicyBindings)))
	s.mux.Handle("GET /api/v1/clusters/{id}/validating-admission-policy-bindings/{name}", s.protected(http.HandlerFunc(s.getValidatingAdmissionPolicyBinding)))
	s.mux.Handle("GET /api/v1/clusters/{id}/events", s.protected(http.HandlerFunc(s.listEvents)))
	s.mux.Handle("GET /api/v1/clusters/{id}/workloads", s.protected(http.HandlerFunc(s.listWorkloads)))
	s.mux.Handle("GET /api/v1/clusters/{id}/services", s.protected(http.HandlerFunc(s.listServices)))
	s.mux.Handle("GET /api/v1/clusters/{id}/ingresses", s.protected(http.HandlerFunc(s.listIngresses)))
	s.mux.Handle("GET /api/v1/clusters/{id}/endpoint-slices", s.protected(http.HandlerFunc(s.listEndpointSlices)))
	s.mux.Handle("GET /api/v1/clusters/{id}/network-policies", s.protected(http.HandlerFunc(s.listNetworkPolicies)))
	s.mux.Handle("GET /api/v1/clusters/{id}/configmaps", s.protected(http.HandlerFunc(s.listConfigMaps)))
	s.mux.Handle("GET /api/v1/clusters/{id}/secrets", s.protected(http.HandlerFunc(s.listSecrets)))
	s.mux.Handle("GET /api/v1/clusters/{id}/persistent-volume-claims", s.protected(http.HandlerFunc(s.listPersistentVolumeClaims)))
	s.mux.Handle("GET /api/v1/clusters/{id}/persistent-volumes", s.protected(http.HandlerFunc(s.listPersistentVolumes)))
	s.mux.Handle("GET /api/v1/clusters/{id}/storage-classes", s.protected(http.HandlerFunc(s.listStorageClasses)))
	s.mux.Handle("GET /api/v1/clusters/{id}/resource-quotas", s.protected(http.HandlerFunc(s.listResourceQuotas)))
	s.mux.Handle("GET /api/v1/clusters/{id}/limit-ranges", s.protected(http.HandlerFunc(s.listLimitRanges)))
	s.mux.Handle("GET /api/v1/clusters/{id}/horizontal-pod-autoscalers", s.protected(http.HandlerFunc(s.listHorizontalPodAutoscalers)))
	s.mux.Handle("GET /api/v1/clusters/{id}/pod-disruption-budgets", s.protected(http.HandlerFunc(s.listPodDisruptionBudgets)))
	s.mux.Handle("GET /api/v1/clusters/{id}/access-resources", s.protected(http.HandlerFunc(s.listAccessResources)))
	s.mux.Handle("GET /api/v1/clusters/{id}/access-resources/{kind}/{name}", s.protected(http.HandlerFunc(s.getAccessResourceDetail)))
	s.mux.Handle("POST /api/v1/clusters/{id}/service-account-access-reviews", s.protected(http.HandlerFunc(s.reviewServiceAccountAccess)))
	s.mux.Handle("GET /api/v1/clusters/{id}/workloads/{kind}/{namespace}/{name}", s.protected(http.HandlerFunc(s.getWorkloadDetail)))
	s.mux.Handle("GET /api/v1/clusters/{id}/workloads/{kind}/{namespace}/{name}/events", s.protected(http.HandlerFunc(s.listWorkloadEvents)))
	s.mux.Handle("GET /api/v1/clusters/{id}/pods/{namespace}/{name}/logs", s.protected(http.HandlerFunc(s.getPodLogs)))
	s.mux.Handle("POST /api/v1/clusters/{id}/workloads/{kind}/{namespace}/{name}/scales", s.protected(http.HandlerFunc(s.scaleWorkload)))
	s.mux.Handle("POST /api/v1/clusters/{id}/workloads/{kind}/{namespace}/{name}/restarts", s.protected(http.HandlerFunc(s.restartWorkload)))
	s.mux.Handle("POST /api/v1/clusters/{id}/workloads/{kind}/{namespace}/{name}/image-previews", s.protected(http.HandlerFunc(s.previewWorkloadImage)))
	s.mux.Handle("POST /api/v1/clusters/{id}/workloads/{kind}/{namespace}/{name}/image-updates", s.protected(http.HandlerFunc(s.updateWorkloadImage)))

	s.mux.Handle("GET /api/v1/chart-repositories", s.protected(http.HandlerFunc(s.listRepositories)))
	s.mux.Handle("POST /api/v1/chart-repositories", s.protected(http.HandlerFunc(s.createRepository)))
	s.mux.Handle("PATCH /api/v1/chart-repositories/{id}", s.protected(http.HandlerFunc(s.patchRepository)))
	s.mux.Handle("DELETE /api/v1/chart-repositories/{id}", s.protected(http.HandlerFunc(s.deleteRepository)))
	s.mux.Handle("POST /api/v1/chart-repositories/{id}/connection-tests", s.protected(http.HandlerFunc(s.testRepository)))

	s.mux.Handle("GET /api/v1/helm-releases", s.protected(http.HandlerFunc(s.listHelmReleases)))
	s.mux.Handle("POST /api/v1/helm-releases", s.protected(http.HandlerFunc(s.installHelmRelease)))
	s.mux.Handle("POST /api/v1/helm-releases/{name}/upgrades", s.protected(http.HandlerFunc(s.upgradeHelmRelease)))
	s.mux.Handle("POST /api/v1/helm-releases/{name}/rollbacks", s.protected(http.HandlerFunc(s.rollbackHelmRelease)))
	s.mux.Handle("DELETE /api/v1/helm-releases/{name}", s.protected(http.HandlerFunc(s.uninstallHelmRelease)))

	s.mux.Handle("GET /api/v1/operations", s.protected(http.HandlerFunc(s.listOperations)))
	s.mux.Handle("GET /api/v1/operations/{id}", s.protected(http.HandlerFunc(s.getOperation)))
	s.mux.Handle("POST /api/v1/operations/{id}/cancellations", s.protected(http.HandlerFunc(s.cancelOperation)))
	s.mux.Handle("GET /api/v1/audit-events", s.protected(http.HandlerFunc(s.listAuditEvents)))
	s.mux.Handle("GET /api/v1/system/resources", s.protected(http.HandlerFunc(s.systemResources)))

	s.mux.HandleFunc("/", s.static)
}

func (s *Server) protected(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeError(w, r, domain.ErrUnauthorized)
			return
		}
		principal, err := s.sessions.Authenticate(cookie.Value)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if isMutation(r.Method) && !sameOrigin(r) {
			writeError(w, r, domain.ErrForbidden)
			return
		}
		ctx := context.WithValue(r.Context(), principalKey, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeErrorStatus(w, r, http.StatusNotFound, "not_found", "接口不存在", nil)
		return
	}
	if s.staticDir == "" {
		http.NotFound(w, r)
		return
	}
	cleanPath := strings.TrimPrefix(filepath.Clean("/"+r.URL.Path), "/")
	assetPath := filepath.Join(s.staticDir, cleanPath)
	info, err := os.Stat(assetPath)
	if err != nil || info.IsDir() {
		assetPath = filepath.Join(s.staticDir, "index.html")
		w.Header().Set("Cache-Control", "no-cache")
	} else if strings.Contains(cleanPath, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeFile(w, r, assetPath)
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host != r.Host {
		return false
	}
	if r.TLS != nil {
		return parsed.Scheme == "https"
	}
	return parsed.Scheme == "http"
}

func isMutation(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}
