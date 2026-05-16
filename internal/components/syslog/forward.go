package syslog

import (
	"context"
	"fmt"
	"time"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// Forwarder wraps a snoozeclient.Client and converts ParsedMessage values into
// the wire-shape snoozetypes.Record before POSTing them via PostAlert.
type Forwarder struct {
	client *snoozeclient.Client
}

// NewForwarder returns a Forwarder bound to c. The client must already be
// configured for login; the daemon calls Login during startup.
func NewForwarder(c *snoozeclient.Client) *Forwarder {
	return &Forwarder{client: c}
}

// Forward maps msg to a Record and POSTs it to /api/v1/alerts. peer is the
// remote IP[:port] reported by the listener — it is stored in Record.Raw under
// "peer" so operators can correlate alerts with their source after the fact.
func (f *Forwarder) Forward(ctx context.Context, msg ParsedMessage, peer string) error {
	if f.client == nil {
		return fmt.Errorf("syslog: forwarder has no client configured")
	}
	rec := ToRecord(msg, peer)
	if _, err := f.client.PostAlert(ctx, rec); err != nil {
		return fmt.Errorf("syslog: post alert: %w", err)
	}
	return nil
}

// ToRecord converts a ParsedMessage into the canonical snoozetypes.Record.
// Exposed (not unexported) so tests can assert the mapping directly without
// spinning up a Forwarder.
func ToRecord(msg ParsedMessage, peer string) snoozetypes.Record {
	process := msg.AppName
	if process == "" {
		// RFC3164 doesn't expose Appname separately; leodido fills AppName
		// from the TAG field, but legacy senders sometimes leave it empty.
		// Fall back to ProcID for those cases.
		process = msg.ProcID
	}
	ts := msg.Timestamp
	if !msg.HasTime {
		ts = time.Now().UTC()
	}
	raw := map[string]any{
		"format":   msg.Format,
		"facility": msg.Facility,
		"original": msg.Raw,
	}
	if peer != "" {
		raw["peer"] = peer
	}
	if msg.MsgID != "" {
		raw["msgid"] = msg.MsgID
	}
	if msg.ProcID != "" {
		raw["procid"] = msg.ProcID
	}
	if msg.Structured != nil {
		raw["structured_data"] = msg.Structured
	}

	return snoozetypes.Record{
		Host:      msg.Hostname,
		Source:    "syslog",
		Process:   process,
		Severity:  msg.Severity,
		Message:   msg.Message,
		Timestamp: ts,
		Raw:       raw,
	}
}
