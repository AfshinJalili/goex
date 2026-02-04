package testutil

import (
	"time"

	"github.com/AfshinJalili/goex/libs/apikey"
	"github.com/AfshinJalili/goex/libs/auth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	DemoUserID   = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	TraderUserID = uuid.MustParse("00000000-0000-0000-0000-000000000002")
)

func GenerateJWT(userID uuid.UUID, secret []byte, ttl time.Duration, now time.Time) (string, error) {
	claims := auth.Claims{
		Roles:  []string{"user"},
		Scopes: []string{"read"},
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "cex-auth",
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

func GenerateAPIKey(env string) (string, string, string, error) {
	return apikey.Generate(env)
}
