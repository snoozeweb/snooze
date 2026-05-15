package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/japannext/snooze/internal/auth"
	"github.com/japannext/snooze/internal/condition"
	"github.com/japannext/snooze/internal/db"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

// loginRequest is the JSON body accepted by /login/local and /login/ldap.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// loginResponse is the wire shape returned on a successful login.
type loginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	Method    string    `json:"method"`
}

// mountLogin wires the public /api/v1/login/* endpoints.
//
// We expose:
//
//	POST /api/v1/login/local
//	POST /api/v1/login/ldap
//	POST /api/v1/login/anonymous   (only when configured)
//	GET  /api/v1/login              (lists enabled backends)
func (rt *Router) mountLogin(r chi.Router) {
	r.Route("/api/v1/login", func(sub chi.Router) {
		sub.Get("/", rt.handleLoginIndex)
		sub.Post("/local", rt.handleLogin("local"))
		sub.Post("/ldap", rt.handleLogin("ldap"))
		sub.Post("/anonymous", rt.handleLoginAnonymous)
	})
}

// handleLoginIndex enumerates available auth backends. The default backend
// (per general.default_auth_backend) is listed first.
func (rt *Router) handleLoginIndex(w http.ResponseWriter, r *http.Request) {
	if rt.Providers == nil {
		WriteJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"backends": []string{}}})
		return
	}
	names := rt.Providers.Names()
	// Surface the configured default first.
	def := ""
	if rt.Config != nil {
		def = rt.Config.General.DefaultAuthBackend
	}
	if def != "" {
		ordered := make([]string, 0, len(names))
		ordered = append(ordered, def)
		for _, n := range names {
			if n != def {
				ordered = append(ordered, n)
			}
		}
		names = ordered
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"backends": names},
	})
}

// handleLogin runs the username/password flow against the registered
// provider with the given method. The error message is identical for every
// credential-failure path (constant-time comparison via subtle.ConstantTimeEq).
func (rt *Router) handleLogin(method string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if rt.Providers == nil || rt.Auth == nil {
			WriteError(w, r, ErrUnavailable.WithMessage("auth not configured"))
			return
		}
		var body loginRequest
		if err := ParseJSONBody(r, &body); err != nil {
			WriteError(w, r, err)
			return
		}
		provider, err := rt.Providers.Get(method)
		if err != nil {
			WriteError(w, r, ErrNotFound.WithMessage("unknown auth backend").WithCause(err))
			return
		}
		id, err := provider.Authenticate(r.Context(), auth.Credentials{
			Username: body.Username,
			Password: body.Password,
		})
		if err != nil {
			if errors.Is(err, auth.ErrProviderDisabled) {
				WriteError(w, r, ErrConflict.WithMessage("backend disabled"))
				return
			}
			// Single canonical failure message across the "no user" and
			// "wrong password" branches so a timing-cheap caller cannot
			// distinguish them. The underlying Provider also uses
			// constant-time bcrypt comparison.
			WriteError(w, r, ErrUnauthorized.WithMessage("invalid credentials"))
			return
		}
		token, exp, err := rt.signFor(id)
		if err != nil {
			WriteError(w, r, ErrInternal.WithCause(err))
			return
		}
		rt.updateLastLogin(r.Context(), id)
		WriteJSON(w, http.StatusOK, loginResponse{Token: token, ExpiresAt: exp, Method: id.Method})
	}
}

// updateLastLogin best-effort writes the current epoch into the matching
// user record so the admin UI can show a recency badge. Failures are
// logged and ignored — they must never break a successful authentication.
//
// The user record is keyed by (name, method) since the same username can
// exist independently in local and ldap backends. For ldap-only users the
// row may not exist yet; we don't auto-create it (admins still control
// the user list).
func (rt *Router) updateLastLogin(ctx context.Context, id auth.Identity) {
	if rt.DB == nil {
		return
	}
	cond := condition.And(
		condition.Equals("name", id.Username),
		condition.Equals("method", id.Method),
	)
	docs, _, err := rt.DB.Search(ctx, "user", cond, db.Page{PerPage: 1})
	if err != nil || len(docs) == 0 {
		return
	}
	uid, _ := docs[0]["uid"].(string)
	if uid == "" {
		return
	}
	_ = rt.DB.UpdateOne(ctx, "user", uid, db.Document{
		"last_login": float64(time.Now().Unix()),
	}, false)
}

// handleLoginAnonymous is split out because it doesn't require credentials.
func (rt *Router) handleLoginAnonymous(w http.ResponseWriter, r *http.Request) {
	if rt.Providers == nil || rt.Auth == nil {
		WriteError(w, r, ErrUnavailable.WithMessage("auth not configured"))
		return
	}
	provider, err := rt.Providers.Get("anonymous")
	if err != nil {
		WriteError(w, r, ErrNotFound.WithMessage("anonymous backend disabled").WithCause(err))
		return
	}
	id, err := provider.Authenticate(r.Context(), auth.Credentials{})
	if err != nil {
		if errors.Is(err, auth.ErrProviderDisabled) {
			WriteError(w, r, ErrConflict.WithMessage("backend disabled"))
			return
		}
		WriteError(w, r, ErrUnauthorized.WithMessage("anonymous login refused").WithCause(err))
		return
	}
	token, exp, err := rt.signFor(id)
	if err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	WriteJSON(w, http.StatusOK, loginResponse{Token: token, ExpiresAt: exp, Method: id.Method})
}

// signFor expands id into the canonical Claims (subject + method + groups +
// roles + permissions resolved if a resolver is attached) and signs them.
func (rt *Router) signFor(id auth.Identity) (string, time.Time, error) {
	claims := snoozetypes.Claims{
		Subject: id.Username,
		Method:  id.Method,
		Groups:  id.Groups,
	}
	if rt.DB != nil {
		// Best-effort RBAC resolution. Failures fall through with empty
		// roles/permissions; a malformed role collection should not break
		// login outright.
		resolver := auth.NewRoleResolver(rt.DB)
		if roles, perms, err := resolver.Resolve(context.Background(), id); err == nil {
			claims.Roles = roles
			claims.Permissions = perms
		}
	}
	return rt.Auth.Sign(claims)
}
