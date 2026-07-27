package config

import (
	"encoding/base64"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	environment := map[string]string{
		"PANEL_LISTEN_ADDR":                 "127.0.0.1:9080",
		"PANEL_DATA_FILE":                   "/tmp/panel.json",
		"PANEL_WEB_DIR":                     "/tmp/web",
		"PANEL_ENCRYPTION_KEY":              key,
		"PANEL_ADMIN_USERNAME":              "platform-admin",
		"PANEL_ADMIN_PASSWORD_HASH":         "$argon2id$v=19$m=8192,t=1,p=1$c2FsdHNhbHRzYWx0c2FsdA$MTIzNDU2Nzg5MDEyMzQ1Ng",
		"PANEL_SESSION_TTL":                 "2h",
		"PANEL_HELM_TIMEOUT":                "3m",
		"PANEL_HELM_WORKERS":                "1",
		"PANEL_OPERATION_QUEUE_SIZE":        "32",
		"PANEL_ADAPTIVE_OPERATIONS":         "false",
		"PANEL_KUBERNETES_READ_CONCURRENCY": "3",
		"PANEL_MAX_CONCURRENT_REQUESTS":     "24",
		"PANEL_ALLOWED_PRIVATE_CIDRS":       "10.20.0.0/16,192.168.8.10/32",
		"PANEL_SECURE_COOKIES":              "true",
	}
	loaded, err := Load(func(name string) string { return environment[name] })
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.ListenAddr != "127.0.0.1:9080" || loaded.AdminUsername != "platform-admin" {
		t.Errorf("Load() = %#v", loaded)
	}
	if string(loaded.EncryptionKey) != "0123456789abcdef0123456789abcdef" {
		t.Error("EncryptionKey was not decoded")
	}
	if loaded.SessionTTL != 2*time.Hour || loaded.HelmTimeout != 3*time.Minute {
		t.Errorf("durations = %v, %v", loaded.SessionTTL, loaded.HelmTimeout)
	}
	if loaded.HelmWorkers != 1 || loaded.OperationQueueSize != 32 || loaded.AdaptiveOperations ||
		loaded.KubernetesReadConcurrency != 3 || loaded.MaxConcurrentRequests != 24 {
		t.Errorf("resource limits = %#v", loaded)
	}
	if len(loaded.AllowedPrivateCIDRs) != 2 || !loaded.SecureCookies {
		t.Errorf("network config = %#v", loaded)
	}
}

func TestLoadRejectsInvalidResourceLimits(t *testing.T) {
	t.Parallel()

	base := map[string]string{
		"PANEL_ENCRYPTION_KEY":      base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")),
		"PANEL_ADMIN_PASSWORD_HASH": "$argon2id$hash",
	}
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "zero Helm workers", key: "PANEL_HELM_WORKERS", value: "0"},
		{name: "too many Helm workers", key: "PANEL_HELM_WORKERS", value: "9"},
		{name: "zero operation queue", key: "PANEL_OPERATION_QUEUE_SIZE", value: "0"},
		{name: "too large operation queue", key: "PANEL_OPERATION_QUEUE_SIZE", value: "129"},
		{name: "invalid adaptive operations", key: "PANEL_ADAPTIVE_OPERATIONS", value: "sometimes"},
		{name: "zero Kubernetes read concurrency", key: "PANEL_KUBERNETES_READ_CONCURRENCY", value: "0"},
		{name: "too much Kubernetes read concurrency", key: "PANEL_KUBERNETES_READ_CONCURRENCY", value: "33"},
		{name: "zero concurrent requests", key: "PANEL_MAX_CONCURRENT_REQUESTS", value: "0"},
		{name: "too many concurrent requests", key: "PANEL_MAX_CONCURRENT_REQUESTS", value: "129"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			environment := make(map[string]string, len(base)+1)
			for key, value := range base {
				environment[key] = value
			}
			environment[tt.key] = tt.value
			if _, err := Load(func(name string) string { return environment[name] }); err == nil {
				t.Fatal("Load() error = nil")
			}
		})
	}
}

func TestLoadRequiresSecrets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		environment map[string]string
	}{
		{name: "missing all", environment: map[string]string{}},
		{
			name: "invalid key length",
			environment: map[string]string{
				"PANEL_ENCRYPTION_KEY":      base64.StdEncoding.EncodeToString([]byte("too-short")),
				"PANEL_ADMIN_PASSWORD_HASH": "$argon2id$hash",
			},
		},
		{
			name: "plain password",
			environment: map[string]string{
				"PANEL_ENCRYPTION_KEY":      base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")),
				"PANEL_ADMIN_PASSWORD_HASH": "plaintext-password",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Load(func(name string) string { return tt.environment[name] }); err == nil {
				t.Fatal("Load() error = nil")
			}
		})
	}
}
