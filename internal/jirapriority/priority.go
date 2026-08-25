// Package jirapriority resolves a Snooze severity to a JIRA priority without
// ever hardcoding a priority *name*.
//
// Priority names are localized per JIRA instance ("Critique"/"Grave"/"Moyen"
// on a French Cloud site, "Highest"/"High"/"Medium" on an English one) and
// renamable by any admin, so a name-keyed mapping baked into Snooze is wrong
// on arrival for most sites — the create call fails with
// `400 priority: the selected priority is invalid` and no ticket is opened.
// Priority *ids* are stable, and JIRA returns the scheme ordered most-severe
// first. This package therefore maps a severity to a *position* in the live
// scheme and sends the id at that position.
//
// The scheme is discovered at runtime (createmeta for the target project and
// issue type, falling back to the global priority list) and held in a Cache
// so it costs one request per project, not one per alert.
package jirapriority

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// Priority is one entry of a JIRA priority scheme.
type Priority struct {
	ID   string
	Name string
}

// Scheme is a JIRA priority scheme ordered most severe first — the order JIRA
// itself returns from both /priority and createmeta's allowedValues.
type Scheme []Priority

// maxSeverityRank is the least-severe rank in snoozetypes.DefaultSeverityRank
// ("ok" = 8). Severity ranks are spread across the scheme's positions in
// proportion to this span, so a 4-entry scheme and the JIRA-default 5-entry
// one both get a sensible spread instead of an off-the-end index.
const maxSeverityRank = 8

// FindByID returns the entry with the given id.
func (s Scheme) FindByID(id string) (Priority, bool) {
	id = strings.TrimSpace(id)
	for _, p := range s {
		if p.ID == id {
			return p, true
		}
	}
	return Priority{}, false
}

// FindByName returns the entry whose name matches, case- and space-insensitively.
func (s Scheme) FindByName(name string) (Priority, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Priority{}, false
	}
	for _, p := range s {
		if strings.EqualFold(strings.TrimSpace(p.Name), name) {
			return p, true
		}
	}
	return Priority{}, false
}

// ForSeverity maps a Snooze severity onto a position in the scheme using the
// canonical severity ladder (snoozetypes.DefaultSeverityRank, 0 = most
// severe). An unranked severity returns false: the caller should then omit the
// priority field entirely and let JIRA apply the scheme's own default rather
// than guess.
func (s Scheme) ForSeverity(severity string) (Priority, bool) {
	if len(s) == 0 {
		return Priority{}, false
	}
	rank, ok := snoozetypes.SeverityRank(severity)
	if !ok {
		return Priority{}, false
	}
	if len(s) == 1 {
		return s[0], true
	}
	// Proportional placement, rounded to nearest: rank 0 → first entry,
	// maxSeverityRank → last entry. Integer arithmetic with a +half term
	// does the rounding without floats.
	span := len(s) - 1
	idx := (rank*span + maxSeverityRank/2) / maxSeverityRank
	if idx >= len(s) {
		idx = len(s) - 1
	}
	return s[idx], true
}

// Resolution explains how a priority was picked. It exists so callers can log
// the decision once per project instead of on every alert.
type Resolution struct {
	Priority Priority
	// Source is "override-id", "override-name", "severity" or "none".
	Source string
}

// Resolve picks a priority for severity, honouring an operator override when
// one resolves against the live scheme.
//
// Precedence:
//
//  1. override matching an id in the scheme (e.g. priority_mapping value "3")
//  2. override matching a name in the scheme, case-insensitively
//  3. positional mapping from the severity ladder
//  4. nothing — the caller omits the field and JIRA applies its default
//
// A non-resolving override deliberately falls through to (3) rather than
// failing the create: an English default mapping against a French site should
// still open the ticket.
func (s Scheme) Resolve(override, severity string) Resolution {
	if o := strings.TrimSpace(override); o != "" {
		if p, ok := s.FindByID(o); ok {
			return Resolution{Priority: p, Source: "override-id"}
		}
		if p, ok := s.FindByName(o); ok {
			return Resolution{Priority: p, Source: "override-name"}
		}
	}
	if p, ok := s.ForSeverity(severity); ok {
		return Resolution{Priority: p, Source: "severity"}
	}
	return Resolution{Source: "none"}
}

// Field renders the resolved priority as the JIRA `fields.priority` value, or
// nil when nothing resolved (omit the key entirely in that case). The id form
// is the only one sent — see the package doc.
func (r Resolution) Field() map[string]any {
	if r.Priority.ID == "" {
		return nil
	}
	return map[string]any{"id": r.Priority.ID}
}

// ---------------------------------------------------------------------------
// Wire parsing
// ---------------------------------------------------------------------------

