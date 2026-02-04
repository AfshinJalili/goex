package security

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	Roles  []string `json:"roles"`
	Scopes []string `json:"scopes"`
	jwt.RegisteredClaims
}

func NewAccessToken(userID string, roles, scopes []string, secret []byte, ttl time.Duration, now time.Time) (string, error) {
	claims := Claims{
		Roles:  roles,
		Scopes: scopes,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}
