package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/db"
)

// passwordChangeRequest is the body accepted by POST /api/v1/user/me/password.
//
// Both fields are required: the caller proves they still control the session
// by re-supplying their current password before we accept a replacement. The
// server bcrypt-compares it against the stored hash; matching the
// /login/local flow keeps the verification path identical (constant-time
// compare, same provider).
type passwordChangeRequest struct {
	CurrentPassword string `json:"current_password"`
	Password        string `json:"password"`
}

// mountUser wires the authenticated /api/v1/user/me/* surface — currently just
// the self-service password endpoint. The path lives under /api/v1/user so the
// existing Auth middleware (which requires a Bearer token outside the public
// allowlist) covers it without extra plumbing; the per-route check then enforces
// that the caller has `any` permission and that the method is local.
func (rt *Router) mountUser(r chi.Router) {
	r.Route("/api/v1/user/me", func(sub chi.Router) {
		sub.Post("/password", rt.handleSelfPasswordChange)
	})
}

// handleSelfPasswordChange POST /api/v1/user/me/password.
//
// Body: {current_password, password}.
//
// The endpoint:
//
//  1. Reads the verified Claims off the request context (no claims = 401).
//  2. Refuses non-local methods — LDAP/anonymous passwords are not stored
//     here, so changing them via this route is meaningless.
//  3. Re-verifies current_password by going through LocalProvider.Authenticate
//     (constant-time bcrypt compare, single failure message).
//  4. Hashes the new password with bcrypt.DefaultCost and writes it onto the
//     user document.
func (rt *Router) handleSelfPasswordChange(w http.ResponseWriter, r *http.Request) {
	if rt.DB == nil {
		WriteError(w, r, ErrUnavailable.WithMessage("database not configured"))
		return
	}
	claims, ok := auth.ClaimsFrom(r.Context())
	if !ok || claims.Subject == "" {
		WriteError(w, r, ErrUnauthorized.WithMessage("authentication required"))
		return
	}
	if claims.Method != auth.LocalMethod {
		WriteError(w, r, ErrForbidden.WithMessage("self-service password change is only available for local accounts"))
		return
	}

	var body passwordChangeRequest
	if err := ParseJSONBody(r, &body); err != nil {
		WriteError(w, r, err)
		return
	}
	if body.Password == "" {
		WriteError(w, r, ErrValidation.WithMessage("password must not be empty"))
		return
	}
	if body.CurrentPassword == "" {
		WriteError(w, r, ErrValidation.WithMessage("current_password must not be empty"))
		return
	}

	// Verify the current password through the local provider so the bcrypt
	// compare path is identical to /login/local — same constant-time
	// behaviour, same failure mode.
	provider := auth.NewLocalProvider(rt.DB)
	if _, err := provider.Authenticate(r.Context(), auth.Credentials{
		Username: claims.Subject,
		Password: body.CurrentPassword,
	}); err != nil {
		WriteError(w, r, ErrUnauthorized.WithMessage("invalid current password"))
		return
	}

	// Locate the user document so we can target the UpdateOne by uid; the
	// (name, method) primary key is enforced at insert time, but PATCHing
	// by uid is the canonical update path.
	user, err := rt.DB.GetOne(r.Context(), auth.LocalCollection, db.Document{
		"name":   claims.Subject,
		"method": auth.LocalMethod,
	})
	if err != nil || user == nil {
		WriteError(w, r, ErrNotFound.WithMessage("user not found"))
		return
	}
	uid, _ := user["uid"].(string)
	if uid == "" {
		WriteError(w, r, ErrInternal.WithMessage("user document missing uid"))
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	if err := rt.DB.UpdateOne(r.Context(), auth.LocalCollection, uid, db.Document{
		"password": string(hash),
	}, true); err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
