// Package heartbeat implements the "heartbeat" dead-man's-switch input plugin.
//
// A heartbeat is a named expectation that some external job will "ping" Snooze
// at a regular interval. Each heartbeat document lives in the `heartbeat`
// collection (the plugin is a plugins.DataModel, so the generic CRUD mounter
// exposes /api/v1/heartbeat) and records when it was last seen. An external job
// keeps the switch alive by POSTing (or GETting)
// /api/v1/webhook/heartbeat?name=<name>&token=<token>, which stamps `last_seen`
// to now (the plugin is a plugins.WebhookReceiver).
//
// The core server drives a per-tenant scan on a ticker, calling the plugin's
// ScanTenant for each active tenant. For every heartbeat silent past
// interval+grace it injects one miss alert into that tenant's pipeline; dedup
// is per (tenant,name) and a fresh ping clears it so the next missed window
// fires again.
//
// # Auth
//
// CRUD (manage heartbeat records) requires a valid Bearer token — operators
// must be logged in. The ping endpoint itself is network-public (no Bearer
// token required) but is protected at the application layer by a per-heartbeat
// secret token generated at record creation time. Every ping must supply both
// ?name=<name> and ?token=<token>; a missing or wrong token is rejected 401.
// A token that resolves to a heartbeat whose stored name does not match the
// supplied ?name= is also rejected 401.
package heartbeat

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// metaYAML is the raw metadata.yaml content embedded at build time.
//
//go:embed metadata.yaml
var metaYAML []byte

func init() {
	plugins.Register("heartbeat", metaYAML, factory)
}

// factory is the plugins.Factory entry-point.
func factory(meta plugins.Metadata) (plugins.Plugin, error) {
	return &Plugin{
		meta:     meta,
		now:      time.Now,
		interval: defaultScanInterval,
		fired:    make(map[string]string),
	}, nil
}

// collection is the DB collection this plugin owns. It matches Name() so the
// generic CRUD mounter installs /api/v1/heartbeat for it.
const collection = "heartbeat"

// defaultScanInterval is how often the background scanner wakes to look for
// missed heartbeats. It is intentionally coarse: heartbeats are minute-scale
// expectations, not sub-second.
const defaultScanInterval = 30 * time.Second

// defaultSeverity is the severity stamped on a miss alert when the heartbeat
// document does not specify one.
const defaultSeverity = "critical"

// recordProcessor is the slice of the alert pipeline this plugin needs. The
// concrete *core.Core satisfies this shape; the runtime assertion sidesteps an
// import cycle through internal/plugins.Host. Mirrors grafana/alertmanager.
type recordProcessor interface {
	ProcessRecord(ctx context.Context, rec snoozetypes.Record) (snoozetypes.Record, plugins.Action, error)
}

// Plugin is the heartbeat dead-man's-switch.
//
// Lifecycle: Register → factory → PostInit (captures the host). HandleWebhook
// services pings; ScanTenant is invoked once per active tenant per tick by the
// core heartbeat job, injecting a miss alert into that tenant's pipeline.
type Plugin struct {
	meta plugins.Metadata
	host plugins.Host

	// now is the clock, injectable so tests can fake time. Defaults to time.Now.
	now func() time.Time
	// interval is the scanner tick period. Defaults to defaultScanInterval.
	interval time.Duration

	// mu guards fired and warnedNoProcessor.
	mu sync.Mutex
	// fired remembers, per (tenant,name) pair, the last_seen value (RFC3339) we
	// already fired a miss alert for. This dedups repeated firing across ticks
	// for the same silent window. A fresh ping rewrites last_seen, so the next
	// scan sees a different value and re-arms.
	fired map[string]string

	// warnedNoProcessor ensures the "host has no recordProcessor" warning fires
	// at most once even when many scans run.
	warnedNoProcessor bool
}

// Name returns the registry key and collection identifier.
func (p *Plugin) Name() string { return "heartbeat" }

// Metadata returns the parsed metadata.yaml descriptor.
func (p *Plugin) Metadata() plugins.Metadata { return p.meta }

