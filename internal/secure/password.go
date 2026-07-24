package secure

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"
)

type PasswordParams struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

type PasswordHasher struct {
	params PasswordParams
}

func DefaultPasswordParams() PasswordParams {
	return PasswordParams{
		MemoryKiB:   64 * 1024,
		Iterations:  3,
		Parallelism: 2,
		SaltLength:  16,
		KeyLength:   32,
	}
}

func NewPasswordHasher(params PasswordParams) *PasswordHasher {
	return &PasswordHasher{params: params}
}

func (h *PasswordHasher) Hash(password string) (string, error) {
	if err := validatePasswordParams(h.params); err != nil {
		return "", err
	}
	if password == "" {
		return "", errors.New("password must not be empty")
	}
	salt := make([]byte, h.params.SaltLength)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", fmt.Errorf("create password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, h.params.Iterations, h.params.MemoryKiB, h.params.Parallelism, h.params.KeyLength)
	return fmt.Sprintf(
		"$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		h.params.MemoryKiB,
		h.params.Iterations,
		h.params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func (h *PasswordHasher) Verify(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false, errors.New("invalid password hash format")
	}

	var params PasswordParams
	var parallelism uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &params.MemoryKiB, &params.Iterations, &parallelism); err != nil || parallelism > 255 {
		return false, errors.New("invalid password hash parameters")
	}
	params.Parallelism = uint8(parallelism)
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, errors.New("invalid password hash salt")
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, errors.New("invalid password hash value")
	}
	params.SaltLength = uint32(len(salt))
	params.KeyLength = uint32(len(want))
	if err := validatePasswordParams(params); err != nil {
		return false, err
	}

	got := argon2.IDKey([]byte(password), salt, params.Iterations, params.MemoryKiB, params.Parallelism, params.KeyLength)
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func validatePasswordParams(params PasswordParams) error {
	if params.MemoryKiB < 8*1024 || params.MemoryKiB > 1024*1024 {
		return errors.New("argon2 memory must be between 8 MiB and 1 GiB")
	}
	if params.Iterations < 1 || params.Iterations > 10 {
		return errors.New("argon2 iterations must be between 1 and 10")
	}
	if params.Parallelism < 1 || params.Parallelism > 16 {
		return errors.New("argon2 parallelism must be between 1 and 16")
	}
	if params.SaltLength < 16 || params.SaltLength > 64 || params.KeyLength < 16 || params.KeyLength > 64 {
		return errors.New("argon2 salt and key lengths must be between 16 and 64 bytes")
	}
	return nil
}
