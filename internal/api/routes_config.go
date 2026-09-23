package api

import (
	"errors"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"

	"github.com/snoozeweb/snooze/internal/api/middleware"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// ConsoleConfig is the org-wide web-console default document served read-only
// at GET /api/v1/config. It is presentation-only — it deliberately does NOT
// mirror Alerta's config.py (which leaks client_id/azure_tenant/provider
// details); auth bootstrap stays in the login/OIDC discovery routes.
//
// Code defaults (DefaultConsoleConfig) are overlaid by the runtime `console`
// settings section. There is intentionally no `colors` field: a custom
// severity is placed by RANK (severity_ranks) and inherits a theme-aware
// colour variant client-side; the six --severity-* tokens are the only palette.
type ConsoleConfig struct {
	Columns       []string       `json:"columns"`            // ordered column ids
	DefaultFilter string         `json:"default_filter"`     // saved-search expr or ""
	SortBy        string         `json:"sort_by"`            // e.g. "-date_epoch"
	RefreshSecs   int            `json:"refresh_interval"`   // auto-refresh interval, seconds
	SeverityRanks map[string]int `json:"severity_ranks"`     // label → rank (Plan 21 ladder + operator additions)
	SeverityOrder []string       `json:"severity_order"`     // labels most→least severe (derived from ranks)
	Logo          string         `json:"logo"`               // data: URI or URL, "" = bundled
	Title         string         `json:"title"`              // browser/app title, "" = default
	Audio         string         `json:"audio"`              // "new alert" cue URL, "" = none
	Clipboard     string         `json:"clipboard_template"` // copy-to-clipboard template, "" = default
}

// defaultColumns is the server-owned default alert-table layout the SPA falls
// back to when no `console.columns` override is set. Message-first: severity
// then the message (which takes the flexible remainder of the row), then the
// aggregation count, state, owner (who is working on it) and age.
//
// Deliberately NOT the full set defined in web/src/features/alerts/columns.tsx
// — `process` and `source` are defined there but left off this list, because
// they cost ~320px of every desktop row to repeat what host and message
// already say and are one click away in the row inspector. An operator who
// wants them re-adds the id under Settings → Console → "Alert table columns"
// (the metadata default in internal/pluginimpl/settings/metadata.yaml and the
// SPA's offline fallback in web/src/features/config/types.ts must match this).
//
// `acked_by` used to sit in this list and has never had a column definition to
// resolve against, so the SPA silently dropped it; it's gone from the default
// rather than left as a no-op id.
var defaultColumns = []string{
	"severity",
	"message",
	"hits",
	"state",
	"owner",
	"date_epoch",
	"host",
	"environment",
	"ttl",
}

const (
	// defaultRefreshSecs is the alerts-page auto-refresh interval the SPA
	// hardcodes today (useAutoRefresh(5000)).
	defaultRefreshSecs = 5
	// defaultSortBy mirrors the alerts-page default sort (date_epoch desc).
	defaultSortBy = "-date_epoch"
)

// DefaultConsoleConfig builds the code-default console document: the Plan 21
// severity ladder (snoozetypes.DefaultSeverityRank) plus the current frontend
// hardcodes for columns/sort/refresh. cfg is accepted for forward
// compatibility (file-config-derived defaults); it is currently unused.
func DefaultConsoleConfig() ConsoleConfig {
	ranks := make(map[string]int, len(snoozetypes.DefaultSeverityRank))
	for label, rank := range snoozetypes.DefaultSeverityRank {
		ranks[label] = rank
	}
	cc := ConsoleConfig{
		Columns:       append([]string(nil), defaultColumns...),
		DefaultFilter: "",
		SortBy:        defaultSortBy,
		RefreshSecs:   defaultRefreshSecs,
		SeverityRanks: ranks,
		Logo:          "",
		Title:         "",
		Audio:         "",
		Clipboard:     "",
	}
	cc.SeverityOrder = severityOrder(ranks)
	return cc
}

// severityOrder returns the rank labels sorted most→least severe (ascending
// rank), ties broken alphabetically so the output is deterministic. It is
// always recomputed from the (merged) rank map — never stored.
func severityOrder(ranks map[string]int) []string {
	order := make([]string, 0, len(ranks))
	for label := range ranks {
		order = append(order, label)
	}
	sort.Slice(order, func(i, j int) bool {
		ri, rj := ranks[order[i]], ranks[order[j]]
		if ri != rj {
			return ri < rj
		}
		return order[i] < order[j]
	})
	return order
}

// consoleOverride is the runtime overlay decoded from the `console` settings
// section. Scalar fields are pointers so a present key (even a zero value)
// replaces the default, while an absent key leaves the default intact. Slice
// and map fields are nil when absent.
//
// The JSON tags match the ConsoleConfig wire names so the stored section
// (which the admin Settings page edits key-by-key) decodes directly.
type consoleOverride struct {
	Columns       []string       `json:"columns"`
	DefaultFilter *string        `json:"default_filter"`
	SortBy        *string        `json:"sort_by"`
	RefreshSecs   *int           `json:"refresh_interval"`
	SeverityRanks map[string]int `json:"severity_ranks"`
	Logo          *string        `json:"logo"`
	Title         *string        `json:"title"`
	Audio         *string        `json:"audio"`
	Clipboard     *string        `json:"clipboard_template"`
}

// applyTo overlays the override onto cc. Scalar pointers replace when
// non-nil; slice fields replace wholesale when non-nil; SeverityRanks MERGES
// key-by-key (so an operator adds {"p1": 2} without re-listing the ladder).
// SeverityOrder is ALWAYS recomputed from the merged ranks afterwards.
func (o *consoleOverride) applyTo(cc *ConsoleConfig) {
	if o == nil {
		return
	}
	if o.Columns != nil {
		cc.Columns = o.Columns
	}
	if o.DefaultFilter != nil {
		cc.DefaultFilter = *o.DefaultFilter
	}
	if o.SortBy != nil {
		cc.SortBy = *o.SortBy
	}
	if o.RefreshSecs != nil {
		cc.RefreshSecs = *o.RefreshSecs
	}
	if o.Logo != nil {
		cc.Logo = *o.Logo
	}
	if o.Title != nil {
		cc.Title = *o.Title
	}
	if o.Audio != nil {
		cc.Audio = *o.Audio
	}
	if o.Clipboard != nil {
		cc.Clipboard = *o.Clipboard
	}
	if o.SeverityRanks != nil {
		if cc.SeverityRanks == nil {
			cc.SeverityRanks = make(map[string]int, len(o.SeverityRanks))
		}
		for label, rank := range o.SeverityRanks {
			cc.SeverityRanks[label] = rank
		}
	}
	// SeverityOrder is derived, never stored: recompute from the merged ranks.
	cc.SeverityOrder = severityOrder(cc.SeverityRanks)
}

// mountConfig wires GET /api/v1/config — the public, read-only web-console
// defaults document. The path stays in skipAuth (router.go) so the strict
// Auth middleware keeps skipping it globally, but this mount replaces that
// skip with middleware.OptionalAuth on this one route: it resolves a Bearer
// token when present (stamping claims/tenant so a logged-in caller's
// `console` overlay applies) without ever 401ing an anonymous caller (the
// login screen bootstrapping branding before a token exists). rt.Auth and
// rt.APIKeys are the same token engine / key store the strict middleware in
// Build() is constructed with, so a token valid there is valid here.
func (rt *Router) mountConfig(r chi.Router) {
	var keys middleware.APIKeyAuthenticator
	if rt.APIKeys != nil {
		keys = rt.APIKeys
	}
	r.With(middleware.OptionalAuth(rt.Auth, keys)).Get("/api/v1/config", rt.handleConfig)
}

// handleConfig builds the code defaults, overlays the runtime `console`
// settings section (when a RuntimeStore is wired), recomputes the derived
// severity order, and writes the {"data": …} envelope. It is read-only; a nil
// store yields pure defaults.
//
// The settings collection is tenant-scoped, so GetSection fails closed with
// snoozetypes.ErrNoTenant when the context carries no tenant — which is
// exactly the case for an anonymous caller (OptionalAuth left no claims on
// the context: no token, or an invalid one). That is not an error worth a
// 500: anonymous callers (the login screen) get the pure code defaults,
// while an authenticated caller's OptionalAuth-resolved tenant reaches the
// overlay below. Any other GetSection error (storage failure, decode error)
// still 500s.
func (rt *Router) handleConfig(w http.ResponseWriter, r *http.Request) {
	cc := DefaultConsoleConfig()
	if rt.RuntimeStore != nil {
		var ov consoleOverride
		if err := rt.RuntimeStore.GetSection(r.Context(), "console", &ov); err != nil {
			if !errors.Is(err, snoozetypes.ErrNoTenant) {
				WriteError(w, r, ErrInternal.WithCause(err))
				return
			}
		} else {
			ov.applyTo(&cc)
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"data": cc})
}
