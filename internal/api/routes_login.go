package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/oauth2"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// backendInfo is one entry in the GET /api/v1/login backends list. kind is
// "password" (local/ldap/anonymous — credential POST or one-click) or
// "redirect" (OIDC/OAuth — handled by /start + /callback).
type backendInfo struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	DisplayName string `json:"display_name,omitempty"`
	Icon        string `json:"icon,omitempty"`
}

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
//	GET  /api/v1/login                    (lists enabled backends + active/listed tenants)
//	GET  /api/v1/login/tenant?key=<key>   (resolve an opaque login key to tenant {id, display_name})
//	POST /api/v1/login/local
//	POST /api/v1/login/ldap
//	POST /api/v1/login/anonymous   (only when configured)
//	POST /api/v1/login/refresh     (exchange a refresh token for a new pair)
//	POST /api/v1/login/logout      (revoke a refresh token)
func (rt *Router) mountLogin(r chi.Router) {
	r.Route("/api/v1/login", func(sub chi.Router) {
		sub.Get("/", rt.handleLoginIndex)
		sub.Get("/tenant", rt.handleLoginResolveTenant)
		sub.Post("/local", rt.handleLogin("local"))
		sub.Post("/ldap", rt.handleLogin("ldap"))
		sub.Post("/anonymous", rt.handleLoginAnonymous)
		sub.Post("/refresh", rt.handleRefresh)
		sub.Post("/logout", rt.handleLogout)

		// Redirect (OIDC/OAuth) providers get a /start + /callback pair each.
		if rt.Providers != nil {
			for _, name := range rt.Providers.Names() {
				p, err := rt.Providers.Get(name)
				if err != nil {
					continue
				}
				if rp, ok := p.(auth.RedirectProvider); ok {
					sub.Get("/"+name+"/start", rt.handleOIDCStart(rp))
					sub.Get("/"+name+"/callback", rt.handleOIDCCallback(rp))
				}
			}
		}
	})
}

