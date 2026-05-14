package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/japannext/snooze/internal/telemetry"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

func TestAPIError_Error(t *testing.T) {
	e := ErrNotFound.WithMessage("user 42")
	require.Contains(t, e.Error(), "not_found")
	require.Contains(t, e.Error(), "user 42")

	wrapped := ErrInternal.WithCause(errors.New("disk full"))
	require.Contains(t, wrapped.Error(), "disk full")
	require.ErrorIs(t, wrapped, wrapped.Cause)
}

func TestAPIError_Unwrap(t *testing.T) {
	cause := fmt.Errorf("base")
	e := ErrBadRequest.WithCause(cause)
	require.ErrorIs(t, e, cause)
}

func TestWriteError_KnownError(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil).
		WithContext(telemetry.WithRequestID(httptest.NewRequest(http.MethodGet, "/x", nil).Context(), "rid-1"))

	WriteError(rec, req, ErrConflict.WithMessage("dup"))
	require.Equal(t, http.StatusConflict, rec.Code)

	var env snoozetypes.ErrEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.Equal(t, "conflict", env.Error.Code)
	require.Equal(t, "dup", env.Error.Message)
	require.Equal(t, "rid-1", env.Error.RequestID)
}

func TestWriteError_UnknownErrorFallsBackToInternal(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	WriteError(rec, req, errors.New("boom"))
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	var env snoozetypes.ErrEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.Equal(t, "internal", env.Error.Code)
}

func TestWriteError_SentinelCoverage(t *testing.T) {
	cases := []struct {
		e    *APIError
		want int
	}{
		{ErrBadRequest, http.StatusBadRequest},
		{ErrUnauthorized, http.StatusUnauthorized},
		{ErrForbidden, http.StatusForbidden},
		{ErrNotFound, http.StatusNotFound},
		{ErrConflict, http.StatusConflict},
		{ErrValidation, http.StatusUnprocessableEntity},
		{ErrInternal, http.StatusInternalServerError},
		{ErrUnavailable, http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.e.Code, func(t *testing.T) {
			rec := httptest.NewRecorder()
			WriteError(rec, httptest.NewRequest(http.MethodGet, "/", nil), tc.e)
			require.Equal(t, tc.want, rec.Code)
			var env snoozetypes.ErrEnvelope
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
			require.Equal(t, tc.e.Code, env.Error.Code)
			require.NotEmpty(t, env.Error.Message)
		})
	}
}

func TestAPIError_WithDetails(t *testing.T) {
	e := ErrValidation.WithDetails(map[string]any{"field": "email"})
	rec := httptest.NewRecorder()
	WriteError(rec, httptest.NewRequest(http.MethodGet, "/", nil), e)
	var env snoozetypes.ErrEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.Equal(t, "email", env.Error.Details["field"])
}
