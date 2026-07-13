package platform

import "errors"

// ErrorClass is the stable internal routing class for application errors.
type ErrorClass string

const (
	ErrorClassValidation ErrorClass = "validation"
	ErrorClassAuth       ErrorClass = "auth"
	ErrorClassRateLimit  ErrorClass = "rate_limit"
	ErrorClassUpstream   ErrorClass = "upstream"
	ErrorClassTransport  ErrorClass = "transport"
	ErrorClassStorage    ErrorClass = "storage"
	ErrorClassConfig     ErrorClass = "config"
	ErrorClassInternal   ErrorClass = "internal"
)

// ErrorClasses returns the stable display/order table for route adapters.
func ErrorClasses() []ErrorClass {
	return []ErrorClass{
		ErrorClassValidation,
		ErrorClassAuth,
		ErrorClassRateLimit,
		ErrorClassUpstream,
		ErrorClassTransport,
		ErrorClassStorage,
		ErrorClassConfig,
		ErrorClassInternal,
	}
}

type StorageError struct{ *AppError }
type ConfigError struct{ *AppError }
type TransportError struct{ *AppError }

func NewStorageError(message string) *StorageError {
	return &StorageError{AppError: NewAppError(message, ErrorKindServer, "storage_error", 500, nil)}
}

func NewConfigError(message string) *ConfigError {
	return &ConfigError{AppError: NewAppError(message, ErrorKindServer, "config_error", 500, nil)}
}

func NewTransportError(message string) *TransportError {
	return &TransportError{AppError: NewAppError(message, ErrorKindUpstream, "transport_error", 502, nil)}
}

// ClassifyError maps rich errors to coarse classes; route adapters should format the result.
func ClassifyError(err error) ErrorClass {
	var validation *ValidationError
	if errors.As(err, &validation) {
		return ErrorClassValidation
	}
	var auth *AuthError
	if errors.As(err, &auth) {
		return ErrorClassAuth
	}
	var rateLimit *RateLimitError
	if errors.As(err, &rateLimit) {
		return ErrorClassRateLimit
	}
	var storage *StorageError
	if errors.As(err, &storage) {
		return ErrorClassStorage
	}
	var config *ConfigError
	if errors.As(err, &config) {
		return ErrorClassConfig
	}
	var transport *TransportError
	if errors.As(err, &transport) {
		return ErrorClassTransport
	}
	var upstream *UpstreamError
	if errors.As(err, &upstream) {
		return ErrorClassUpstream
	}
	return ErrorClassInternal
}
