package plugins

import (
	"context"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

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
	// AfterPersist are side effects that describe this pass's change to the
	// stored record — aggregaterule's lifecycle comments ("Auto re-opened",
	// "Auto closed") are the canonical case. The pipeline runs them, in plugin
	// order, only once the record has actually been written; if a later plugin
	// discards the record (ActionAbort, including a Filter on the
	// abort-and-persist path) they are dropped, so the timeline never narrates a
	// transition that was not stored. Each runs under the pipeline's
	// tenant-scoped context and must be best-effort: it cannot fail the record.
	AfterPersist []func(context.Context)
	// Release is meaningful only from Filter.Filter, on the abort-and-persist
	// path: the plugin that held the record (aggregaterule's throttle, in
	// practice) was holding an alert this filter had silenced, and that
	// silence is over. A held duplicate inside a throttle window that opened
	// on a silenced — never notified — occurrence would otherwise stay
	// un-notified for the rest of the window. With Release the record resumes
	// the ordinary loop right after the filter, so the plugins behind it
	// (notification) run, and it is written with pass-through semantics, so
	// the throttle window restarts from that first real notification.
	// Action must be ActionContinue. Ignored everywhere else.
	Release bool
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
	// Inject is an optional callback the notifier may invoke to stamp a
	// field onto the originating record. Used by webhook's `inject_response`
	// to write the parsed HTTP response into `response_<action_name>`.
	// nil for notifiers that don't need it; calls on a nil pointer are
	// no-ops via the InjectField helper.
	Inject InjectFunc
	// Escalation describes whether this is a first delivery or a
	// re-escalation of an alert the notifier has already delivered. The zero
	// value means first delivery, so a caller that doesn't populate it gets
	// pre-escalation behaviour. See escalation.go.
	Escalation Escalation
	// NotificationUID is the uid of the notification entry that routed this
	// alert to the action. Populated by the notification dispatcher; empty for
	// out-of-band senders (the action-test endpoint, CLI probes).
	//
	// Batching notifiers need it: they queue a delivery member at Send time
	// and report the whole bucket at flush, by which point the dispatcher's
	// per-send scope is long gone. The uid is what every UI filter keys on
	// (names are not unique per tenant), so a member without it cannot be
	// attributed to its notification.
	NotificationUID string
	// Test marks an out-of-band probe send: POST /api/v1/action/test asking
	// "does this action config actually work?" with a synthetic alert.
	//
	// A test send must be REAL (it proves the transport works) but must leave
	// no trace: batching notifiers deliver it immediately instead of dropping
	// a phantom alert into a live bucket, and nothing writes a delivery-history
	// row, a counter bump or an action_success/action_error stat for it.
	// False for every dispatcher-originated send.
	Test bool
}

// ActionName returns the stored action's name, which the dispatcher stamps into
// Meta as `action_name`. Notifiers key their persisted external handle on it
// (see NotifyRef / StoreNotifyRef), so two actions pointing at the same
// notifier keep independent handles.
func (p NotificationPayload) ActionName() string {
	if p.Meta == nil {
		return ""
	}
	s, _ := p.Meta["action_name"].(string)
	return s
}

// InjectFunc writes one field onto the originating record. The dispatcher
// builds a closure that calls DB.UpdateOne against the record's collection.
// Errors are logged at the call site; the notifier doesn't need to handle
// them.
type InjectFunc func(field string, value any)

// InjectField is the nil-safe call helper. Notifiers should use this rather
// than dereferencing payload.Inject directly so that a nil callback (the
// default for non-dispatcher callers, e.g. tests) is silently ignored.
func InjectField(fn InjectFunc, field string, value any) {
	if fn == nil {
		return
	}
	fn(field, value)
}

// ActionOpts carries per-invocation options for Action.Execute.
type ActionOpts struct {
	// Form is the user-filled action form (see metadata.action_form).
	Form map[string]any
	// Batch is true when the action runs against a coalesced set of records.
	Batch bool
}
