package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/domain"
	"github.com/caoyanyi/k8s-panel/internal/secure"
)

func TestSessionLoginAuthenticateAndLogout(t *testing.T) {
	t.Parallel()

	hasher := secure.NewPasswordHasher(secure.PasswordParams{
		MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	hash, err := hasher.Hash("admin-password")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	now := time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	manager, err := NewSessionManager("admin", hash, time.Hour, hasher, clock)
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}

	token, principal, err := manager.Login("admin", "admin-password")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if token == "" || principal.Username != "admin" || principal.Role != "system-admin" {
		t.Fatalf("Login() = token %q, principal %#v", token, principal)
	}

	authenticated, err := manager.Authenticate(token)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if authenticated.Username != "admin" {
		t.Errorf("Authenticate() username = %q", authenticated.Username)
	}

	manager.Logout(token)
	if _, err := manager.Authenticate(token); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("Authenticate() after logout error = %v", err)
	}
}

func TestSessionRejectsWrongCredentialsAndExpires(t *testing.T) {
	t.Parallel()

	hasher := secure.NewPasswordHasher(secure.PasswordParams{
		MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	hash, err := hasher.Hash("admin-password")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	now := time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC)
	manager, err := NewSessionManager("admin", hash, time.Minute, hasher, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewSessionManager() error = %v", err)
	}

	if _, _, err := manager.Login("admin", "wrong"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("Login(wrong) error = %v", err)
	}
	token, _, err := manager.Login("admin", "admin-password")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := manager.Authenticate(token); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("Authenticate(expired) error = %v", err)
	}
}
