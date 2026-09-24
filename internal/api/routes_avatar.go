// routes_avatar.go serves the people directory and profile pictures:
//
//	GET    /api/v1/people                  — enabled users of the caller's tenant
//	GET    /api/v1/avatar/{method}/{name}  — one user's picture, as a data URL
//	PUT    /api/v1/user/me/avatar          — upload the caller's picture
//	DELETE /api/v1/user/me/avatar          — remove it
//
// The two /user/me routes are mounted by mountUser (the /api/v1/user/me
// subrouter must be registered once); the handlers live here.
//
// Pictures are stored in the tenant-scoped `avatar` collection, keyed by
// (name, method), as a bare base64 PNG. Their content hash is mirrored onto the
// user doc as `avatar_version` so the directory needs no join and clients can
// cache a picture by (name, method, version). Any authenticated user may read
// the directory and every picture of their tenant; neither carries anything
// beyond what an avatar needs to render.

package api

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// avatarCollection holds the profile pictures. It is tenant-scoped (never
// registered global) and purged with its tenant (see tenantPurgeCollections).
const avatarCollection = "avatar"

// avatarMaxBodyBytes bounds the PUT body read into memory: comfortably above
// a base64 data URL of avatarMaxInputBytes, so the precise limit is reported by
// normalizeAvatar and only an absurd body is cut off here.
const avatarMaxBodyBytes = 1 << 20

// person is one GET /api/v1/people entry (the Person schema).
type person struct {
	Name          string `json:"name"`
	Method        string `json:"method"`
	DisplayName   string `json:"display_name"`
	AvatarVersion string `json:"avatar_version"`
}

// avatarResponse is the Avatar schema returned by GET /avatar/{method}/{name}.
type avatarResponse struct {
	Name    string `json:"name"`
	Method  string `json:"method"`
	Version string `json:"version"`
	Data    string `json:"data"`
}

// mountPeople wires the read side: the directory and the picture lookup. No
// plugin owns /api/v1/people or /api/v1/avatar, so there is nothing to shadow.
func (rt *Router) mountPeople(r chi.Router) {
	r.Get("/api/v1/people", rt.handlePeople)
	r.Get("/api/v1/avatar/{method}/{name}", rt.handleGetAvatar)
}

// requireCaller returns the verified claims, or writes 503 (no DB) / 401 (no
// claims) and reports false.
func (rt *Router) requireCaller(w http.ResponseWriter, r *http.Request) (snoozetypes.Claims, bool) {
	if rt.DB == nil {
		WriteError(w, r, ErrUnavailable.WithMessage("database not configured"))
		return snoozetypes.Claims{}, false
	}
	claims, ok := auth.ClaimsFrom(r.Context())
	if !ok || claims.Subject == "" {
		WriteError(w, r, ErrUnauthorized.WithMessage("authentication required"))
		return snoozetypes.Claims{}, false
	}
	return claims, true
}

// requireAvatarOwner is requireCaller plus the API-key refusal: a key-backed
// request carries method "apikey" rather than its owner's auth method, so it
// cannot name the (name, method) identity a picture belongs to.
func (rt *Router) requireAvatarOwner(w http.ResponseWriter, r *http.Request) (snoozetypes.Claims, bool) {
	claims, ok := rt.requireCaller(w, r)
	if !ok {
		return claims, false
	}
	if claims.Method == auth.APIKeyMethod {
		WriteError(w, r, ErrForbidden.WithMessage("profile pictures cannot be changed with an API key"))
		return claims, false
	}
	return claims, true
}

// userIdentity matches the user doc (or avatar doc) of one (name, method).
func userIdentity(name, method string) condition.Cond {
	return condition.And(condition.Equals("name", name), condition.Equals("method", method))
}

