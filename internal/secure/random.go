package secure

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

func RandomID(prefix string) (string, error) {
	random := make([]byte, 18)
	if _, err := io.ReadFull(rand.Reader, random); err != nil {
		return "", fmt.Errorf("create random ID: %w", err)
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(random), nil
}
