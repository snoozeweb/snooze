package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
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
		sub.Get("/apikeys", rt.handleListMyAPIKeys)
		sub.Post("/apikeys", rt.handleCreateMyAPIKey)
		sub.Delete("/apikeys/{id}", rt.handleDeleteMyAPIKey)
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

// apiKeyCreateRequest is the body for POST /api/v1/user/me/apikeys.
type apiKeyCreateRequest struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
	// ExpiresAt is RFC3339; omit/empty to default to the configured cap.
	ExpiresAt string `json:"expires_at"`
}

// requireSelf reads the verified Claims off the request context, refusing the
// request when the API-key store is not wired (503) or the caller is
// unauthenticated (401). The returned claims identify the caller whose keys the
// self-service routes operate on.
func (rt *Router) requireSelf(w http.ResponseWriter, r *http.Request) (snoozetypes.Claims, bool) {
	if rt.APIKeys == nil || rt.DB == nil {
		WriteError(w, r, ErrUnavailable.WithMessage("api keys not configured"))
		return snoozetypes.Claims{}, false
	}
	claims, ok := auth.ClaimsFrom(r.Context())
	if !ok || claims.Subject == "" {
		WriteError(w, r, ErrUnauthorized.WithMessage("authentication required"))
		return snoozetypes.Claims{}, false
	}
	return claims, true
}

// handleCreateMyAPIKey POST /api/v1/user/me/apikeys.
//
// Body: {name, permissions?, expires_at?}. Mints a key scoped to the caller,
// carrying a subset of the caller's own permissions; the raw key is returned
// exactly once (HTTP 201). A request authenticated with an API key may not mint
// further keys — minting requires an interactive session so the caller's live
// perms bound the grant.
func (rt *Router) handleCreateMyAPIKey(w http.ResponseWriter, r *http.Request) {
	claims, ok := rt.requireSelf(w, r)
	if !ok {
		return
	}
	if claims.Method == auth.APIKeyMethod {
		WriteError(w, r, ErrForbidden.WithMessage("API keys cannot create other API keys"))
		return
	}
	var body apiKeyCreateRequest
	if err := ParseJSONBody(r, &body); err != nil {
		WriteError(w, r, err)
		return
	}
	var expiresAt time.Time
	if body.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, body.ExpiresAt)
		if err != nil {
			WriteError(w, r, ErrValidation.WithMessage("expires_at must be RFC3339"))
			return
		}
		expiresAt = t
	}
	raw, doc, err := rt.APIKeys.Issue(r.Context(), claims, body.Name, body.Permissions, expiresAt)
	if err != nil {
		WriteError(w, r, mapAPIKeyError(err))
		return
	}
	doc["key"] = raw // shown exactly once
	WriteJSON(w, http.StatusCreated, doc)
}

// handleListMyAPIKeys GET /api/v1/user/me/apikeys lists the caller's own keys
// with key_hash stripped.
func (rt *Router) handleListMyAPIKeys(w http.ResponseWriter, r *http.Request) {
	claims, ok := rt.requireSelf(w, r)
	if !ok {
		return
	}
	keys, err := rt.APIKeys.ListByOwner(r.Context(), claims.Subject, claims.Method)
	if err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"data": keys})
}

// handleDeleteMyAPIKey DELETE /api/v1/user/me/apikeys/{id} deletes a key only
// when it is owned by the caller (404 otherwise).
func (rt *Router) handleDeleteMyAPIKey(w http.ResponseWriter, r *http.Request) {
	claims, ok := rt.requireSelf(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	deleted, err := rt.APIKeys.DeleteByID(r.Context(), claims.Subject, claims.Method, id)
	if err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	if !deleted {
		WriteError(w, r, ErrNotFound.WithMessage("api key not found"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// mapAPIKeyError translates store sentinels to API errors.
func mapAPIKeyError(err error) error {
	switch {
	case errors.Is(err, auth.ErrAPIKeyForbiddenPerm):
		return ErrForbidden.WithMessage(err.Error())
	case errors.Is(err, auth.ErrAPIKeyDuplicateName):
		return ErrConflict.WithMessage(err.Error())
	case errors.Is(err, auth.ErrAPIKeyExpiryTooFar), errors.Is(err, auth.ErrAPIKeyExpiryPast), errors.Is(err, auth.ErrAPIKeyNameRequired):
		return ErrValidation.WithMessage(err.Error())
	default:
		return ErrInternal.WithCause(err)
	}
}
