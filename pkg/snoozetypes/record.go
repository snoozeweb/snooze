// Package snoozetypes contains wire types shared between the server, CLI, SDK, and components.
package snoozetypes

import (
	"encoding/json"
	"time"
)

// Record is the canonical alert document moving through the Snooze pipeline.
// It mirrors the Python record schema. Fields are JSON-friendly and stable across
// the v1 API.
type Record struct {
	UID         string    `json:"uid,omitempty"`
	Host        string    `json:"host,omitempty"`
	Source      string    `json:"source,omitempty"`
	Process     string    `json:"process,omitempty"`
	Severity    string    `json:"severity,omitempty"`
	Message     string    `json:"message,omitempty"`
	Timestamp   time.Time `json:"timestamp,omitempty"`
	DateEpoch   int64     `json:"date_epoch,omitempty"`
	TTL         int64     `json:"ttl,omitempty"`
	Environment string    `json:"environment,omitempty"`
	// Hash is the aggregaterule-computed duplicate-detection key. Lives as a
	// typed field so it survives the snooze-server → snooze-teams JSON hop;
	// stuffing it into Extra would silently drop it because Extra is `json:"-"`.
	Hash  string         `json:"hash,omitempty"`
	Tags  []string       `json:"tags,omitempty"`
	Raw   map[string]any `json:"raw,omitempty"`
	State string         `json:"state,omitempty"`
	// AckedBy is the login of the operator who last acknowledged the alert.
	// Stamped by the comment plugin on ack and cleared on open or close.
	// Empty/absent when the alert has not been acknowledged in its current lifecycle.
	AckedBy string   `json:"acked_by,omitempty"`
	Plugins []string `json:"plugins,omitempty"`
	// AckUntil is the epoch-seconds deadline after which a server-controlled ack
	// expires: the housekeeper's escalate-timeout sweep reverts the record from
	// "ack" back to "open" once now passes this. Set when a state→ack comment is
	// posted; cleared (0) on any other transition. Survives the JSON hop and is
	// projected by recordToDoc.
	AckUntil int64 `json:"ack_until,omitempty"`
	// EscalateAt is the epoch-seconds deadline after which an un-acknowledged
	// open alert auto-escalates to "esc" (the sweep flips it and re-fires
	// notifications). Armed when a record enters open/esc and escalate_after>0;
	// cleared (0) on ack/close and one-shot on escalation.
	EscalateAt int64 `json:"escalate_at,omitempty"`
	// ShelveUntil is the epoch-seconds deadline after which a time-boxed shelve
	// expires: the housekeeper's unshelve-timeout sweep reverts the record from
	// "shelved" back to "open" once now passes this. Stamped when a state→shelve
	// comment is posted (now + housekeeping.shelve_timeout); cleared (0) on
	// unshelve/open/close/ack. A zero (absent) value is the legacy permanent
	// shelve (ttl=-1) marker — the sweep's `shelve_until > 0` guard never
	// auto-unshelves those. Projected by recordToDoc.
	ShelveUntil int64 `json:"shelve_until,omitempty"`
	// EscalationCount is how many times this alert has been re-escalated during
	// its current lifecycle: 0 on a first delivery, 1 on the first
	// re-escalation, and so on. Incremented by every escalation producer (the
	// escalate-timeout sweep, a state→esc comment, and aggregaterule's
	// watchlist auto-re-escalation) and reset to 0 on close so a genuinely new
	// occurrence starts fresh. Notifiers branch on it to update an existing
	// ticket / thread instead of creating a second one; the zero value must
	// always mean "first fire" so records predating this field behave exactly
	// as before.
	EscalationCount int `json:"escalation_count,omitempty"`
	// EscalatedAt is the epoch-seconds instant the current escalation was
	// stamped. Zero when the alert has never been escalated.
	EscalatedAt int64 `json:"escalated_at,omitempty"`
	// EscalationReason names the producer of the current escalation:
	// "timeout" (escalate-timeout sweep), "manual" (operator via UI/chat), or
	// "watchlist" (aggregaterule field change). Empty when never escalated.
	EscalationReason string `json:"escalation_reason,omitempty"`
	// EscalationActor is the login of the operator who escalated, on a manual
	// escalation only. Empty otherwise.
	EscalationActor string `json:"escalation_actor,omitempty"`
	// Extra carries any plugin-injected fields (rule modifications, aggregaterule
	// counters, etc.) that don't have a typed home.
	Extra map[string]any `json:"-"`
}

