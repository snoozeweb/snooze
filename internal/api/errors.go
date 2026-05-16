// Package api implements the Snooze HTTP server: chi-based router, middleware
// chain, error envelope, login routes, static-asset serving, and the unix-socket
// admin endpoint. Concrete CRUD per plugin is mounted via plugins.MountCRUD;
// this package wires the framework around it.
package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/snoozeweb/snooze/internal/telemetry"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// Error is the typed error returned by handlers; WriteError converts it
// into the canonical ErrEnvelope wire shape. Status is the HTTP status code
// and Code is the stable machine-readable identifier in the envelope.
type Error struct {
	Code    string
	Status  int
	Message string
	Details map[string]any
	Cause   error
}

// Error implements the error interface; the cause, when present, is appended
// after a colon so logs preserve the wrap chain.
func (e *Error) Error() string {
	if e == nil {
		return "<nil api error>"
	}
	if e.Cause != nil {
		if e.Message != "" {
			return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
		}
		return fmt.Sprintf("%s: %v", e.Code, e.Cause)
	}
	if e.Message != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return e.Code
}

// Unwrap returns the wrapped cause for errors.Is / errors.As traversal.
func (e *Error) Unwrap() error { return e.Cause }

// Sentinel errors covering the common HTTP failure modes. WriteError checks
// errors.As for *Error so wrapping these via WithCause/WithDetails/etc. is
// fully supported.
var (
	// ErrBadRequest indicates malformed input the client should not retry.
	ErrBadRequest = &Error{Code: "bad_request", Status: http.StatusBadRequest}
	// ErrUnauthorized indicates missing or invalid credentials.
	ErrUnauthorized = &Error{Code: "unauthorized", Status: http.StatusUnauthorized}
	// ErrForbidden indicates the credentials are valid but lack permission.
	ErrForbidden = &Error{Code: "forbidden", Status: http.StatusForbidden}
	// ErrNotFound indicates the addressed resource does not exist.
	ErrNotFound = &Error{Code: "not_found", Status: http.StatusNotFound}
	// ErrConflict indicates a duplicate-key or state conflict.
	ErrConflict = &Error{Code: "conflict", Status: http.StatusConflict}
	// ErrValidation indicates the body parsed but failed semantic validation.
	ErrValidation = &Error{Code: "validation_error", Status: http.StatusUnprocessableEntity}
	// ErrInternal is the catch-all for unexpected failures.
	ErrInternal = &Error{Code: "internal", Status: http.StatusInternalServerError}
	// ErrUnavailable indicates the service is not ready to handle the request.
	ErrUnavailable = &Error{Code: "unavailable", Status: http.StatusServiceUnavailable}
)

// WithMessage returns a copy of e with the human-readable Message set. The
// receiver is left untouched so the package-level sentinels remain reusable.
func (e *Error) WithMessage(msg string) *Error {
	if e == nil {
		return nil
	}
	cp := *e
	cp.Message = msg
	return &cp
}

// WithCause returns a copy of e wrapping err. The original sentinel is
// preserved for errors.Is comparisons via the chain.
func (e *Error) WithCause(err error) *Error {
	if e == nil {
		return nil
	}
	cp := *e
	cp.Cause = err
	return &cp
}

// WithDetails returns a copy of e with structured details attached. The
// details map is not copied; callers should not mutate it after the call.
func (e *Error) WithDetails(details map[string]any) *Error {
	if e == nil {
		return nil
	}
	cp := *e
	cp.Details = details
	return &cp
}

// WriteError emits the canonical ErrEnvelope. When err is not a *Error it
// is wrapped under ErrInternal. The request_id and trace_id context fields
// are injected when present so log/UI correlation works out of the box.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	apiErr := asError(err)
	envelope := snoozetypes.ErrEnvelope{
		Error: snoozetypes.ErrBody{
			Code:    apiErr.Code,
			Message: apiErr.Message,
			Details: apiErr.Details,
		},
	}
	if r != nil {
		if id := telemetry.RequestIDFrom(r.Context()); id != "" {
			envelope.Error.RequestID = id
		}
		if tid := telemetry.TraceIDFrom(r.Context()); tid != "" {
			envelope.Error.TraceID = tid
		}
	}
	if envelope.Error.Message == "" {
		envelope.Error.Message = defaultMessage(apiErr.Code)
	}
	WriteJSON(w, apiErr.Status, envelope)
}

// asError extracts (or fabricates) a non-nil *Error describing err.
func asError(err error) *Error {
	if err == nil {
		return ErrInternal
	}
	var apiErr *Error
	if errors.As(err, &apiErr) && apiErr != nil {
		return apiErr
	}
	return ErrInternal.WithCause(err)
}

// defaultMessage returns a human-friendly default for the well-known codes.
func defaultMessage(code string) string {
	switch code {
	case "bad_request":
		return "bad request"
	case "unauthorized":
		return "authentication required"
	case "forbidden":
		return "permission denied"
	case "not_found":
		return "resource not found"
	case "conflict":
		return "resource conflict"
	case "validation_error":
		return "validation failed"
	case "unavailable":
		return "service unavailable"
	default:
		return "internal server error"
	}
}
