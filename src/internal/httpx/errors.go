// Package httpx contains shared HTTP error and response helpers.
package httpx

import (
	"fmt"
	"net/http"
)

// Error is an API-safe error with a stable code and HTTP status.
type Error struct {
	Status  int
	Code    string
	Message string
	Details map[string]any
	Cause   error
}

// Error implements error.
func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

// Unwrap exposes the underlying cause.
func (e *Error) Unwrap() error { return e.Cause }

// New builds an API error.
func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// WithDetails adds a detail key.
func (e *Error) WithDetails(key string, value any) *Error {
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	e.Details[key] = value
	return e
}

// WithCause attaches an internal cause that is logged but never returned.
func (e *Error) WithCause(err error) *Error {
	e.Cause = err
	return e
}

// BadRequest returns a 400 error.
func BadRequest(code, message string) *Error { return New(http.StatusBadRequest, code, message) }

// Unauthorized returns a 401 error.
func Unauthorized(message string) *Error {
	return New(http.StatusUnauthorized, "UNAUTHORIZED", message)
}

// Forbidden returns a 403 error.
func Forbidden(code, message string) *Error { return New(http.StatusForbidden, code, message) }

// NotFound returns a 404 error.
func NotFound(code, message string) *Error { return New(http.StatusNotFound, code, message) }

// Conflict returns a 409 error.
func Conflict(code, message string) *Error { return New(http.StatusConflict, code, message) }

// Unprocessable returns a 422 error.
func Unprocessable(code, message string) *Error {
	return New(http.StatusUnprocessableEntity, code, message)
}

// Internal returns a 500 error.
func Internal(err error) *Error {
	return New(http.StatusInternalServerError, "INTERNAL", "internal server error").WithCause(err)
}
