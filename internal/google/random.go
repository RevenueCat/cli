package google

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
)

// randomState returns an unguessable OAuth state parameter for CSRF protection.
func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// randomSuffix returns n lowercase alphanumeric characters, for making a
// generated project ID unlikely to collide with an existing global one.
func randomSuffix(n int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is effectively impossible; fall back to a fixed
		// but valid suffix rather than returning an error up a UX path.
		return strings.Repeat("0", n)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}
