// Package password hashes and verifies passwords using Argon2id.
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	// These parameters meet OWASP's Argon2id baseline.
	memory      = 19 * 1024
	iterations  = 2
	parallelism = 1
	saltLength  = 16
	keyLength   = 32
	prefix      = "$argon2id$v=19$m=19456,t=2,p=1$"
	MinLength   = 15
	MaxLength   = 128
)

var (
	ErrInvalidPassword = errors.New("password must contain between 15 and 128 characters")
	ErrInvalidHash     = errors.New("invalid or unsupported password hash")
)

// Hash generates a salted Argon2id hash. Passwords are preserved exactly.
func Hash(plaintext string) (string, error) {
	length := utf8.RuneCountInString(plaintext)
	if !utf8.ValidString(plaintext) || length < MinLength || length > MaxLength {
		return "", ErrInvalidPassword
	}
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(plaintext), salt, iterations, memory, parallelism, keyLength)
	return prefix + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key), nil
}

// Verify compares a password against a hash produced by Hash.
// Only the supported parameter set is accepted, bounding memory and CPU usage.
func Verify(encoded, plaintext string) (bool, error) {
	// Each Unicode character uses at most four UTF-8 bytes.
	if len(plaintext) > MaxLength*4 {
		return false, nil
	}
	if len(encoded) != len(prefix)+22+1+43 || !strings.HasPrefix(encoded, prefix) {
		return false, ErrInvalidHash
	}
	saltText, keyText, ok := strings.Cut(strings.TrimPrefix(encoded, prefix), "$")
	if !ok {
		return false, ErrInvalidHash
	}
	salt, saltErr := base64.RawStdEncoding.Strict().DecodeString(saltText)
	expected, keyErr := base64.RawStdEncoding.Strict().DecodeString(keyText)
	if saltErr != nil || keyErr != nil || len(salt) != saltLength || len(expected) != keyLength {
		return false, ErrInvalidHash
	}
	actual := argon2.IDKey([]byte(plaintext), salt, iterations, memory, parallelism, keyLength)
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}