// PostInit wires the host in and backfills tokens for legacy heartbeat
// documents that pre-date the per-heartbeat token feature.
//
// Legacy heartbeats (created before tokens existed) have no `token` field, so
// their pings would always fail with 401. For every such document PostInit
// generates a fresh token via generateToken() and persists it with SetFields.
// Documents that already carry a non-empty token are left untouched, making the
// migration idempotent across restarts.
//
// Clustering race: two instances booting concurrently may both mint a token for
// the same heartbeat; last write wins. The surviving token is valid and readable
// from the API, so the race is benign.
//
// Nil-safety: if the host is nil or host.DB() returns nil (migration tool,
// early-boot tests) the backfill is skipped and nil is returned.
func (p *Plugin) PostInit(ctx context.Context, host plugins.Host) error {
	p.host = host
	if p.now == nil {
		p.now = time.Now
	}
	if p.interval <= 0 {
		p.interval = defaultScanInterval
	}
	if p.fired == nil {
		p.fired = make(map[string]string)
	}

	// Backfill tokens for legacy (tokenless) heartbeat documents.
	driver := p.db()
	if driver == nil {
		// No DB available (nil host or nil DB); skip migration gracefully.
		return nil
	}

	// Index the token field: the unauthenticated ping resolves a heartbeat (and
	// thus its tenant) by token under platform scope.
	if err := driver.CreateIndex(ctx, collection, []string{"token"}); err != nil {
		if lg := p.logger(); lg != nil {
			lg.Warn("heartbeat: PostInit create token index failed", "err", err)
		}
	}

	docs, _, err := driver.Search(ctx, collection, condition.Cond{}, db.Page{})
	if err != nil {
		if lg := p.logger(); lg != nil {
			lg.Warn("heartbeat: PostInit token backfill search failed", "err", err)
		}
		// Best-effort: do not fail PostInit on a search error.
		return nil
	}

	for _, doc := range docs {
		name, _ := doc["name"].(string)
		if name == "" {
			continue
		}
		existing, _ := doc["token"].(string)
		if existing != "" {
			// Already has a token; nothing to do.
			continue
		}
		tok, err := generateToken()
		if err != nil {
			if lg := p.logger(); lg != nil {
				lg.Warn("heartbeat: PostInit failed to generate token", "name", name, "err", err)
			}
			continue
		}
		if _, err := driver.SetFields(ctx, collection, db.Document{"token": tok}, condition.Equals("name", name)); err != nil {
			if lg := p.logger(); lg != nil {
				lg.Warn("heartbeat: PostInit failed to persist backfilled token", "name", name, "err", err)
			}
		}
	}

	return nil
}

// Reload is a no-op: the plugin caches no document state in memory (the only
// in-memory state is the per-window fired set, which must survive a reload).
func (p *Plugin) Reload(_ context.Context) error { return nil }

// WebhookPath returns the route fragment mounted under /api/v1/webhook/. The
// full external URL is therefore /api/v1/webhook/heartbeat.
func (p *Plugin) WebhookPath() string { return "/heartbeat" }

// Compile-time proof we satisfy every contract this plugin advertises.
var (
	_ plugins.Plugin           = (*Plugin)(nil)
	_ plugins.DataModel        = (*Plugin)(nil)
	_ plugins.WebhookReceiver  = (*Plugin)(nil)
	_ plugins.WriteTransformer = (*Plugin)(nil)
)

// ---- WebhookReceiver: ping ------------------------------------------------

// HandleWebhook services a ping. It accepts POST or GET with both the
// heartbeat name and the per-heartbeat secret token in the query string:
// ?name=<name>&token=<token>.
//
// Validation order:
//  1. Method check (POST or GET only; anything else → 405).
//  2. name required (→ 400 when absent).
//  3. token required (→ 401 when absent).
//  4. Resolve the heartbeat by its token under platform scope: 0 matches → 401
//     (an unknown token does not reveal whether the name exists); >1 match →
//     500 (token collision).
//  5. The stored name must equal the supplied name (→ 401 on mismatch).
//  6. The matched heartbeat must carry a tenant_id (→ 500 if absent; indicates
//     an incomplete migration).
//  7. On success: stamp last_seen and clear fired state under the resolved
//     tenant, return 200.
//
// NOTE: the standard webhook router (internal/api/router.go mountWebhooks) only
// mounts POST on the webhook path, so GET pings reach this handler only when a
// caller wires the handler directly. The GET branch is supported here so the
// handler is correct independent of how it is mounted, and so the package is
// self-contained for testing.
func (p *Plugin) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost, http.MethodGet:
	default:
		w.Header().Set("Allow", http.MethodPost+", "+http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "missing heartbeat name (use ?name=<name>&token=<token>)", http.StatusBadRequest)
		return
	}

	suppliedToken := r.URL.Query().Get("token")
	if suppliedToken == "" {
		http.Error(w, "missing heartbeat token (use ?name=<name>&token=<token>)", http.StatusUnauthorized)
		return
	}

	// Resolve the heartbeat by its token under platform scope: the token is a
	// 24-byte crypto-random secret and is the only globally unique key now that
	// heartbeat names are per-tenant. The exact-token match *is* the auth check.
	driver := p.db()
	if driver == nil {
		http.Error(w, "heartbeat: no database available", http.StatusInternalServerError)
		return
	}
	docs, _, err := driver.Search(
		snoozetypes.WithPlatformScope(r.Context()),
		collection, condition.Equals("token", suppliedToken), db.Page{})
	if err != nil {
		if lg := p.logger(); lg != nil {
			lg.Warn("heartbeat: ping lookup failed", "err", err)
		}
		http.Error(w, fmt.Sprintf("heartbeat ping failed: %v", err), http.StatusInternalServerError)
		return
	}
	if len(docs) == 0 {
		// Unknown token: do not reveal whether the name exists.
		http.Error(w, "invalid heartbeat token", http.StatusUnauthorized)
		return
	}
	if len(docs) > 1 {
		// Astronomically unlikely token collision across tenants.
		if lg := p.logger(); lg != nil {
			lg.Warn("heartbeat: token resolved to multiple heartbeats", "count", len(docs))
		}
		http.Error(w, "heartbeat token is ambiguous", http.StatusInternalServerError)
		return
	}
	hbDoc := docs[0]
	storedName, _ := hbDoc["name"].(string)
	if storedName != name {
		http.Error(w, "invalid heartbeat token", http.StatusUnauthorized)
		return
	}
	tenantID, _ := hbDoc["tenant_id"].(string)
	if tenantID == "" {
		if lg := p.logger(); lg != nil {
			lg.Warn("heartbeat: matched heartbeat has no tenant_id; migration incomplete?", "name", name)
		}
		http.Error(w, "heartbeat: tenant not resolved", http.StatusInternalServerError)
		return
	}
	tenantCtx := snoozetypes.WithTenant(r.Context(), tenantID)

	matched, err := p.touch(tenantCtx, name)
	if err != nil {
		if lg := p.logger(); lg != nil {
			lg.Warn("heartbeat: ping update failed", "name", name, "err", err)
		}
		http.Error(w, fmt.Sprintf("heartbeat ping failed: %v", err), http.StatusInternalServerError)
		return
	}
	if matched == 0 {
		http.Error(w, fmt.Sprintf("unknown heartbeat %q", name), http.StatusNotFound)
		return
	}

	// A successful ping re-arms the heartbeat: forget any miss we already fired
	// so the next silent window fires again.
	p.clearFired(tenantID, name)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "name": name})
}

