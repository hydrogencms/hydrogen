package access

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

// TokenPrefix starts every API token secret.
const TokenPrefix = "hyt_"

const tokenDisplayPrefix = 8

// NewSecret creates an API token secret ("hyt_" + base64url of 32 random bytes), its SHA-256 hash for storage,
// and the first 8 characters for display. The secret is shown once and never stored.
func NewSecret(rand io.Reader) (secret string, hash []byte, prefix string, err error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand, b); err != nil {
		return "", nil, "", fmt.Errorf("access: token secret: %w", err)
	}
	secret = TokenPrefix + base64.RawURLEncoding.EncodeToString(b)
	return secret, HashSecret(secret), secret[:tokenDisplayPrefix], nil
}

// HashSecret is the SHA-256 stored for a token secret or session cookie value. Fast hashing is fine for
// 256-bit random secrets.
func HashSecret(secret string) []byte {
	h := sha256.Sum256([]byte(secret))
	return h[:]
}
