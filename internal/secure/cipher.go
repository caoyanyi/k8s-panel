package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const ciphertextVersion = "v1."

type Cipher struct {
	aead cipher.AEAD
}

func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, errors.New("encryption key must be exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

func (c *Cipher) SealString(plaintext, associatedData string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("create encryption nonce: %w", err)
	}
	sealed := c.aead.Seal(nil, nonce, []byte(plaintext), []byte(associatedData))
	payload := append(nonce, sealed...)
	return ciphertextVersion + base64.RawURLEncoding.EncodeToString(payload), nil
}

func (c *Cipher) OpenString(encoded, associatedData string) (string, error) {
	if !strings.HasPrefix(encoded, ciphertextVersion) {
		return "", errors.New("unsupported ciphertext version")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(encoded, ciphertextVersion))
	if err != nil {
		return "", errors.New("invalid ciphertext encoding")
	}
	if len(payload) <= c.aead.NonceSize() {
		return "", errors.New("invalid ciphertext length")
	}
	nonce := payload[:c.aead.NonceSize()]
	ciphertext := payload[c.aead.NonceSize():]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, []byte(associatedData))
	if err != nil {
		return "", errors.New("ciphertext authentication failed")
	}
	return string(plaintext), nil
}