// touch stamps last_seen=now (RFC3339, UTC) on the named heartbeat and returns
// how many documents matched. A 0 return means the name does not exist.
func (p *Plugin) touch(ctx context.Context, name string) (int, error) {
	driver := p.db()
	if driver == nil {
		return 0, fmt.Errorf("heartbeat: no database available")
	}
	patch := db.Document{"last_seen": p.now().UTC().Format(time.RFC3339)}
	return driver.SetFields(ctx, collection, patch, condition.Equals("name", name))
}

// ---- WriteTransformer: token generation -----------------------------------

// TransformWrite implements plugins.WriteTransformer. It generates a
// cryptographically random, URL-safe token and stores it in doc["token"] when
// the field is absent or empty. An existing non-empty token is never
// overwritten, so PATCH operations that do not touch the token field preserve
// the value the operator received at create time.
//
// Contract (mirrors user plugin's password-hashing path):
//   - If doc["token"] is absent → this is a create (full document); generate.
//   - If doc["token"] is present but empty string → treat same as absent; generate.
//   - If doc["token"] is present and non-empty → preserve (never overwrite).
func (p *Plugin) TransformWrite(_ context.Context, doc map[string]any) error {
	if t, ok := doc["token"]; ok {
		if s, _ := t.(string); s != "" {
			// Caller supplied an explicit token (unusual but valid) — preserve it.
			return nil
		}
	}
	// Only generate a token on documents that look like a create (i.e. they carry
	// `name`). A partial PATCH that does not carry `name` is updating an existing
	// document and must not invent a new token.
	if _, hasName := doc["name"]; !hasName {
		return nil
	}
	tok, err := generateToken()
	if err != nil {
		return fmt.Errorf("heartbeat: generate token: %w", err)
	}
	doc["token"] = tok
	return nil
}

// generateToken returns a cryptographically random, URL-safe string of ~32
// base64-RawURL characters (24 random bytes → 32 chars, no padding).
func generateToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ---- helpers --------------------------------------------------------------

func (p *Plugin) db() db.Driver {
	if p.host == nil {
		return nil
	}
	return p.host.DB()
}

func (p *Plugin) recordProcessor() recordProcessor {
	if p.host == nil {
		return nil
	}
	rp, ok := any(p.host).(recordProcessor)
	if !ok {
		return nil
	}
	return rp
}

// logger returns the host logger or nil if unavailable. Mirrors grafana.
func (p *Plugin) logger() interface {
	Warn(string, ...any)
	Info(string, ...any)
} {
	if p.host == nil {
		return nil
	}
	lg := p.host.Logger()
	if lg == nil {
		return nil
	}
	return lg
}

func (p *Plugin) clearFired(tenant, name string) {
	p.mu.Lock()
	delete(p.fired, firedKey(tenant, name))
	p.mu.Unlock()
}
