package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

var (
	ErrInvalidKey       = errors.New("invalid api key")
	ErrRevokedKey       = errors.New("revoked api key")
	ErrIPNotAllowed     = errors.New("ip not allowed")
	ErrInvalidWhitelist = errors.New("invalid ip whitelist")
)

type Record struct {
	ID          string
	UserID      string
	KeyHash     string
	Scopes      []string
	IPWhitelist []string
	RevokedAt   *time.Time
}

func Generate(env string) (fullKey string, prefix string, hash string, err error) {
	prefix, err = generatePrefix()
	if err != nil {
		return "", "", "", err
	}
	secret, err := generateSecret()
	if err != nil {
		return "", "", "", err
	}
	fullKey = fmt.Sprintf("ck_%s_%s.%s", env, prefix, secret)
	hash = Hash(prefix, secret)
	return fullKey, prefix, hash, nil
}

func Parse(key string) (env string, prefix string, secret string, err error) {
	parts := strings.SplitN(key, ".", 2)
	if len(parts) != 2 {
		return "", "", "", ErrInvalidKey
	}
	head := parts[0]
	secret = parts[1]

	headParts := strings.SplitN(head, "_", 3)
	if len(headParts) != 3 || headParts[0] != "ck" {
		return "", "", "", ErrInvalidKey
	}
	env = headParts[1]
	prefix = headParts[2]
	if env == "" || prefix == "" || secret == "" {
		return "", "", "", ErrInvalidKey
	}
	return env, prefix, secret, nil
}

func Hash(prefix, secret string) string {
	sum := sha256.Sum256([]byte(prefix + "." + secret))
	return hex.EncodeToString(sum[:])
}

func Verify(key string, record Record, clientIP string) error {
	_, prefix, secret, err := Parse(key)
	if err != nil {
		return err
	}

	hash := Hash(prefix, secret)
	if !strings.EqualFold(hash, record.KeyHash) {
		return ErrInvalidKey
	}

	if record.RevokedAt != nil {
		return ErrRevokedKey
	}

	if !IPAllowed(clientIP, record.IPWhitelist) {
		return ErrIPNotAllowed
	}

	return nil
}

func VerifyAPIKey(key string, record Record, clientIP string) (string, []string, error) {
	if err := Verify(key, record, clientIP); err != nil {
		return "", nil, err
	}
	return record.UserID, record.Scopes, nil
}

func ValidateIPWhitelist(whitelist []string) error {
	for _, entry := range whitelist {
		if strings.TrimSpace(entry) == "" {
			return ErrInvalidWhitelist
		}
		if strings.Contains(entry, "/") {
			if _, _, err := net.ParseCIDR(entry); err != nil {
				return ErrInvalidWhitelist
			}
			continue
		}
		if net.ParseIP(entry) == nil {
			return ErrInvalidWhitelist
		}
	}
	return nil
}

func IPAllowed(clientIP string, whitelist []string) bool {
	if len(whitelist) == 0 {
		return true
	}
	ip := net.ParseIP(clientIP)
	if ip == nil {
		return false
	}
	for _, entry := range whitelist {
		if strings.Contains(entry, "/") {
			_, netw, err := net.ParseCIDR(entry)
			if err == nil && netw.Contains(ip) {
				return true
			}
			continue
		}
		if parsed := net.ParseIP(entry); parsed != nil && parsed.Equal(ip) {
			return true
		}
	}
	return false
}

func generatePrefix() (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	return strings.ToLower(enc.EncodeToString(buf)), nil
}

func generateSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