// ListResponse is the canonical wire envelope for paginated list endpoints.
type ListResponse[T any] struct {
	Data []T  `json:"data"`
	Meta Meta `json:"meta"`
}

// Meta holds pagination metadata for list responses.
type Meta struct {
	Count  int `json:"count"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Total  int `json:"total"`
}

// ErrEnvelope is the canonical error response shape.
type ErrEnvelope struct {
	Error ErrBody `json:"error"`
}

// ErrBody carries the structured error detail inside an ErrEnvelope.
type ErrBody struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
	TraceID   string         `json:"trace_id,omitempty"`
}

// Claims are the JWT claims a logged-in user carries.
type Claims struct {
	Subject     string   `json:"sub"`
	Method      string   `json:"method"`
	TenantID    string   `json:"tenant_id,omitempty"` // tenant slug (D3); empty on legacy tokens
	Roles       []string `json:"roles,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
	Groups      []string `json:"groups,omitempty"`
	Issuer      string   `json:"iss,omitempty"`
	Audience    []string `json:"aud,omitempty"`
	ExpiresAt   int64    `json:"exp,omitempty"`
	NotBefore   int64    `json:"nbf,omitempty"`
	IssuedAt    int64    `json:"iat,omitempty"`
	ID          string   `json:"jti,omitempty"`
}

// typedRecordFields are the document keys Record decodes into typed struct
// fields. Everything else belongs in Extra.
//
// KEEP IN SYNC with the Record struct above: a typed field missing from this set
// is merely duplicated into Extra (harmless — projectors prefer the typed
// value), but a key wrongly listed here is DROPPED, which is not.
//
// NOTE: internal/core.knownRecordKeys serves the same purpose for the INGEST
// direction and is a separate, and currently staler, list (it predates
// acked_by / the timed-lifecycle deadlines / the escalation fields). Folding the
// two together would change what `_preserve_raw` archives, so it is deliberately
// left alone here; update both when adding a typed field.
var typedRecordFields = map[string]bool{
	"uid": true, "host": true, "source": true, "process": true,
	"severity": true, "message": true, "timestamp": true, "date_epoch": true,
	"ttl": true, "environment": true, "hash": true, "tags": true, "raw": true,
	"state": true, "acked_by": true, "plugins": true, "ack_until": true,
	"escalate_at": true, "shelve_until": true, "escalation_count": true,
	"escalated_at": true, "escalation_reason": true, "escalation_actor": true,
}

// RecordFromDocument projects a loose record document (as read back from a
// driver) into a typed Record, keeping the fields Record has no typed home for
// in Extra.
//
// Use this rather than a bare json.Unmarshal into a Record. Extra is
// `json:"-"`, so unmarshalling drops EVERY untyped field on the floor: the
// aggregaterule counters, the notification attribution, and — the one that bites
// — the `notify_ref_<action>` handles a notifier needs to find the ticket or
// chat thread it already created. A notifier handed a record projected without
// Extra sees no handle and creates a duplicate.
func RecordFromDocument(doc map[string]any) (Record, error) {
	var rec Record
	raw, err := json.Marshal(doc)
	if err != nil {
		return Record{}, err
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		return Record{}, err
	}
	extra := make(map[string]any, len(doc))
	for k, v := range doc {
		if typedRecordFields[k] {
			continue
		}
		// Driver bookkeeping (`_id`, `_old`) is not part of the record and must
		// not ride along into a notifier's outbound payload. Mirrors the
		// underscore-prefix convention the web inspector strips on.
		if len(k) > 0 && k[0] == '_' {
			continue
		}
		extra[k] = v
	}
	if len(extra) > 0 {
		rec.Extra = extra
	}
	return rec, nil
}