// handlePeople GET /api/v1/people lists the enabled users of the caller's
// tenant, sorted case-insensitively by display name (the login when there is
// none), then login, then method. A user doc without `enabled` counts as
// enabled, as it does at login. The tenant fence is the driver's.
func (rt *Router) handlePeople(w http.ResponseWriter, r *http.Request) {
	if _, ok := rt.requireCaller(w, r); !ok {
		return
	}
	docs, _, err := rt.DB.Search(r.Context(), auth.LocalCollection, condition.Cond{}, db.Page{})
	if err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	out := make([]person, 0, len(docs))
	for _, d := range docs {
		if enabled, ok := d["enabled"].(bool); ok && !enabled {
			continue
		}
		p := person{}
		p.Name, _ = d["name"].(string)
		p.Method, _ = d["method"].(string)
		p.DisplayName, _ = d["display_name"].(string)
		p.AvatarVersion, _ = d["avatar_version"].(string)
		if p.Name == "" {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if ka, kb := strings.ToLower(personLabel(a)), strings.ToLower(personLabel(b)); ka != kb {
			return ka < kb
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Method < b.Method
	})
	WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

// personLabel is what the UI shows for p: the display name, else the login.
func personLabel(p person) string {
	if p.DisplayName != "" {
		return p.DisplayName
	}
	return p.Name
}

// handleGetAvatar GET /api/v1/avatar/{method}/{name} returns the stored
// picture as a PNG data URL, or 404.
func (rt *Router) handleGetAvatar(w http.ResponseWriter, r *http.Request) {
	if _, ok := rt.requireCaller(w, r); !ok {
		return
	}
	name, method, ok := avatarIdentityParams(r)
	if !ok {
		WriteError(w, r, ErrValidation.WithMessage("malformed name or method"))
		return
	}
	doc, err := rt.DB.GetOne(r.Context(), avatarCollection, db.Document{"name": name, "method": method})
	if errors.Is(err, db.ErrNotFound) || (err == nil && doc == nil) {
		WriteError(w, r, ErrNotFound.WithMessage("no profile picture"))
		return
	}
	if err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	data, _ := doc["data"].(string)
	if data == "" {
		WriteError(w, r, ErrNotFound.WithMessage("no profile picture"))
		return
	}
	version, _ := doc["version"].(string)
	WriteJSON(w, http.StatusOK, avatarResponse{
		Name:    name,
		Method:  method,
		Version: version,
		Data:    "data:image/png;base64," + data,
	})
}

// avatarIdentityParams reads {method} and {name} unescaped. chi routes on
// r.URL.RawPath whenever the client's escaping differs from Go's default, and
// then hands URLParam the still-escaped segment: an email login sent as
// "alice%40example.com" (what encodeURIComponent produces) would otherwise be
// looked up verbatim and never match.
// Only then: with no RawPath the segment is already decoded, and unescaping it
// again would mangle a login that contains a literal "%".
func avatarIdentityParams(r *http.Request) (name, method string, ok bool) {
	name, method = chi.URLParam(r, "name"), chi.URLParam(r, "method")
	if r.URL.RawPath == "" {
		return name, method, true
	}
	name, errName := url.PathUnescape(name)
	method, errMethod := url.PathUnescape(method)
	return name, method, errName == nil && errMethod == nil
}

// avatarUploadRequest is the PUT /api/v1/user/me/avatar body.
type avatarUploadRequest struct {
	Data string `json:"data"`
}

// handlePutMyAvatar PUT /api/v1/user/me/avatar validates and re-encodes the
// uploaded picture (normalizeAvatar), upserts it under the caller's own
// (name, method) and mirrors its version onto the user doc. The identity is
// always the caller's claims; the body names no user.
func (rt *Router) handlePutMyAvatar(w http.ResponseWriter, r *http.Request) {
	claims, ok := rt.requireAvatarOwner(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, avatarMaxBodyBytes)
	var body avatarUploadRequest
	if err := ParseJSONBody(r, &body); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			WriteError(w, r, ErrPayloadTooLarge.WithMessage("profile picture too large"))
			return
		}
		WriteError(w, r, err)
		return
	}
	pngBytes, err := normalizeAvatar(body.Data)
	switch {
	case errors.Is(err, errAvatarTooLarge):
		WriteError(w, r, ErrPayloadTooLarge.WithMessage(err.Error()))
		return
	case err != nil:
		WriteError(w, r, ErrValidation.WithMessage(err.Error()))
		return
	}
	version := avatarVersion(pngBytes)

	ctx := r.Context()
	if _, err := rt.DB.Write(ctx, avatarCollection, []db.Document{{
		"name":          claims.Subject,
		"method":        claims.Method,
		"version":       version,
		"data":          base64.StdEncoding.EncodeToString(pngBytes),
		"updated_epoch": time.Now().Unix(),
	}}, db.WriteOptions{Primary: []string{"name", "method"}, UpdateTime: true}); err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	// A caller with no user doc yet (never provisioned) still gets the
	// picture; the directory just has no row to carry the version.
	if _, err := rt.DB.SetFields(ctx, auth.LocalCollection,
		db.Document{"avatar_version": version}, userIdentity(claims.Subject, claims.Method)); err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"version": version})
}

// handleDeleteMyAvatar DELETE /api/v1/user/me/avatar removes the caller's
// picture and clears the mirrored version; 204 whether or not one existed.
func (rt *Router) handleDeleteMyAvatar(w http.ResponseWriter, r *http.Request) {
	claims, ok := rt.requireAvatarOwner(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	who := userIdentity(claims.Subject, claims.Method)
	if _, err := rt.DB.Delete(ctx, avatarCollection, who, false); err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	if _, err := rt.DB.SetFields(ctx, auth.LocalCollection, db.Document{"avatar_version": ""}, who); err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