// handleLoginIndex enumerates available auth backends. Providers that
// implement auth.EnableChecker and report IsEnabled(ctx)=false are filtered
// out — the login UI then renders one tab per surviving backend. The default
// backend (per general.default_auth_backend) is listed first when it survives.
func (rt *Router) handleLoginIndex(w http.ResponseWriter, r *http.Request) {
	if rt.Providers == nil {
		WriteJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"backends": []backendInfo{}}})
		return
	}
	// Evaluate each backend's enabled state under the default tenant. The login
	// index is a pre-auth, tenant-less request, but providers whose IsEnabled
	// reads RuntimeSettings (LDAP, OIDC) need a tenant to see DB-stored toggles —
	// without this a runtime "enabled" edit would never surface on the login page.
	idxCtx := auth.WithTenant(r.Context(), snoozetypes.DefaultTenant)
	all := rt.Providers.Names()
	names := make([]string, 0, len(all))
	for _, n := range all {
		p, err := rt.Providers.Get(n)
		if err != nil {
			continue
		}
		if !auth.ProviderEnabled(idxCtx, p) {
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
	// Build the public tenant list: active+listed only, never login_key.
	type pubTenant struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	}
	tenants := []pubTenant{}
	if rt.DB != nil {
		pctx := auth.WithPlatformScope(r.Context())
		docs, _, err := rt.DB.Search(pctx, "tenant",
			condition.Equals("status", "active"), db.Page{})
		if err == nil {
			for _, d := range docs {
				// Filter suspended tenants — also handled by the DB condition,
				// but re-checked here so in-memory stubs (tests) stay correct.
				if status, _ := d["status"].(string); status == "suspended" {
					continue
				}
				if listed, ok := d["listed"].(bool); ok && !listed {
					continue // explicit listed:false hides the tenant
				}
				id, _ := d["id"].(string)
				if id == "" {
					continue
				}
				dn, _ := d["display_name"].(string)
				tenants = append(tenants, pubTenant{ID: id, DisplayName: dn})
			}
			sort.SliceStable(tenants, func(i, j int) bool { return tenants[i].DisplayName < tenants[j].DisplayName })
		}
	}
	infos := make([]backendInfo, 0, len(names))
	for _, n := range names {
		p, err := rt.Providers.Get(n)
		if err != nil {
			continue
		}
		bi := backendInfo{Name: n, Kind: "password"}
		if rp, ok := p.(auth.RedirectProvider); ok {
			bi.Kind = "redirect"
			bi.DisplayName = rp.DisplayName()
			bi.Icon = rp.Icon()
		}
		infos = append(infos, bi)
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"backends": infos, "tenants": tenants},
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
			// A disabled account is surfaced distinctly (403) so the user gets a
			// clear reason. The provider only returns ErrUserDisabled once the
			// password has already verified, so this does not let an anonymous
			// guesser enumerate account state.
			if errors.Is(err, auth.ErrUserDisabled) {
				WriteError(w, r, ErrForbidden.WithMessage("account disabled"))
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

// provisionOIDCUser ensures a user record exists for a redirect-provider (OIDC)
// login so SSO users are visible and manageable on the Users page, and enforces
// the `enabled` flag. It returns blocked=true when the user already exists and
// is disabled — the caller MUST abort the login (issue no session) in that case.
//
// All writes are best-effort: a provisioning write failure is swallowed and
// never breaks an otherwise-valid login. Only an explicit `enabled:false`
// blocks. The created/updated record carries no roles (effective roles come
// from group→role resolution), so a JIT row can never self-escalate; admin
// edits still flow through the user plugin's GuardWrite via the CRUD API.
func (rt *Router) provisionOIDCUser(ctx context.Context, id auth.Identity) (blocked bool) {
	if rt.DB == nil {
		return false
	}
	now := float64(time.Now().Unix())
	existing, err := rt.DB.GetOne(ctx, auth.LocalCollection, db.Document{
		"name":   id.Username,
		"method": id.Method,
	})
	if err == nil && existing != nil {
		if enabled, ok := existing["enabled"].(bool); ok && !enabled {
			return true // disabled — block the login before any token is minted
		}
		uid, _ := existing["uid"].(string)
		if uid != "" {
			// Refresh display/audit fields only; never touch enabled or roles.
			_ = rt.DB.UpdateOne(ctx, auth.LocalCollection, uid, db.Document{
				"groups":     id.Groups,
				"last_login": now,
			}, false)
		}
		return false
	}
	// First login: create the record (enabled by default). tenant_id is stamped
	// by the driver from ctx, so it is deliberately omitted from the document.
	doc := db.Document{
		"name":       id.Username,
		"method":     id.Method,
		"groups":     id.Groups,
		"enabled":    true,
		"last_login": now,
		"created_at": now,
	}
	_, _ = rt.DB.Write(ctx, auth.LocalCollection, []db.Document{doc}, db.WriteOptions{
		Primary:    []string{"tenant_id", "name", "method"},
		UpdateTime: true,
	})
	return false
}

// userDisabled reports whether the user named in the claims has been disabled.
// It is used to fail a refresh-token rotation for a user disabled after their
// session was minted (the refresh token alone would otherwise keep producing
// access tokens until it expired). Methods with no user record (e.g. anonymous)
// are never considered disabled.
func (rt *Router) userDisabled(ctx context.Context, c snoozetypes.Claims) bool {
	if rt.DB == nil {
		return false
	}
	tenantID := c.TenantID
	if tenantID == "" {
		tenantID = snoozetypes.DefaultTenant
	}
	tctx := auth.WithTenant(ctx, tenantID)
	doc, err := rt.DB.GetOne(tctx, auth.LocalCollection, db.Document{
		"name":   c.Subject,
		"method": c.Method,
	})
	if err != nil || doc == nil {
		return false
	}
	enabled, ok := doc["enabled"].(bool)
	return ok && !enabled
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
	// Suspend check: mirror handleLogin so an anonymous session cannot be
	// issued against a suspended org.
	if err := rt.checkTenantStatus(r.Context(), org); err != nil {
		WriteError(w, r, err)
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
	// refresh_token is a tenant-scoped collection, but this endpoint is on the
	// auth-skip path so r.Context() carries no tenant. The token_hash is a
	// 256-bit globally-unique value, so we look it up under platform scope; the
	// store then re-mints the rotation strictly under the stored row's tenant_id
	// (it never trusts the request context for the tenant). Without this the
	// driver fails closed (ErrNoTenant) and every refresh returns 401.
	pctx := auth.WithPlatformScope(r.Context())
	claims, newRefresh, refreshExp, err := rt.Refresh.VerifyAndRotate(pctx, body.RefreshToken)
	if err != nil {
		WriteError(w, r, ErrUnauthorized.WithMessage("invalid refresh token").WithCause(err))
		return
	}
	// Block a user disabled since login: revoke the just-rotated token and
	// refuse, so a disabled account cannot keep minting access tokens for the
	// remainder of the refresh lease.
	if rt.userDisabled(r.Context(), claims) {
		_ = rt.Refresh.Revoke(pctx, newRefresh)
		WriteError(w, r, ErrUnauthorized.WithMessage("account disabled"))
		return
	}
	// Re-resolve roles+permissions so a recent admin change takes effect on
	// the next access-token rotation (max staleness: the access-token lease).
	// resolveRoles re-scopes to the token's own tenant internally, so the
	// access token keeps its original tenant_id claim.
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
		// Logout is on the auth-skip path (naked context) and refresh_token is
		// tenant-scoped, so revoke under platform scope — otherwise the lookup
		// fails closed (ErrNoTenant) and the revoke silently no-ops, leaving the
		// token usable. The store fences the actual revoke by the high-entropy
		// token_hash.
		pctx := auth.WithPlatformScope(r.Context())
		_ = rt.Refresh.Revoke(pctx, body.RefreshToken)
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

// handleLoginResolveTenant GET /api/v1/login/tenant?key=<login_key>
// Resolves an opaque per-tenant login key to {id, display_name}. Returns a
// generic 404 on empty/unknown key or a suspended tenant — it never resolves
// by slug, so it cannot be used to enumerate tenants.
func (rt *Router) handleLoginResolveTenant(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" || rt.DB == nil {
		WriteError(w, r, ErrNotFound.WithMessage("unknown tenant"))
		return
	}
	pctx := auth.WithPlatformScope(r.Context())
	doc, err := rt.DB.GetOne(pctx, "tenant", db.Document{"login_key": key})
	if err != nil || doc == nil {
		WriteError(w, r, ErrNotFound.WithMessage("unknown tenant"))
		return
	}
	if status, _ := doc["status"].(string); status == "suspended" {
		WriteError(w, r, ErrNotFound.WithMessage("unknown tenant"))
		return
	}
	id, _ := doc["id"].(string)
	dn, _ := doc["display_name"].(string)
	WriteJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"id": id, "display_name": dn},
	})
}

