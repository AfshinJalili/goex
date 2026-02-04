package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const (
	ErrorCodeInvalidRequest      = "INVALID_REQUEST"
	ErrorCodeUnauthorized        = "UNAUTHORIZED"
	ErrorCodeForbidden           = "FORBIDDEN"
	ErrorCodeRateLimited         = "RATE_LIMITED"
	ErrorCodeInsufficientBalance = "INSUFFICIENT_BALANCE"
	ErrorCodeOrderNotFound       = "ORDER_NOT_FOUND"
	ErrorCodeSymbolHalted        = "SYMBOL_HALTED"
	ErrorCodeInternalError       = "INTERNAL_ERROR"
)

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func AssertErrorCode(t *testing.T, resp *httptest.ResponseRecorder, expectedCode string) {
	t.Helper()
	if resp.Code != getHTTPStatusForErrorCode(expectedCode) {
		t.Fatalf("expected status %d, got %d", getHTTPStatusForErrorCode(expectedCode), resp.Code)
	}

	var errResp errorResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}

	if errResp.Code != expectedCode {
		t.Fatalf("expected error code %q, got %q", expectedCode, errResp.Code)
	}
}

func AssertErrorMessage(t *testing.T, resp *httptest.ResponseRecorder, expectedMessage string) {
	t.Helper()
	var errResp errorResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}

	if errResp.Message != expectedMessage {
		t.Fatalf("expected error message %q, got %q", expectedMessage, errResp.Message)
	}
}

func AssertHTTPStatus(t *testing.T, resp *httptest.ResponseRecorder, expectedStatus int) {
	t.Helper()
	if resp.Code != expectedStatus {
		t.Fatalf("expected status %d, got %d", expectedStatus, resp.Code)
	}
}

func getHTTPStatusForErrorCode(code string) int {
	switch code {
	case ErrorCodeInvalidRequest:
		return http.StatusBadRequest
	case ErrorCodeUnauthorized:
		return http.StatusUnauthorized
	case ErrorCodeForbidden:
		return http.StatusForbidden
	case ErrorCodeRateLimited:
		return http.StatusTooManyRequests
	case ErrorCodeInsufficientBalance, ErrorCodeOrderNotFound, ErrorCodeSymbolHalted:
		return http.StatusBadRequest
	case ErrorCodeInternalError:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}
