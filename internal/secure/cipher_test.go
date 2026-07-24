package secure

import (
	"bytes"
	"strings"
	"testing"
)

func TestCipherSealAndOpen(t *testing.T) {
	t.Parallel()

	key := bytes.Repeat([]byte{0x2a}, 32)
	cipher, err := NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}

	plaintext := "service-account-token"
	sealedA, err := cipher.SealString(plaintext, "cluster:one:token")
	if err != nil {
		t.Fatalf("SealString() error = %v", err)
	}
	sealedB, err := cipher.SealString(plaintext, "cluster:one:token")
	if err != nil {
		t.Fatalf("SealString() second error = %v", err)
	}
	if sealedA == sealedB {
		t.Fatal("SealString() reused a nonce")
	}
	if strings.Contains(sealedA, plaintext) {
		t.Fatal("ciphertext contains plaintext")
	}

	opened, err := cipher.OpenString(sealedA, "cluster:one:token")
	if err != nil {
		t.Fatalf("OpenString() error = %v", err)
	}
	if opened != plaintext {
		t.Errorf("OpenString() = %q, want %q", opened, plaintext)
	}
}

func TestCipherRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	if _, err := NewCipher(make([]byte, 31)); err == nil {
		t.Fatal("NewCipher() accepted a non-256-bit key")
	}

	cipher, err := NewCipher(bytes.Repeat([]byte{0x11}, 32))
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}
	sealed, err := cipher.SealString("secret", "cluster:one:token")
	if err != nil {
		t.Fatalf("SealString() error = %v", err)
	}

	if _, err := cipher.OpenString(sealed, "cluster:two:token"); err == nil {
		t.Fatal("OpenString() accepted different AAD")
	}
	if _, err := cipher.OpenString("v1.not-base64", "cluster:one:token"); err == nil {
		t.Fatal("OpenString() accepted malformed ciphertext")
	}
}