// oidcStateKey derives the HMAC key for the OIDC state cookie from the JWT
// signing secret (stable per deploy, shared across cluster nodes).
func (rt *Router) oidcStateKey() ([]byte, error) {
	if rt.Auth == nil {
		return nil, errors.New("oidc: token engine not configured")
	}
	return rt.Auth.DeriveKey(oidcStateLabel), nil
}

// secureCookies reports whether the Secure cookie flag should be set: true when
// the request arrived over TLS directly (r.TLS), via a TLS-terminating proxy
// that set X-Forwarded-Proto: https, or when the server terminates TLS itself
// (core.ssl.enabled). The OIDC state cookie carries a PKCE verifier + nonce, so
// it must be Secure whenever the browser reached us over https.
func (rt *Router) secureCookies(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	return rt.Config != nil && rt.Config.Core.SSL.Enabled
}

// redirectLoginError sends the browser back to the SPA login page with a
// user-safe error message. Never leaks provider internals.
func redirectLoginError(w http.ResponseWriter, r *http.Request, msg string) {
	http.Redirect(w, r, "/web/login?sso_error="+url.QueryEscape(msg), http.StatusFound)
}

// handleOIDCStart begins the auth-code flow: generate state/nonce/PKCE, store
// them in a signed cookie, and redirect to the IdP authorize endpoint.
func (rt *Router) handleOIDCStart(rp auth.RedirectProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key, err := rt.oidcStateKey()
		if err != nil {
			redirectLoginError(w, r, "single sign-on is not configured")
			return
		}
		state, err1 := randURLToken(24)
		nonce, err2 := randURLToken(24)
		verifier := oauth2.GenerateVerifier()
		if err1 != nil || err2 != nil {
			redirectLoginError(w, r, "could not start sign-in")
			return
		}
		st := oidcState{
			State:    state,
			Nonce:    nonce,
			Verifier: verifier,
			ReturnTo: r.URL.Query().Get("return_to"),
			Org:      r.URL.Query().Get("org"),
			Exp:      time.Now().Add(10 * time.Minute).Unix(),
		}
		cookieVal := encodeOIDCState(key, st)
		if cookieVal == "" {
			redirectLoginError(w, r, "could not start sign-in")
			return
		}
		http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is set dynamically via secureCookies(r) (TLS / X-Forwarded-Proto); HttpOnly + SameSite=Lax are always set.
			Name:     oidcStateCookie,
			Value:    cookieVal,
			Path:     "/api/v1/login",
			MaxAge:   600,
			HttpOnly: true,
			Secure:   rt.secureCookies(r),
			SameSite: http.SameSiteLaxMode,
		})
		authURL, err := rp.AuthCodeURL(r.Context(), state, nonce, verifier)
		if err != nil {
			redirectLoginError(w, r, "single sign-on is unavailable")
			return
		}
		http.Redirect(w, r, authURL, http.StatusFound)
	}
}

