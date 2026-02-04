package testutil

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
)

func MakeAuthRequest(router *gin.Engine, method, path string, body any, token string) *httptest.ResponseRecorder {
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func MakeAPIRequest(router *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	return MakeAuthRequest(router, method, path, body, "")
}
