package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/outbound"
)

type Config struct {
	ListenAddr            string
	DataFile              string
	WebDir                string
	EncryptionKey         []byte
	AdminUsername         string
	AdminPasswordHash     string
	SessionTTL            time.Duration
	HelmTimeout           time.Duration
	HelmWorkers           int
	MaxConcurrentRequests int
	AllowedPrivateCIDRs   []netip.Prefix
	SecureCookies         bool
	LogLevel              string
}

func Load(getenv func(string) string) (Config, error) {
	if getenv == nil {
		return Config{}, errors.New("environment reader is required")
	}
	loaded := Config{
		ListenAddr:        valueOrDefault(getenv("PANEL_LISTEN_ADDR"), "127.0.0.1:8080"),
		DataFile:          valueOrDefault(getenv("PANEL_DATA_FILE"), "./data/panel.json"),
		WebDir:            valueOrDefault(getenv("PANEL_WEB_DIR"), "./web/dist"),
		AdminUsername:     valueOrDefault(getenv("PANEL_ADMIN_USERNAME"), "admin"),
		AdminPasswordHash: strings.TrimSpace(getenv("PANEL_ADMIN_PASSWORD_HASH")),
		LogLevel:          valueOrDefault(getenv("PANEL_LOG_LEVEL"), "info"),
	}
	if err := validateListenAddress(loaded.ListenAddr); err != nil {
		return Config{}, err
	}
	if loaded.DataFile == "" || loaded.WebDir == "" {
		return Config{}, errors.New("data file and web directory must not be empty")
	}

	encodedKey := strings.TrimSpace(getenv("PANEL_ENCRYPTION_KEY"))
	if encodedKey == "" {
		return Config{}, errors.New("PANEL_ENCRYPTION_KEY is required")
	}
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil || len(key) != 32 {
		return Config{}, errors.New("PANEL_ENCRYPTION_KEY must be Base64 for exactly 32 bytes")
	}
	loaded.EncryptionKey = key
	if !strings.HasPrefix(loaded.AdminPasswordHash, "$argon2id$") {
		return Config{}, errors.New("PANEL_ADMIN_PASSWORD_HASH must be an Argon2id encoded hash")
	}
	if strings.TrimSpace(loaded.AdminUsername) == "" {
		return Config{}, errors.New("PANEL_ADMIN_USERNAME must not be empty")
	}

	loaded.SessionTTL, err = durationOrDefault(getenv("PANEL_SESSION_TTL"), 8*time.Hour)
	if err != nil || loaded.SessionTTL <= 0 || loaded.SessionTTL > 24*time.Hour {
		return Config{}, errors.New("PANEL_SESSION_TTL must be greater than zero and at most 24h")
	}
	loaded.HelmTimeout, err = durationOrDefault(getenv("PANEL_HELM_TIMEOUT"), 5*time.Minute)
	if err != nil || loaded.HelmTimeout < 10*time.Second || loaded.HelmTimeout > 30*time.Minute {
		return Config{}, errors.New("PANEL_HELM_TIMEOUT must be between 10s and 30m")
	}
	loaded.HelmWorkers, err = intOrDefault(getenv("PANEL_HELM_WORKERS"), 2)
	if err != nil || loaded.HelmWorkers < 1 || loaded.HelmWorkers > 8 {
		return Config{}, errors.New("PANEL_HELM_WORKERS must be between 1 and 8")
	}
	loaded.MaxConcurrentRequests, err = intOrDefault(getenv("PANEL_MAX_CONCURRENT_REQUESTS"), 16)
	if err != nil || loaded.MaxConcurrentRequests < 1 || loaded.MaxConcurrentRequests > 128 {
		return Config{}, errors.New("PANEL_MAX_CONCURRENT_REQUESTS must be between 1 and 128")
	}
	loaded.AllowedPrivateCIDRs, err = outbound.ParseAllowedPrefixes(getenv("PANEL_ALLOWED_PRIVATE_CIDRS"))
	if err != nil {
		return Config{}, err
	}
	if raw := strings.TrimSpace(getenv("PANEL_SECURE_COOKIES")); raw != "" {
		loaded.SecureCookies, err = strconv.ParseBool(raw)
		if err != nil {
			return Config{}, errors.New("PANEL_SECURE_COOKIES must be true or false")
		}
	}
	if loaded.LogLevel != "debug" && loaded.LogLevel != "info" && loaded.LogLevel != "warn" && loaded.LogLevel != "error" {
		return Config{}, errors.New("PANEL_LOG_LEVEL must be debug, info, warn or error")
	}
	return loaded, nil
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func durationOrDefault(value string, fallback time.Duration) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	return time.ParseDuration(strings.TrimSpace(value))
}

func intOrDefault(value string, fallback int) (int, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	return strconv.Atoi(strings.TrimSpace(value))
}

func validateListenAddress(address string) error {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("PANEL_LISTEN_ADDR must be host:port: %w", err)
	}
	numericPort, err := strconv.Atoi(port)
	if err != nil || numericPort < 1 || numericPort > 65535 {
		return errors.New("PANEL_LISTEN_ADDR port must be between 1 and 65535")
	}
	return nil
}
