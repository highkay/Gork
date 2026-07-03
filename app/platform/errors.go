package platform

import (
	"errors"
	"maps"
	"strconv"
	"strings"

	"github.com/dslzl/gork/app/platform/observability"
)

// ErrorKind is the OpenAI-compatible error type string.
type ErrorKind string

const (
	ErrorKindValidation     ErrorKind = "invalid_request_error"
	ErrorKindAuthentication ErrorKind = "authentication_error"
	ErrorKindRateLimit      ErrorKind = "rate_limit_exceeded"
	ErrorKindUpstream       ErrorKind = "upstream_error"
	ErrorKindServer         ErrorKind = "server_error"
)

// AppError is the base error for application failures.
type AppError struct {
	Message string
	Kind    ErrorKind
	Code    string
	Status  int
	Details map[string]any
}

// NewAppError creates an application error.
func NewAppError(message string, kind ErrorKind, code string, status int, details map[string]any) *AppError {
	if kind == "" {
		kind = ErrorKindServer
	}
	if code == "" {
		code = "internal_error"
	}
	if status == 0 {
		status = 500
	}
	if details == nil {
		details = map[string]any{}
	}
	return &AppError{
		Message: message,
		Kind:    kind,
		Code:    code,
		Status:  status,
		Details: details,
	}
}

func (e *AppError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// ToDict returns the OpenAI-compatible JSON error body.
func (e *AppError) ToDict() map[string]any {
	err := map[string]any{
		"message": e.Message,
		"type":    e.Kind,
		"code":    e.Code,
	}
	if param, ok := e.Details["param"]; ok {
		err["param"] = param
	}
	return map[string]any{"error": err}
}

// ValidationError represents invalid request data.
type ValidationError struct {
	*AppError
	Param string
}

// NewValidationError creates a validation error.
func NewValidationError(message, param, code string) *ValidationError {
	if code == "" {
		code = "invalid_value"
	}
	return &ValidationError{
		AppError: NewAppError(message, ErrorKindValidation, code, 400, map[string]any{"param": param}),
		Param:    param,
	}
}

// AuthError represents invalid or missing API credentials.
type AuthError struct {
	*AppError
}

// NewAuthError creates an authentication error.
func NewAuthError(message string) *AuthError {
	if message == "" {
		message = "Invalid or missing API key"
	}
	return &AuthError{AppError: NewAppError(message, ErrorKindAuthentication, "invalid_api_key", 401, nil)}
}

// RateLimitError represents unavailable account capacity.
type RateLimitError struct {
	*AppError
}

// NewRateLimitError creates a rate limit error.
func NewRateLimitError(message string) *RateLimitError {
	if message == "" {
		message = "No available accounts"
	}
	return &RateLimitError{AppError: NewAppError(message, ErrorKindRateLimit, "rate_limit_exceeded", 429, nil)}
}

// UpstreamError represents an upstream XAI/Grok failure.
type UpstreamError struct {
	*AppError
	Body    string
	Headers map[string]string
}

// NewUpstreamError creates an upstream error.
func NewUpstreamError(message string, status int, body string) *UpstreamError {
	return NewUpstreamErrorWithHeaders(message, status, body, nil)
}

// NewUpstreamErrorWithHeaders creates an upstream error with retry-safe headers.
func NewUpstreamErrorWithHeaders(message string, status int, body string, headers map[string]string) *UpstreamError {
	if status == 0 {
		status = 502
	}
	observability.RecordUpstreamError(observability.UpstreamError{
		Product:    "xai",
		StatusCode: status,
		Message:    message,
	})
	return &UpstreamError{
		AppError: NewAppError(message, ErrorKindUpstream, "upstream_error", status, map[string]any{"body": body}),
		Body:     body,
		Headers:  cloneStringMap(headers),
	}
}

// StreamIdleTimeout represents a streaming response idle timeout.
type StreamIdleTimeout struct {
	*AppError
	TimeoutSeconds float64
}

// NewStreamIdleTimeout creates a stream idle timeout error.
func NewStreamIdleTimeout(timeoutSeconds float64) *StreamIdleTimeout {
	value := strconv.FormatFloat(timeoutSeconds, 'f', -1, 64)
	return &StreamIdleTimeout{
		AppError:       NewAppError("Stream idle timeout after "+value+"s", ErrorKindUpstream, "stream_idle_timeout", 504, nil),
		TimeoutSeconds: timeoutSeconds,
	}
}

// ErrorResponse is the HTTP-ready shape shared by product routers.
type ErrorResponse struct {
	Status  int
	Class   ErrorClass
	Payload map[string]any
	Headers map[string]string
}

// AdaptErrorResponse maps internal errors to OpenAI-compatible HTTP errors.
func AdaptErrorResponse(err error) ErrorResponse {
	class := ClassifyError(err)
	var validation *ValidationError
	if errors.As(err, &validation) && validation.AppError != nil {
		return ErrorResponse{Status: validation.Status, Class: class, Payload: validation.ToDict()}
	}
	var upstream *UpstreamError
	if errors.As(err, &upstream) && upstream.AppError != nil {
		return ErrorResponse{
			Status:  upstream.Status,
			Class:   class,
			Payload: upstream.ToDict(),
			Headers: retryHeaderAllowlist(upstream.Headers),
		}
	}
	if appErr := embeddedAppError(err); appErr != nil {
		return ErrorResponse{Status: appErr.Status, Class: class, Payload: appErr.ToDict()}
	}
	var appErr *AppError
	if errors.As(err, &appErr) && appErr != nil {
		return ErrorResponse{Status: appErr.Status, Class: class, Payload: appErr.ToDict()}
	}
	return ErrorResponse{Status: 500, Class: class, Payload: map[string]any{
		"error": map[string]any{
			"message": err.Error(),
			"type":    ErrorKindServer,
			"code":    "internal_error",
		},
	}}
}

func embeddedAppError(err error) *AppError {
	var auth *AuthError
	if errors.As(err, &auth) && auth.AppError != nil {
		return auth.AppError
	}
	var rateLimit *RateLimitError
	if errors.As(err, &rateLimit) && rateLimit.AppError != nil {
		return rateLimit.AppError
	}
	var streamIdle *StreamIdleTimeout
	if errors.As(err, &streamIdle) && streamIdle.AppError != nil {
		return streamIdle.AppError
	}
	var storage *StorageError
	if errors.As(err, &storage) && storage.AppError != nil {
		return storage.AppError
	}
	var config *ConfigError
	if errors.As(err, &config) && config.AppError != nil {
		return config.AppError
	}
	var transport *TransportError
	if errors.As(err, &transport) && transport.AppError != nil {
		return transport.AppError
	}
	return nil
}

func retryHeaderAllowlist(headers map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range headers {
		canonical := canonicalRetryHeader(key)
		if canonical == "" || strings.TrimSpace(value) == "" {
			continue
		}
		out[canonical] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func canonicalRetryHeader(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "retry-after":
		return "Retry-After"
	case "x-ratelimit-limit":
		return "X-RateLimit-Limit"
	case "x-ratelimit-remaining":
		return "X-RateLimit-Remaining"
	case "x-ratelimit-reset":
		return "X-RateLimit-Reset"
	case "x-gork-retry-after":
		return "X-Gork-Retry-After"
	default:
		return ""
	}
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	return maps.Clone(values)
}
