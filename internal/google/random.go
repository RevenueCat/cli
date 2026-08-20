package google

import (
	"crypto/rand"
	"encoding/base64"
)

// randomState returns an unguessable OAuth state parameter for CSRF protection.
func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
