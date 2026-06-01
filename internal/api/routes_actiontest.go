package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// actionTestTimeout caps a single test-send so a hung target can't wedge the
// request handler.
const actionTestTimeout = 15 * time.Second

// actionTestRequest is the POST /api/v1/action/test body: an (unsaved) action
// config = the notifier registry key + its action_form values.
type actionTestRequest struct {
	Selected   string         `json:"selected"`
	Subcontent map[string]any `json:"subcontent"`
}

// mountActionTest wires POST /api/v1/action/test. It is registered before the
// generic plugin CRUD mount so the static `/test` segment is never shadowed by
// the action plugin's `/{uid}` routes (those are GET/PUT/PATCH/DELETE anyway).
func (rt *Router) mountActionTest(r chi.Router) {
	r.Post("/api/v1/action/test", rt.handleActionTest)
}

// handleActionTest delivers one synthetic alert through the named notifier,
// reusing the exact Notifier.Send path the dispatcher uses. A 200 proves the
// real delivery path works; failures surface the upstream error as the
// envelope message so the UI can show why.
func (rt *Router) handleActionTest(w http.ResponseWriter, r *http.Request) {
	var req actionTestRequest
	if err := ParseJSONBody(r, &req); err != nil {
		WriteError(w, r, err)
		return
	}
	if req.Selected == "" {
		WriteError(w, r, ErrBadRequest.WithMessage("missing 'selected' notifier"))
		return
	}

	p := rt.Plugins[req.Selected]
	if p == nil {
		WriteError(w, r, ErrNotFound.WithMessage("unknown plugin: "+req.Selected))
		return
	}
	notifier, ok := p.(plugins.Notifier)
	if !ok {
		WriteError(w, r, ErrValidation.WithMessage("plugin is not a notifier: "+req.Selected))
		return
	}

	// Mirror notification.dispatch's metaFromSubcontent: clone subcontent and
	// stamp a synthetic action_name so notifiers that read it behave normally.
	meta := make(map[string]any, len(req.Subcontent)+1)
	for k, v := range req.Subcontent {
		meta[k] = v
	}
	meta["action_name"] = "__test__"

	payload := plugins.NotificationPayload{
		Template: req.Selected,
		Meta:     meta,
		// Inject is deliberately nil: a test must not mutate any stored record.
	}

	ctx, cancel := context.WithTimeout(r.Context(), actionTestTimeout)
	defer cancel()

	if err := notifier.Send(ctx, sampleTestRecord(), payload); err != nil {
		WriteError(w, r, ErrValidation.WithMessage(err.Error()))
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// sampleTestRecord is the representative synthetic alert used for test sends.
// time.Now() is fine here: this is an API handler, not a plugin core path, and
// the timestamp is cosmetic for the rendered message.
func sampleTestRecord() snoozetypes.Record {
	return snoozetypes.Record{
		Host:      "test-host.example.com",
		Source:    "snooze-test",
		Process:   "snooze",
		Severity:  "critical",
		Message:   "This is a test notification from Snooze",
		Timestamp: time.Now().UTC(),
		State:     "open",
		Tags:      []string{"test"},
	}
}
