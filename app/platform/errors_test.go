package platform

import (
	"testing"

	"github.com/dslzl/gork/app/platform/observability"
)

func TestNewUpstreamErrorRecordsObservabilitySummary(t *testing.T) {
	observability.ResetForTest()
	_ = NewUpstreamError("bad upstream", 503, "body")

	recent := observability.RecentUpstreamErrors(1)
	if len(recent) != 1 || recent[0].StatusCode != 503 || recent[0].Message != "bad upstream" {
		t.Fatalf("recent upstream errors = %#v", recent)
	}
}

func TestAppErrorToDictMatchesOpenAIErrorShape(t *testing.T) {
	err := NewAppError("bad input", ErrorKindValidation, "invalid_value", 400, map[string]any{"param": "model"})
	body := err.ToDict()
	errorBody := body["error"].(map[string]any)

	if errorBody["message"] != "bad input" {
		t.Fatalf("message = %#v, want bad input", errorBody["message"])
	}
	if errorBody["type"] != ErrorKindValidation {
		t.Fatalf("type = %#v, want %s", errorBody["type"], ErrorKindValidation)
	}
	if errorBody["code"] != "invalid_value" {
		t.Fatalf("code = %#v, want invalid_value", errorBody["code"])
	}
	if errorBody["param"] != "model" {
		t.Fatalf("param = %#v, want model", errorBody["param"])
	}
	if err.Status != 400 {
		t.Fatalf("status = %d, want 400", err.Status)
	}
}

func TestSpecificAppErrorsUsePythonDefaults(t *testing.T) {
	cases := []struct {
		name    string
		err     *AppError
		message string
		kind    ErrorKind
		code    string
		status  int
	}{
		{
			name:    "validation",
			err:     NewValidationError("missing model", "model", "invalid_value").AppError,
			message: "missing model",
			kind:    ErrorKindValidation,
			code:    "invalid_value",
			status:  400,
		},
		{
			name:    "auth",
			err:     NewAuthError("").AppError,
			message: "Invalid or missing API key",
			kind:    ErrorKindAuthentication,
			code:    "invalid_api_key",
			status:  401,
		},
		{
			name:    "rate_limit",
			err:     NewRateLimitError("").AppError,
			message: "No available accounts",
			kind:    ErrorKindRateLimit,
			code:    "rate_limit_exceeded",
			status:  429,
		},
		{
			name:    "upstream",
			err:     NewUpstreamError("bad upstream", 503, "body").AppError,
			message: "bad upstream",
			kind:    ErrorKindUpstream,
			code:    "upstream_error",
			status:  503,
		},
		{
			name:    "stream idle",
			err:     NewStreamIdleTimeout(12.5).AppError,
			message: "Stream idle timeout after 12.5s",
			kind:    ErrorKindUpstream,
			code:    "stream_idle_timeout",
			status:  504,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Message != tt.message || tt.err.Kind != tt.kind || tt.err.Code != tt.code || tt.err.Status != tt.status {
				t.Fatalf("error = %#v, want message=%q kind=%s code=%s status=%d", tt.err, tt.message, tt.kind, tt.code, tt.status)
			}
		})
	}
}

func TestValidationErrorParamOnlyAppearsWhenProvided(t *testing.T) {
	withoutParam := NewValidationError("bad", "", "invalid_value").ToDict()["error"].(map[string]any)
	if _, ok := withoutParam["param"]; !ok {
		t.Fatalf("Python-compatible shape keeps empty param when details include param")
	}

	serverErr := NewAppError("server", ErrorKindServer, "internal_error", 500, nil).ToDict()["error"].(map[string]any)
	if _, ok := serverErr["param"]; ok {
		t.Fatalf("AppError without param details should not include param")
	}
}

func TestAdaptErrorResponseMapsStatusPayloadAndRetryHeaders(t *testing.T) {
	upstream := NewUpstreamErrorWithHeaders("busy", 503, "body", map[string]string{
		"Retry-After":             "12",
		"X-RateLimit-Reset":       "12345",
		"X-Internal-Debug-Secret": "do-not-pass",
	})

	adapted := AdaptErrorResponse(upstream)
	if adapted.Status != 503 {
		t.Fatalf("status=%d", adapted.Status)
	}
	errBody := adapted.Payload["error"].(map[string]any)
	if errBody["type"] != ErrorKindUpstream || errBody["code"] != "upstream_error" || errBody["message"] != "busy" {
		t.Fatalf("payload=%#v", adapted.Payload)
	}
	if adapted.Headers["Retry-After"] != "12" || adapted.Headers["X-RateLimit-Reset"] != "12345" {
		t.Fatalf("headers=%#v", adapted.Headers)
	}
	if _, ok := adapted.Headers["X-Internal-Debug-Secret"]; ok {
		t.Fatalf("unexpected private header pass-through: %#v", adapted.Headers)
	}

	fallback := AdaptErrorResponse(assertionError("plain"))
	if fallback.Status != 500 || fallback.Payload["error"].(map[string]any)["code"] != "internal_error" {
		t.Fatalf("fallback=%#v", fallback)
	}
}

type assertionError string

func (e assertionError) Error() string { return string(e) }
