package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const ContextUserIDKey = "user_id"

func Middleware(secret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := ExtractBearer(c.GetHeader("Authorization"))
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "missing token"})
			return
		}

		claims, err := ParseJWT(token, secret)
		if err != nil || claims.Subject == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "invalid token"})
			return
		}

		c.Set(ContextUserIDKey, claims.Subject)
		c.Next()
	}
}
