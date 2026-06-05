// Package snoozetypes contains wire types shared between the server, CLI, SDK, and components.
package snoozetypes

import "time"

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
	Hash    string         `json:"hash,omitempty"`
	Tags    []string       `json:"tags,omitempty"`
	Raw     map[string]any `json:"raw,omitempty"`
	State   string         `json:"state,omitempty"`
	Plugins []string       `json:"plugins,omitempty"`
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
