package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

type TokenGenerator interface {
	New() (token string, hash string, err error)
}

type DefaultTokenGenerator struct{}

func (DefaultTokenGenerator) New() (string, string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generate token: %w", err)
	}
	ok := base64.RawURLEncoding.EncodeToString(buf)
	h := sha256.Sum256([]byte(ok))
	return ok, hex.EncodeToString(h[:]), nil
}
