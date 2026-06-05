package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// loginRequest is the JSON body accepted by /login/local and /login/ldap.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Org      string `json:"org,omitempty"` // optional tenant slug (D10); omitted => DefaultTenant
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

// handleLoginIndex enumerates available auth backends. Providers that
// implement auth.EnableChecker and report IsEnabled(ctx)=false are filtered
// out — the login UI then renders one tab per surviving backend. The default
// backend (per general.default_auth_backend) is listed first when it survives.
func (rt *Router) handleLoginIndex(w http.ResponseWriter, r *http.Request) {
	if rt.Providers == nil {
		WriteJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"backends": []string{}}})
		return
	}
	all := rt.Providers.Names()
	names := make([]string, 0, len(all))
	for _, n := range all {
		p, err := rt.Providers.Get(n)
		if err != nil {
			continue
		}
		if !auth.ProviderEnabled(r.Context(), p) {
			continue
		}
		names = append(names, n)
	}
	// Surface the configured default first when it survived the filter.
	def := ""
	if rt.Config != nil {
		def = rt.Config.General.DefaultAuthBackend
	}
	if def != "" {
		ordered := make([]string, 0, len(names))
		seen := false
		for _, n := range names {
			if n == def {
				seen = true
			}
		}
		if seen {
			ordered = append(ordered, def)
		}
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
		org := body.Org
		if org == "" {
			org = snoozetypes.DefaultTenant
		}
		authCtx := auth.WithTenant(r.Context(), org)
		id, err := provider.Authenticate(authCtx, auth.Credentials{
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
		// Suspend check: look up the tenant registry under platform scope and
		// reject the login when the org is suspended.
		if err := rt.checkTenantStatus(r.Context(), org); err != nil {
			WriteError(w, r, err)
			return
		}
		// Ensure TenantID propagates even when provider doesn't set it.
		if id.TenantID == "" {
			id.TenantID = org
		}
		resp, err := rt.signSession(authCtx, id)
		if err != nil {
			WriteError(w, r, ErrInternal.WithCause(err))
			return
		}
		rt.updateLastLogin(authCtx, id)
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
	// Anonymous login may carry an optional "org" field (D10).
	var body loginRequest
	_ = ParseJSONBody(r, &body) // best-effort; empty body is valid
	org := body.Org
	if org == "" {
		org = snoozetypes.DefaultTenant
	}
	authCtx := auth.WithTenant(r.Context(), org)

	provider, err := rt.Providers.Get("anonymous")
	if err != nil {
		WriteError(w, r, ErrNotFound.WithMessage("anonymous backend disabled").WithCause(err))
		return
	}
	id, err := provider.Authenticate(authCtx, auth.Credentials{})
	if err != nil {
		if errors.Is(err, auth.ErrProviderDisabled) {
			WriteError(w, r, ErrConflict.WithMessage("backend disabled"))
			return
		}
		WriteError(w, r, ErrUnauthorized.WithMessage("anonymous login refused").WithCause(err))
		return
	}
	if id.TenantID == "" {
		id.TenantID = org
	}
	resp, err := rt.signSession(authCtx, id)
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
	// Mirror signSession: refresh of an anonymous token preserves admin rights
	// when general.anonymous_admin is on.
	if claims.Method == auth.AnonymousMethod && rt.Config != nil && rt.Config.General.AnonymousAdmin {
		claims.Roles = []string{"admin"}
		claims.Permissions = []string{auth.AllPermission}
	}
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
	tenantID := id.TenantID
	if tenantID == "" {
		tenantID = snoozetypes.DefaultTenant
	}
	claims := snoozetypes.Claims{
		Subject:  id.Username,
		Method:   id.Method,
		TenantID: tenantID,
		Groups:   id.Groups,
	}
	rt.resolveRoles(ctx, &claims)
	// Per-deploy override: anonymous_admin grants every anonymous login the
	// "admin" role + the AllPermission wildcard. Used by try / demo
	// environments where every visitor needs full access.
	if id.Method == auth.AnonymousMethod && rt.Config != nil && rt.Config.General.AnonymousAdmin {
		claims.Roles = []string{"admin"}
		claims.Permissions = []string{auth.AllPermission}
	}
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
// record. The ctx passed to Resolve carries the tenant so the driver (Phase 3)
// injects the tenant predicate automatically. Failures fall through silently.
func (rt *Router) resolveRoles(ctx context.Context, claims *snoozetypes.Claims) {
	if rt.DB == nil {
		return
	}
	tenantID := claims.TenantID
	if tenantID == "" {
		tenantID = snoozetypes.DefaultTenant
	}
	tctx := auth.WithTenant(ctx, tenantID)
	resolver := auth.NewRoleResolver(rt.DB)
	roles, perms, err := resolver.Resolve(tctx, auth.Identity{
		Username: claims.Subject,
		Method:   claims.Method,
		TenantID: tenantID,
		Groups:   claims.Groups,
	})
	if err != nil {
		return
	}
	claims.Roles = roles
	claims.Permissions = perms
}

// checkTenantStatus fetches the tenant document (under platform scope) and
// returns an error if the tenant is suspended or does not exist. A nil DB or
// missing tenant doc is treated as "active" for forward compatibility — the
// tenant collection may not exist in older installs being migrated.
func (rt *Router) checkTenantStatus(ctx context.Context, tenantID string) error {
	if rt.DB == nil {
		return nil
	}
	pCtx := auth.WithPlatformScope(ctx)
	doc, err := rt.DB.GetOne(pCtx, "tenant", db.Document{"id": tenantID})
	if err != nil || doc == nil {
		// No registry entry = treat as active (pre-multitenancy or default tenant).
		return nil
	}
	status, _ := doc["status"].(string)
	if status == "suspended" {
		return ErrForbidden.WithMessage("tenant is suspended")
	}
	return nil
}
