package main

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/auth"
	"github.com/caoyanyi/k8s-panel/internal/buildinfo"
	"github.com/caoyanyi/k8s-panel/internal/chartrepo"
	"github.com/caoyanyi/k8s-panel/internal/config"
	"github.com/caoyanyi/k8s-panel/internal/helmadapter"
	"github.com/caoyanyi/k8s-panel/internal/httpapi"
	"github.com/caoyanyi/k8s-panel/internal/kubernetes"
	"github.com/caoyanyi/k8s-panel/internal/outbound"
	"github.com/caoyanyi/k8s-panel/internal/platform"
	"github.com/caoyanyi/k8s-panel/internal/resourceguard"
	"github.com/caoyanyi/k8s-panel/internal/secure"
	"github.com/caoyanyi/k8s-panel/internal/store"
)

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Println(buildinfo.String("k8s-panel"))
		return
	}

	settings, err := config.Load(os.Getenv)
	if err != nil {
		fatal("load configuration", err)
	}
	logger := newLogger(settings.LogLevel)
	slog.SetDefault(logger)

	fileStore, err := store.Open(settings.DataFile, time.Now)
	if err != nil {
		fatal("open data store", err)
	}
	cipher, err := secure.NewCipher(settings.EncryptionKey)
	if err != nil {
		fatal("initialize encryption", err)
	}
	hasher := secure.NewPasswordHasher(secure.DefaultPasswordParams())
	sessions, err := auth.NewSessionManager(
		settings.AdminUsername,
		settings.AdminPasswordHash,
		settings.SessionTTL,
		hasher,
		time.Now,
	)
	if err != nil {
		fatal("initialize sessions", err)
	}
	policy := outbound.NewPolicy(net.DefaultResolver, settings.AllowedPrivateCIDRs)
	rootCAs, _ := x509.SystemCertPool()
	systemSampler := resourceguard.NewSystemSampler()
	operationGovernor, err := resourceguard.New(resourceguard.Config{
		Enabled:           settings.AdaptiveOperations,
		MaxConcurrent:     settings.HelmWorkers,
		HighWatermark:     resourceguard.DefaultHighWatermark,
		CriticalWatermark: resourceguard.DefaultCriticalWatermark,
		Sampler:           systemSampler,
		Clock:             time.Now,
	})
	if err != nil {
		fatal("initialize operation resource governor", err)
	}
	readGovernor, err := resourceguard.New(resourceguard.Config{
		Enabled:           settings.AdaptiveOperations,
		MaxConcurrent:     settings.KubernetesReadConcurrency,
		HighWatermark:     resourceguard.DefaultHighWatermark,
		CriticalWatermark: resourceguard.DefaultCriticalWatermark,
		Sampler:           systemSampler,
		Clock:             time.Now,
	})
	if err != nil {
		fatal("initialize Kubernetes read resource governor", err)
	}
	service, err := platform.New(platform.Dependencies{
		Store:                     fileStore,
		Cipher:                    cipher,
		TargetValidator:           policy,
		KubeFactory:               kubeFactory{policy: policy},
		RepositoryChecker:         chartrepo.NewChecker(policy, rootCAs),
		Helm:                      helmadapter.New(settings.HelmTimeout, policy, rootCAs),
		OperationGovernor:         operationGovernor,
		ReadGovernor:              readGovernor,
		OperationQueueSize:        settings.OperationQueueSize,
		KubernetesClientCacheSize: settings.KubernetesClientCacheSize,
		KubernetesClientCacheTTL:  settings.KubernetesClientCacheTTL,
		Clock:                     time.Now,
		NewID:                     secure.RandomID,
	})
	if err != nil {
		fatal("initialize platform service", err)
	}
	handler, err := httpapi.New(httpapi.Config{
		Service:               service,
		Sessions:              sessions,
		StaticDir:             settings.WebDir,
		SecureCookies:         settings.SecureCookies,
		MaxConcurrentRequests: settings.MaxConcurrentRequests,
	})
	if err != nil {
		fatal("initialize HTTP API", err)
	}

	rootContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go service.Run(rootContext, settings.HelmWorkers)

	server := &http.Server{
		Addr:              settings.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    32 * 1024,
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("panel server listening", "address", settings.ListenAddr)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case <-rootContext.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			logger.Error("HTTP shutdown failed", "error", err)
		}
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			fatal("serve HTTP", err)
		}
	}
}

type kubeFactory struct {
	policy *outbound.Policy
}

func (f kubeFactory) New(ctx context.Context, connection kubernetes.Connection) (platform.KubeGateway, error) {
	return kubernetes.NewClientContext(ctx, connection, f.policy)
}

func newLogger(level string) *slog.Logger {
	var selected slog.Level
	switch level {
	case "debug":
		selected = slog.LevelDebug
	case "warn":
		selected = slog.LevelWarn
	case "error":
		selected = slog.LevelError
	default:
		selected = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: selected}))
}

func fatal(message string, err error) {
	slog.Error(message, "error", err)
	os.Exit(1)
}