// handleOIDCCallback completes the auth-code flow: validate state, exchange the
// code, verify the ID token, mint a Snooze session, and redirect to the SPA
// callback with the token in the URL fragment.
func (rt *Router) handleOIDCCallback(rp auth.RedirectProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Always clear the state cookie.
		http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is set dynamically via secureCookies(r) (TLS / X-Forwarded-Proto); HttpOnly + SameSite=Lax are always set.
			Name: oidcStateCookie, Value: "", Path: "/api/v1/login", MaxAge: -1,
			HttpOnly: true, Secure: rt.secureCookies(r), SameSite: http.SameSiteLaxMode,
		})
		if e := r.URL.Query().Get("error"); e != "" {
			redirectLoginError(w, r, "sign-in was cancelled or failed")
			return
		}
		key, err := rt.oidcStateKey()
		if err != nil {
			redirectLoginError(w, r, "single sign-on is not configured")
			return
		}
		c, err := r.Cookie(oidcStateCookie)
		if err != nil {
			redirectLoginError(w, r, "your sign-in session expired, please try again")
			return
		}
		st, err := decodeOIDCState(key, c.Value)
		if err != nil {
			redirectLoginError(w, r, "invalid sign-in session, please try again")
			return
		}
		if subtle.ConstantTimeCompare([]byte(st.State), []byte(r.URL.Query().Get("state"))) != 1 {
			redirectLoginError(w, r, "sign-in could not be verified, please try again")
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			redirectLoginError(w, r, "sign-in failed")
			return
		}
		org := st.Org
		if org == "" {
			org = snoozetypes.DefaultTenant
		}
		authCtx := auth.WithTenant(r.Context(), org)
		id, err := rp.ExchangeAndVerify(authCtx, code, st.Nonce, st.Verifier)
		if err != nil {
			redirectLoginError(w, r, "sign-in failed")
			return
		}
		if err := rt.checkTenantStatus(r.Context(), org); err != nil {
			redirectLoginError(w, r, "your organization is not allowed to sign in")
			return
		}
		if id.TenantID == "" {
			id.TenantID = org
		}
		// JIT-provision the SSO user (so it appears on the Users page) and
		// enforce the enabled flag. A disabled user is blocked here, before any
		// session is issued. provisionOIDCUser also refreshes last_login/groups,
		// so the OIDC path does not call updateLastLogin separately.
		if rt.provisionOIDCUser(authCtx, id) {
			redirectLoginError(w, r, "your account has been disabled")
			return
		}
		resp, err := rt.signSession(authCtx, id)
		if err != nil {
			redirectLoginError(w, r, "sign-in failed")
			return
		}

		frag := url.Values{}
		frag.Set("token", resp.Token)
		if resp.RefreshToken != "" {
			frag.Set("refresh_token", resp.RefreshToken)
		}
		if st.ReturnTo != "" {
			frag.Set("return_to", st.ReturnTo)
		}
		http.Redirect(w, r, "/web/login/callback#"+frag.Encode(), http.StatusFound)
	}
}
