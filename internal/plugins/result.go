package plugins

import "github.com/snoozeweb/snooze/pkg/snoozetypes"

// Action discriminates the four terminal verdicts a Processor.Process call
// can produce. The pipeline interprets each as follows:
//
//   - ActionContinue: hand the (possibly mutated) record to the next plugin.
//   - ActionAbort: stop processing; do not persist.
//   - ActionAbortWrite: stop processing; persist with a fresh updated_at.
//   - ActionAbortUpdate: stop processing; persist without bumping updated_at.
type Action int

// Pipeline verdicts emitted by Processor plugins.
const (
	ActionContinue Action = iota
	ActionAbort
	ActionAbortWrite
	ActionAbortUpdate
)

// String returns the lowercase wire form of the action (matches the
// `snooze_alert_hit_total` metric label).
func (a Action) String() string {
	switch a {
	case ActionContinue:
		return "continue"
	case ActionAbort:
		return "abort"
	case ActionAbortWrite:
		return "abort_write"
	case ActionAbortUpdate:
		return "abort_update"
	default:
		return "unknown"
	}
}

// Result is the value returned by Processor.Process.
type Result struct {
	// Action is the verdict for this plugin's pass over the record.
	Action Action
	// Record carries the record forward; for ActionContinue this is the
	// record the next plugin sees.
	Record snoozetypes.Record
}

// NotificationPayload is the rendered content a Notifier consumes.
type NotificationPayload struct {
	// Template is the canonical template identifier (e.g. "mail/default").
	Template string
	// Subject is the rendered subject line.
	Subject string
	// Body is the rendered body. Notifiers decide whether to interpret it
	// as plain-text or HTML based on Meta hints.
	Body string
	// Meta holds notifier-specific knobs (e.g. mime type, priority).
	Meta map[string]any
}

// ActionOpts carries per-invocation options for Action.Execute.
type ActionOpts struct {
	// Form is the user-filled action form (see metadata.action_form).
	Form map[string]any
	// Batch is true when the action runs against a coalesced set of records.
	Batch bool
}
