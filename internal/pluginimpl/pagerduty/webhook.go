package pagerduty

// This file extends the outbound pagerduty notifier with an INBOUND
// status-sync webhook receiver. PagerDuty webhook v2 (`messages[]`) payloads
// posted to /api/v1/webhook/pagerduty are mapped to Snooze record state
// transitions: incident.acknowledge → "ack", incident.resolve → "close",
// trigger/unacknowledge/escalate → re-open (state cleared). The originating
// record is located by the dedup_key the outbound notifier set
// (rec.Hash, falling back to rec.UID), echoed back as data.incident.incident_key.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
)

// pdWebhookEnvelope is the top-level PagerDuty webhook v2 body: {"messages":[...]}.
type pdWebhookEnvelope struct {
	Messages []pdWebhookMessage `json:"messages"`
}

// pdWebhookMessage is a single message: {"type":"incident.acknowledge","data":{...}}.
type pdWebhookMessage struct {
	Type string `json:"type"`
	Data struct {
		Incident pdIncident `json:"incident"`
	} `json:"data"`
}

// pdIncident is the incident object inside a v2 webhook message. The
// incident_key (a.k.a. dedup_key) is what the outbound notifier set, so it is
// the lookup key for the originating Snooze record.
type pdIncident struct {
	IncidentKey    string `json:"incident_key"`
	IncidentNumber int    `json:"incident_number,omitempty"`
	HTMLURL        string `json:"html_url,omitempty"`
}

// mapEventType maps a PagerDuty webhook v2 event type to the Snooze state to
// apply. It returns:
//
//   - "ack"     for incident.acknowledge (suppresses notifications)
//   - "close"   for incident.resolve (closes the record)
//   - ""        for incident.trigger / unacknowledge / escalate (re-open)
//   - "ignored" for incident.assign / delegate and any unknown event type
//
// "ignored" is the fail-open sentinel: unknown events return 200 OK with no DB
// write so PagerDuty does not retry forever after a Snooze upgrade.
func mapEventType(t string) string {
	switch t {
	case "incident.acknowledge":
		return "ack"
	case "incident.resolve":
		return "close"
	case "incident.trigger", "incident.unacknowledge", "incident.escalate":
		return ""
	default:
		// incident.assign, incident.delegate, and any future/unknown type.
		return "ignored"
	}
}

// WebhookPath returns the route fragment for the inbound status-sync endpoint,
// mounted under /api/v1/webhook by mountWebhooks.
func (p *Plugin) WebhookPath() string { return "/pagerduty" }

// HandleWebhook accepts PagerDuty webhook v2 payloads and applies ack/close/
// re-open state transitions to matching Snooze records. It is a synchronous
// state-sync path: each message's incident_key locates an existing record
// (by hash, falling back to uid) and patches its state directly via the
// driver — no pipeline injection.
func (p *Plugin) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var env pdWebhookEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return
	}
	if len(env.Messages) == 0 {
		http.Error(w, "no messages in payload", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	driver := p.host.DB()

	updated := 0
	needed := 0 // count of non-ignored messages that required a record lookup
	for _, msg := range env.Messages {
		key := msg.Data.Incident.IncidentKey
		if key == "" {
			http.Error(w, "missing incident_key", http.StatusBadRequest)
			return
		}

		state := mapEventType(msg.Type)
		if state == "ignored" {
			// Fail-open no-op: no DB write, no record needed.
			continue
		}
		needed++

		// Locate the originating record: hash first (the outbound notifier's
		// preferred dedup_key), then uid.
		_, err := driver.GetOne(ctx, "record", db.Document{"hash": key})
		matchedByHash := true
		if errors.Is(err, db.ErrNotFound) {
			matchedByHash = false
			_, err = driver.GetOne(ctx, "record", db.Document{"uid": key})
		}
		if errors.Is(err, db.ErrNotFound) {
			// Both lookups missed this message; keep processing the batch.
			continue
		}
		if err != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}

		if err := p.applyState(ctx, driver, key, matchedByHash, state); err != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}
		updated++
	}

	// At least one message required a record but every lookup missed → the
	// caller's incident_key(s) match no Snooze record (404). An all-ignored
	// batch (needed == 0) is a successful no-op, not a 404.
	if needed > 0 && updated == 0 {
		http.Error(w, "no matching record", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"updated": updated,
	})
}

// applyState patches the located record's state. For a re-open (state == "")
// it clears the state field with UnsetFields — mirroring how the comment
// plugin clears acked_by — so the key is truly absent (omitempty/EXISTS stops
// matching) rather than persisted as an empty string. For a non-empty state it
// merges the new value with SetFields. The write is keyed on the same field the
// lookup matched (hash, else uid).
func (p *Plugin) applyState(ctx context.Context, driver db.Driver, key string, matchedByHash bool, state string) error {
	cond := condition.Equals("hash", key)
	if !matchedByHash {
		cond = condition.Equals("uid", key)
	}

	if state == "" {
		_, err := driver.UnsetFields(ctx, "record", []string{"state"}, cond)
		return err
	}
	_, err := driver.SetFields(ctx, "record", db.Document{"state": state}, cond)
	return err
}

// Compile-time proof we satisfy the WebhookReceiver contract in addition to the
// Notifier contract asserted in plugin.go.
var _ plugins.WebhookReceiver = (*Plugin)(nil)
