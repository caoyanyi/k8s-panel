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
	Service       *platform.Service
	Sessions      *auth.SessionManager
	StaticDir     string
	SecureCookies bool
}

type Server struct {
	service       *platform.Service
	sessions      *auth.SessionManager
	staticDir     string
	secureCookies bool
	loginLimiter  *failureLimiter
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
	server := &Server{
		service:       config.Service,
		sessions:      config.Sessions,
		staticDir:     config.StaticDir,
		secureCookies: config.SecureCookies,
		loginLimiter:  newFailureLimiter(5, 5*time.Minute, 10_000, time.Now),
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
	s.mux.Handle("GET /api/v1/clusters/{id}/summary", s.protected(http.HandlerFunc(s.clusterSummary)))
	s.mux.Handle("GET /api/v1/clusters/{id}/namespaces", s.protected(http.HandlerFunc(s.listNamespaces)))
	s.mux.Handle("GET /api/v1/clusters/{id}/workloads", s.protected(http.HandlerFunc(s.listWorkloads)))

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
	s.mux.Handle("GET /api/v1/audit-events", s.protected(http.HandlerFunc(s.listAuditEvents)))

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
