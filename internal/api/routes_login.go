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

// refreshRequest is the JSON body accepted by /login/refresh and /login/logout.
type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// loginResponse is the wire shape returned on a successful login or refresh.
type loginResponse struct {
	Token            string    `json:"token"`
	ExpiresAt        time.Time `json:"expires_at"`
	RefreshToken     string    `json:"refresh_token,omitempty"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at,omitempty"`
	Method           string    `json:"method"`
}

// refreshIssuer is the narrow surface the login routes need from the refresh
// token store. The concrete type is *auth.RefreshTokenStore; isolating the
// dependency behind an interface lets the route tests inject a stub without
// pulling in a full DB driver.
type refreshIssuer interface {
	Issue(ctx context.Context, c snoozetypes.Claims) (string, time.Time, error)
	VerifyAndRotate(ctx context.Context, raw string) (snoozetypes.Claims, string, time.Time, error)
	Revoke(ctx context.Context, raw string) error
}

// mountLogin wires the public /api/v1/login/* endpoints.
//
// We expose:
//
//	POST /api/v1/login/local
//	POST /api/v1/login/ldap
//	POST /api/v1/login/anonymous   (only when configured)
//	POST /api/v1/login/refresh     (exchange a refresh token for a new pair)
//	POST /api/v1/login/logout      (revoke a refresh token)
//	GET  /api/v1/login              (lists enabled backends)
func (rt *Router) mountLogin(r chi.Router) {
	r.Route("/api/v1/login", func(sub chi.Router) {
		sub.Get("/", rt.handleLoginIndex)
		sub.Post("/local", rt.handleLogin("local"))
		sub.Post("/ldap", rt.handleLogin("ldap"))
		sub.Post("/anonymous", rt.handleLoginAnonymous)
		sub.Post("/refresh", rt.handleRefresh)
		sub.Post("/logout", rt.handleLogout)
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
		resp, err := rt.signSession(r.Context(), id)
		if err != nil {
			WriteError(w, r, ErrInternal.WithCause(err))
			return
		}
		rt.updateLastLogin(r.Context(), id)
		WriteJSON(w, http.StatusOK, resp)
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
	resp, err := rt.signSession(r.Context(), id)
	if err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}

// handleRefresh exchanges a refresh token for a new access+refresh pair.
// Rotation is enforced server-side: the supplied token is revoked atomically
// before the new pair is minted. An invalid, revoked, expired or unknown
// token returns 401 with a generic message — callers cannot tell the
// failure modes apart, mirroring how /login responds to bad credentials.
func (rt *Router) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if rt.Auth == nil || rt.Refresh == nil {
		WriteError(w, r, ErrUnavailable.WithMessage("auth not configured"))
		return
	}
	var body refreshRequest
	if err := ParseJSONBody(r, &body); err != nil {
		WriteError(w, r, err)
		return
	}
	if body.RefreshToken == "" {
		WriteError(w, r, ErrUnauthorized.WithMessage("invalid refresh token"))
		return
	}
	claims, newRefresh, refreshExp, err := rt.Refresh.VerifyAndRotate(r.Context(), body.RefreshToken)
	if err != nil {
		WriteError(w, r, ErrUnauthorized.WithMessage("invalid refresh token").WithCause(err))
		return
	}
	// Re-resolve roles+permissions so a recent admin change takes effect on
	// the next access-token rotation (max staleness: the access-token lease).
	rt.resolveRoles(r.Context(), &claims)
	token, exp, err := rt.Auth.Sign(claims)
	if err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	WriteJSON(w, http.StatusOK, loginResponse{
		Token:            token,
		ExpiresAt:        exp,
		RefreshToken:     newRefresh,
		RefreshExpiresAt: refreshExp,
		Method:           claims.Method,
	})
}

// handleLogout revokes the supplied refresh token. The endpoint is always a
// 204 — unknown / already-revoked tokens are not surfaced as errors so a
// client cleaning up state never sees a 500 from a stale token.
func (rt *Router) handleLogout(w http.ResponseWriter, r *http.Request) {
	if rt.Refresh == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var body refreshRequest
	if err := ParseJSONBody(r, &body); err != nil {
		// Malformed JSON still resolves to "no-op logout".
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if body.RefreshToken != "" {
		_ = rt.Refresh.Revoke(r.Context(), body.RefreshToken)
	}
	w.WriteHeader(http.StatusNoContent)
}

// signSession mints the (access token, refresh token) pair returned to the
// client on a successful login. The refresh token is omitted when no store
// is wired (tests, single-binary deployments without DB-backed sessions).
func (rt *Router) signSession(ctx context.Context, id auth.Identity) (loginResponse, error) {
	claims := snoozetypes.Claims{
		Subject: id.Username,
		Method:  id.Method,
		Groups:  id.Groups,
	}
	rt.resolveRoles(ctx, &claims)
	token, exp, err := rt.Auth.Sign(claims)
	if err != nil {
		return loginResponse{}, err
	}
	resp := loginResponse{Token: token, ExpiresAt: exp, Method: id.Method}
	if rt.Refresh != nil {
		refresh, refreshExp, err := rt.Refresh.Issue(ctx, claims)
		if err != nil {
			return loginResponse{}, err
		}
		resp.RefreshToken = refresh
		resp.RefreshExpiresAt = refreshExp
	}
	return resp, nil
}

// resolveRoles populates claims.Roles and claims.Permissions from the user
// record. Failures fall through silently — a malformed role collection
// should not break login outright.
func (rt *Router) resolveRoles(ctx context.Context, claims *snoozetypes.Claims) {
	if rt.DB == nil {
		return
	}
	resolver := auth.NewRoleResolver(rt.DB)
	roles, perms, err := resolver.Resolve(ctx, auth.Identity{
		Username: claims.Subject,
		Method:   claims.Method,
		Groups:   claims.Groups,
	})
	if err != nil {
		return
	}
	claims.Roles = roles
	claims.Permissions = perms
}

// signFor expands id into the canonical Claims (subject + method + groups +
// roles + permissions resolved if a resolver is attached) and signs them.
//
// Deprecated: kept as a thin wrapper around signSession for callers that
// only need the access-token half of the pair.
func (rt *Router) signFor(id auth.Identity) (string, time.Time, error) {
	resp, err := rt.signSession(context.Background(), id)
	if err != nil {
		return "", time.Time{}, err
	}
	return resp.Token, resp.ExpiresAt, nil
}