// createMetaResponse is the shape of
// GET /issue/createmeta?projectKeys=…&issuetypeIds=…&expand=projects.issuetypes.fields.
type createMetaResponse struct {
	Projects []struct {
		IssueTypes []struct {
			ID     string `json:"id"`
			Fields struct {
				Priority struct {
					AllowedValues []struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"allowedValues"`
				} `json:"priority"`
			} `json:"fields"`
		} `json:"issuetypes"`
	} `json:"projects"`
}

// ParseCreateMeta extracts the ordered priority scheme from a createmeta
// response. issueTypeID narrows the search when the response covers several
// issue types; pass "" to accept the first issue type that carries a priority
// field. Returns an empty scheme (no error) when the priority field is simply
// not on the create screen — callers then fall back to the global list.
func ParseCreateMeta(raw []byte, issueTypeID string) (Scheme, error) {
	var resp createMetaResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("jirapriority: decode createmeta: %w", err)
	}
	var fallback Scheme
	for _, prj := range resp.Projects {
		for _, it := range prj.IssueTypes {
			vals := it.Fields.Priority.AllowedValues
			if len(vals) == 0 {
				continue
			}
			sch := make(Scheme, 0, len(vals))
			for _, v := range vals {
				if v.ID != "" {
					sch = append(sch, Priority{ID: v.ID, Name: v.Name})
				}
			}
			if issueTypeID != "" && it.ID == issueTypeID {
				return sch, nil
			}
			if fallback == nil {
				fallback = sch
			}
		}
	}
	return fallback, nil
}

// wirePriority is one entry of /priority or /priority/search.
type wirePriority struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ParsePriorityList parses GET /rest/api/3/priority (a bare ordered array) or
// its paginated sibling /priority/search ({"values": [...]}).
func ParsePriorityList(raw []byte) (Scheme, error) {
	var flat []wirePriority
	if err := json.Unmarshal(raw, &flat); err == nil {
		return schemeFrom(flat), nil
	}
	var paged struct {
		Values []wirePriority `json:"values"`
	}
	if err := json.Unmarshal(raw, &paged); err != nil {
		return nil, fmt.Errorf("jirapriority: decode priority list: %w", err)
	}
	return schemeFrom(paged.Values), nil
}

func schemeFrom(entries []wirePriority) Scheme {
	out := make(Scheme, 0, len(entries))
	for _, e := range entries {
		if e.ID != "" {
			out = append(out, Priority{ID: e.ID, Name: e.Name})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Cache
// ---------------------------------------------------------------------------

// DefaultTTL is how long a fetched scheme is trusted. Priority schemes change
// about never; the TTL exists so an admin's edit lands without a restart.
const DefaultTTL = time.Hour

// Fetch loads a scheme from JIRA. It is supplied by the caller so this package
// stays free of HTTP and of any particular client type.
type Fetch func(ctx context.Context) (Scheme, error)

// Cache memoizes schemes per key (typically "PROJECT/issuetype"). It is safe
// for concurrent use; concurrent misses on the same key fetch once.
type Cache struct {
	ttl time.Duration
	now func() time.Time

	mu      sync.Mutex
	entries map[string]*entry
}

// entry holds one key's cached scheme plus the lock that serializes its
// fetches. The per-entry lock means a slow fetch for one project doesn't stall
// lookups for another.
type entry struct {
	mu     sync.Mutex
	scheme Scheme
	loaded time.Time
}

// NewCache builds a Cache. A ttl <= 0 means DefaultTTL.
func NewCache(ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Cache{ttl: ttl, now: time.Now, entries: map[string]*entry{}}
}

// Get returns the cached scheme for key, calling fetch on a miss or once the
// TTL has expired. A fetch error is returned as-is and nothing is cached, so
// the next alert retries.
//
// A nil Cache is valid and simply doesn't cache — callers constructed without
// one (bare structs in tests) still resolve correctly.
func (c *Cache) Get(ctx context.Context, key string, fetch Fetch) (Scheme, error) {
	if c == nil {
		return fetch(ctx)
	}
	e := c.entryFor(key)
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.scheme) > 0 && c.now().Sub(e.loaded) < c.ttl {
		return e.scheme, nil
	}
	sch, err := fetch(ctx)
	if err != nil {
		return nil, err
	}
	if len(sch) == 0 {
		return nil, fmt.Errorf("jirapriority: empty priority scheme for %q", key)
	}
	e.scheme = sch
	e.loaded = c.now()
	return sch, nil
}

// Invalidate drops the cached scheme for key so the next Get refetches. Call
// it when JIRA rejects a priority the cache said was valid — an admin has
// edited the scheme underneath us.
func (c *Cache) Invalidate(key string) {
	if c == nil {
		return
	}
	e := c.entryFor(key)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.scheme = nil
	e.loaded = time.Time{}
}

func (c *Cache) entryFor(key string) *entry {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		e = &entry{}
		c.entries[key] = e
	}
	return e
}
