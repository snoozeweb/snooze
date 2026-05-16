package relp

import (
	"context"
	"fmt"

	"github.com/snoozeweb/snooze/internal/components/syslog"
	"github.com/snoozeweb/snooze/pkg/snoozeclient"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// Forwarder converts RELP syslog payloads into snoozetypes.Record values and
// POSTs them via snoozeclient.Client. It wraps the syslog package's parser
// and mapper so RELP records are indistinguishable on the wire from
// plain-syslog ones, except for the Source field which is "relp".
type Forwarder struct {
	client *snoozeclient.Client
	parser *syslog.MessageParser
}

// NewForwarder builds a Forwarder from a logged-in client and a parser mode
// (one of "auto", "rfc3164", "rfc5424"). NewForwarder validates the mode by
// calling syslog.NewParser; an invalid mode is returned as an error.
func NewForwarder(c *snoozeclient.Client, parserMode string) (*Forwarder, error) {
	p, err := syslog.NewParser(parserMode)
	if err != nil {
		return nil, fmt.Errorf("relp: build parser: %w", err)
	}
	return &Forwarder{client: c, parser: p}, nil
}

// Forward parses payload as a syslog line, maps it to a Record (with
// Source="relp") and POSTs it. peer is the remote IP[:port] reported by the
// listener; it ends up in Record.Raw["peer"].
//
// A parse error short-circuits before the HTTP request so the caller can
// distinguish "bad payload" (NACK-and-drop) from "downstream offline"
// (NACK-and-retry).
func (f *Forwarder) Forward(ctx context.Context, payload []byte, peer string) error {
	if f.client == nil {
		return fmt.Errorf("relp: forwarder has no client configured")
	}
	msg, err := f.parser.Parse(payload)
	if err != nil {
		return fmt.Errorf("relp: parse payload: %w", err)
	}
	rec := toRelpRecord(msg, peer)
	if _, err := f.client.PostAlert(ctx, rec); err != nil {
		return fmt.Errorf("relp: post alert: %w", err)
	}
	return nil
}

// toRelpRecord is the package-local mapping helper. It delegates the bulk of
// the conversion to syslog.ToRecord and then overrides Source so consumers
// (rules, dashboards) can distinguish RELP-delivered alerts from plain
// syslog.
func toRelpRecord(msg syslog.ParsedMessage, peer string) snoozetypes.Record {
	rec := syslog.ToRecord(msg, peer)
	rec.Source = "relp"
	return rec
}
