package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// jsonContentType is the canonical content-type for JSON responses Snooze emits.
const jsonContentType = "application/json; charset=utf-8"

// WriteJSON sets the canonical content-type, writes status, then JSON-encodes
// body. A nil body short-circuits the encode step. Encoding errors after the
// status has been written cannot be recovered; we best-effort log via the
// response writer.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", jsonContentType)
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// Header already flushed — last-resort log via http.Error is not
		// usable; we can't recover. The caller's slog logger will catch the
		// half-written response on the next request via the recoverer.
		_, _ = io.WriteString(w, fmt.Sprintf(`{"error":{"code":"internal","message":%q}}`, err.Error()))
	}
}

// ParseJSONBody reads the request body into dst, returning a *Error on
// failure so callers can pass the error through to WriteError.
func ParseJSONBody(r *http.Request, dst any) error {
	if r == nil || r.Body == nil {
		return ErrBadRequest.WithMessage("empty body")
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return ErrBadRequest.WithCause(fmt.Errorf("read body: %w", err))
	}
	if len(raw) == 0 {
		return ErrBadRequest.WithMessage("empty body")
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return ErrBadRequest.WithCause(fmt.Errorf("decode body: %w", err))
	}
	return nil
}

// ParseJSONOrArray reads the request body and returns it decoded either as a
// single object or an array of objects, always producing []map[string]any.
// This matches the legacy /alert payload shape (single record or batch).
func ParseJSONOrArray(r *http.Request) ([]map[string]any, error) {
	if r == nil || r.Body == nil {
		return nil, ErrBadRequest.WithMessage("empty body")
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, ErrBadRequest.WithCause(fmt.Errorf("read body: %w", err))
	}
	if len(raw) == 0 {
		return nil, ErrBadRequest.WithMessage("empty body")
	}
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, nil
	}
	var one map[string]any
	if err := json.Unmarshal(raw, &one); err != nil {
		return nil, ErrBadRequest.WithCause(fmt.Errorf("decode body: %w", err))
	}
	return []map[string]any{one}, nil
}
