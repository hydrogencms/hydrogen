package access

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Password parameters. The encoding is "pbkdf2-sha256$<iterations>$<salt>$<key>" with salt and key in unpadded
// standard base64 (its alphabet has no '$').
const (
	passwordScheme     = "pbkdf2-sha256"
	passwordIterations = 600_000
	passwordSaltLen    = 16
	passwordKeyLen     = 32
	// MinPasswordLen and MaxPasswordLen are in bytes.
	MinPasswordLen = 12
	MaxPasswordLen = 256
	// maxStoredIterations bounds the work a tampered stored hash can demand.
	maxStoredIterations = 10_000_000
)

// ErrPasswordLength: the password is shorter than MinPasswordLen or longer than MaxPasswordLen bytes.
var ErrPasswordLength = fmt.Errorf("access: password must be %d to %d bytes", MinPasswordLen, MaxPasswordLen)

// ValidatePassword enforces the length rules; there are no composition rules.
func ValidatePassword(pw string) error {
	if len(pw) < MinPasswordLen || len(pw) > MaxPasswordLen {
		return ErrPasswordLength
	}
	return nil
}

// HashPassword returns the encoded PBKDF2-HMAC-SHA256 hash of pw with a fresh salt from rand.
func HashPassword(rand io.Reader, pw string) (string, error) {
	return HashPasswordWith(rand, pw, passwordIterations)
}

// HashPasswordWith is HashPassword with an explicit iteration count (0 = the default); lower counts are for tests.
func HashPasswordWith(rand io.Reader, pw string, iter int) (string, error) {
	if err := ValidatePassword(pw); err != nil {
		return "", err
	}
	return hashPassword(rand, pw, iterationsOr(iter))
}

func iterationsOr(n int) int {
	if n <= 0 {
		return passwordIterations
	}
	return n
}

func hashPassword(rand io.Reader, pw string, iter int) (string, error) {
	salt := make([]byte, passwordSaltLen)
	if _, err := io.ReadFull(rand, salt); err != nil {
		return "", fmt.Errorf("access: salt: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, pw, salt, iter, passwordKeyLen)
	if err != nil {
		return "", fmt.Errorf("access: hash password: %w", err)
	}
	enc := base64.RawStdEncoding
	return strings.Join([]string{passwordScheme, strconv.Itoa(iter), enc.EncodeToString(salt), enc.EncodeToString(key)}, "$"), nil
}

var errPasswordFormat = errors.New("access: malformed password hash")

// VerifyPassword compares pw with an encoded hash in constant time. needsRehash reports a correct password
// whose hash uses other parameters than the current ones. A malformed hash never verifies.
func VerifyPassword(encoded, pw string) (ok, needsRehash bool) {
	return VerifyPasswordWith(encoded, pw, passwordIterations)
}

// VerifyPasswordWith is VerifyPassword judging "current parameters" by iter (0 = the default).
func VerifyPasswordWith(encoded, pw string, iter int) (ok, needsRehash bool) {
	want := iterationsOr(iter)
	return verifyPassword(encoded, pw, want)
}

func verifyPassword(encoded, pw string, wantIter int) (ok, needsRehash bool) {
	iter, salt, want, err := parsePasswordHash(encoded)
	if err != nil {
		return false, false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	if err != nil {
		return false, false
	}
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false
	}
	return true, iter != wantIter || len(salt) != passwordSaltLen || len(want) != passwordKeyLen
}

func parsePasswordHash(s string) (iter int, salt, key []byte, err error) {
	parts := strings.Split(s, "$")
	if len(parts) != 4 || parts[0] != passwordScheme {
		return 0, nil, nil, errPasswordFormat
	}
	iter, err = strconv.Atoi(parts[1])
	if err != nil || iter < 1 || iter > maxStoredIterations {
		return 0, nil, nil, errPasswordFormat
	}
	enc := base64.RawStdEncoding
	if salt, err = enc.DecodeString(parts[2]); err != nil || len(salt) == 0 {
		return 0, nil, nil, errPasswordFormat
	}
	if key, err = enc.DecodeString(parts[3]); err != nil || len(key) == 0 || len(key) > 64 {
		return 0, nil, nil, errPasswordFormat
	}
	return iter, salt, key, nil
}
