package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

type PasswordVerifier interface {
	Verify(encoded, password string) (bool, error)
}

type Principal struct {
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
}

type SessionManager struct {
	mu           sync.Mutex
	username     string
	passwordHash string
	ttl          time.Duration
	verifier     PasswordVerifier
	clock        func() time.Time
	sessions     map[[sha256.Size]byte]Principal
}

func NewSessionManager(
	username string,
	passwordHash string,
	ttl time.Duration,
	verifier PasswordVerifier,
	clock func() time.Time,
) (*SessionManager, error) {
	if strings.TrimSpace(username) == "" {
		return nil, errors.New("admin username must not be empty")
	}
	if !strings.HasPrefix(passwordHash, "$argon2id$") {
		return nil, errors.New("admin password hash must use Argon2id")
	}
	if ttl <= 0 || ttl > 24*time.Hour {
		return nil, errors.New("session TTL must be between zero and 24 hours")
	}
	if verifier == nil {
		return nil, errors.New("password verifier is required")
	}
	if clock == nil {
		clock = time.Now
	}
	return &SessionManager{
		username:     username,
		passwordHash: passwordHash,
		ttl:          ttl,
		verifier:     verifier,
		clock:        clock,
		sessions:     make(map[[sha256.Size]byte]Principal),
	}, nil
}

func (m *SessionManager) Login(username, password string) (string, Principal, error) {
	matched, err := m.verifier.Verify(m.passwordHash, password)
	if err != nil {
		return "", Principal{}, fmt.Errorf("verify configured password hash: %w", err)
	}
	if username != m.username || !matched {
		return "", Principal{}, domain.ErrUnauthorized
	}
	rawToken := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, rawToken); err != nil {
		return "", Principal{}, fmt.Errorf("create session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(rawToken)
	digest := sha256.Sum256([]byte(token))
	principal := Principal{
		Username:  m.username,
		Role:      "system-admin",
		ExpiresAt: m.clock().UTC().Add(m.ttl),
	}
	m.mu.Lock()
	m.removeExpiredLocked()
	m.sessions[digest] = principal
	m.mu.Unlock()
	return token, principal, nil
}

func (m *SessionManager) Authenticate(token string) (Principal, error) {
	if token == "" {
		return Principal{}, domain.ErrUnauthorized
	}
	digest := sha256.Sum256([]byte(token))
	m.mu.Lock()
	defer m.mu.Unlock()
	principal, ok := m.sessions[digest]
	if !ok {
		return Principal{}, domain.ErrUnauthorized
	}
	if !m.clock().UTC().Before(principal.ExpiresAt) {
		delete(m.sessions, digest)
		return Principal{}, domain.ErrUnauthorized
	}
	return principal, nil
}

func (m *SessionManager) Logout(token string) {
	digest := sha256.Sum256([]byte(token))
	m.mu.Lock()
	delete(m.sessions, digest)
	m.mu.Unlock()
}

func (m *SessionManager) removeExpiredLocked() {
	now := m.clock().UTC()
	for digest, principal := range m.sessions {
		if !now.Before(principal.ExpiresAt) {
			delete(m.sessions, digest)
		}
	}
}
