// routes_runs.go installs the two "runs" views behind the alert inspector:
//
//	GET /api/v1/notificationlog/runs?alert_uid=<uid>&limit&offset
//	GET /api/v1/comment/runs?record_uid=<uid>&limit&offset
//
// An alert that stays open for weeks and re-notifies every quarter of an hour
// leaves thousands of identical rows behind: in the delivery log, one dispatch
// per re-notification, and on the timeline one "New escalation" auto-comment
// each time. Listed raw they bury everything that is not a repeat. These
// routes fold consecutive repeats into one *run* — "×1,420, every ~16 min,
// Sep 17 → now" — and page over the runs, so a repeat spanning a hundred list
// pages is still one row.
//
// The fold needs every row of the alert (a run's length is a property of the
// whole history, not of a page), and the driver has no projection, so each
// request reads the alert's rows in one bounded Search: at most runScanCap,
// newest first. Past the cap the oldest run is marked truncated and the
// client says "since at least …". Both routes are scoped to ONE alert, which
// is what keeps the scan small; a notification's or an action's whole log is
// deliberately not offered here.
//
// Both mount before plugin CRUD (the static /runs segment must win over the
// generic /{uid} handlers) and are gated by the owning plugin's own
// authorization_policy, so they read exactly as the list next to them does.

package api

import (
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
)

// commentCollection is the timeline's collection (the comment plugin).
const commentCollection = "comment"

// runScanCap bounds the rows one runs request reads. Thirty days of a
// re-notification every 15 minutes with three actions is ~8,600 delivery rows.
const runScanCap = 10000

// runsDefaultLimit and runsMaxLimit bound a page of runs.
const (
	runsDefaultLimit = 10
	runsMaxLimit     = 100
)

// deliveryRun is one GET /notificationlog/runs entry: consecutive dispatches
// of the same notification, to the same actions, for the same alerts, that
// all succeeded. A dispatch with a failure is always a run of its own.
type deliveryRun struct {
	// Key is stable across polls while the run grows: its oldest dispatch.
	Key string `json:"key"`
	// Dispatches is how many times the notification fired in this run; Sends
	// the log rows (dispatches × actions).
	Dispatches int `json:"dispatches"`
	Sends      int `json:"sends"`
	// FirstEpoch and LastEpoch are the oldest and newest dispatch.
	FirstEpoch int64 `json:"first_epoch"`
	LastEpoch  int64 `json:"last_epoch"`
	// IntervalS is the median gap between consecutive dispatches; 0 for one.
	IntervalS int64 `json:"interval_s"`
	// Latest holds the log rows of the newest dispatch, verbatim — what the
	// timeline renders as the run's face.
	Latest []db.Document `json:"latest"`
	// Truncated: the run reaches the scan cap, so it may be longer.
	Truncated bool `json:"truncated"`
}

// deliveryRunsMeta sums up the alert's whole scanned log for the header.
type deliveryRunsMeta struct {
	Total      int   `json:"total"` // runs
	Sends      int   `json:"sends"` // log rows for the alert (the driver's count)
	Dispatches int   `json:"dispatches"`
	FirstEpoch int64 `json:"first_epoch"`
	LastEpoch  int64 `json:"last_epoch"`
	IntervalS  int64 `json:"interval_s"`
	Truncated  bool  `json:"truncated"`
}

type deliveryRunsResponse struct {
	Data []deliveryRun    `json:"data"`
	Meta deliveryRunsMeta `json:"meta"`
}

// commentRun is one GET /comment/runs entry: consecutive automatic comments
// of the same type and message. Anything written by a person is a run of one.
type commentRun struct {
	Key        string      `json:"key"`
	Count      int         `json:"count"`
	FirstEpoch int64       `json:"first_epoch"`
	LastEpoch  int64       `json:"last_epoch"`
	IntervalS  int64       `json:"interval_s"`
	Latest     db.Document `json:"latest"`
	Truncated  bool        `json:"truncated"`
}

type commentRunsMeta struct {
	Total     int  `json:"total"`    // runs
	Comments  int  `json:"comments"` // comments on the record (the driver's count)
	Truncated bool `json:"truncated"`
}

type commentRunsResponse struct {
	Data []commentRun    `json:"data"`
	Meta commentRunsMeta `json:"meta"`
}

// mountRuns wires both routes, each behind its plugin's own read policy. A
// plugin that is not loaded gets no route: there is nothing to fold.
func (rt *Router) mountRuns(r chi.Router) {
	if rt.DB == nil {
		return
	}
	for name, handler := range map[string]http.HandlerFunc{
		plugins.NotificationLogCollection: rt.handleDeliveryRuns,
		commentCollection:                 rt.handleCommentRuns,
	} {
		p, ok := rt.Plugins[name]
		if !ok {
			continue
		}
		// As MountCRUD does: the ro_/rw_<plugin> pair is derived from
		// PluginName, which the metadata does not carry on its own.
		meta := p.Metadata()
		meta.PluginName = p.Name()
		r.With(plugins.AuthorizeCRUD(meta)).Get("/api/v1/"+name+"/runs", handler)
	}
}

// runsPage reads ?limit and ?offset, defaulted and clamped.
func runsPage(r *http.Request) (limit, offset int, err error) {
	limit, offset = runsDefaultLimit, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if limit, err = strconv.Atoi(v); err != nil || limit < 1 {
			return 0, 0, fmt.Errorf("limit must be a positive integer")
		}
		limit = min(limit, runsMaxLimit)
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if offset, err = strconv.Atoi(v); err != nil || offset < 0 {
			return 0, 0, fmt.Errorf("offset must be a non-negative integer")
		}
	}
	return limit, offset, nil
}

// pageOf slices one page out of all.
func pageOf[T any](all []T, limit, offset int) []T {
	if offset >= len(all) {
		return []T{}
	}
	return all[offset:min(offset+limit, len(all))]
}

// scanNewest reads up to runScanCap rows of collection matching cond, newest
// first, and reports whether more exist.
func (rt *Router) scanNewest(r *http.Request, collection string, cond condition.Cond) ([]db.Document, int, bool, error) {
	docs, total, err := rt.DB.Search(r.Context(), collection, cond, db.Page{
		OrderBy: "date_epoch", PerPage: runScanCap + 1, PageNb: 1,
	})
	if err != nil {
		return nil, 0, false, err
	}
	truncated := len(docs) > runScanCap
	if truncated {
		docs = docs[:runScanCap]
	}
	if total < 0 {
		total = len(docs)
	}
	return docs, total, truncated, nil
}

// handleDeliveryRuns folds one alert's delivery log into runs.
func (rt *Router) handleDeliveryRuns(w http.ResponseWriter, r *http.Request) {
	uid := r.URL.Query().Get("alert_uid")
	if uid == "" {
		WriteError(w, r, ErrBadRequest.WithMessage("alert_uid is required"))
		return
	}
	limit, offset, err := runsPage(r)
	if err != nil {
		WriteError(w, r, ErrBadRequest.WithMessage(err.Error()))
		return
	}
	docs, total, truncated, err := rt.scanNewest(r, plugins.NotificationLogCollection,
		condition.Cond{Op: condition.OpContains, Field: "alert_uids", Value: uid})
	if err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	runs, meta := foldDeliveryRuns(docs, truncated)
	meta.Sends = total
	WriteJSON(w, http.StatusOK, deliveryRunsResponse{Data: pageOf(runs, limit, offset), Meta: meta})
}

// handleCommentRuns folds one record's timeline into runs.
func (rt *Router) handleCommentRuns(w http.ResponseWriter, r *http.Request) {
	uid := r.URL.Query().Get("record_uid")
	if uid == "" {
		WriteError(w, r, ErrBadRequest.WithMessage("record_uid is required"))
		return
	}
	limit, offset, err := runsPage(r)
	if err != nil {
		WriteError(w, r, ErrBadRequest.WithMessage(err.Error()))
		return
	}
	docs, total, truncated, err := rt.scanNewest(r, commentCollection, condition.Equals("record_uid", uid))
	if err != nil {
		WriteError(w, r, ErrInternal.WithCause(err))
		return
	}
	runs := foldCommentRuns(docs, truncated)
	WriteJSON(w, http.StatusOK, commentRunsResponse{
		Data: pageOf(runs, limit, offset),
		Meta: commentRunsMeta{Total: len(runs), Comments: total, Truncated: truncated},
	})
}

// dispatch is the rows one notification firing wrote for one alert set: the
// server-side twin of web/…/deliveries/group.ts, keyed identically so a run's
// Latest renders as exactly one dispatch row there.
type dispatch struct {
	key       string
	signature string
	epoch     int64
	rows      []db.Document
}

// foldDeliveryRuns groups rows (newest first) into dispatches, then folds
// each notification's consecutive dispatches with the same signature into
// runs.
func foldDeliveryRuns(rows []db.Document, truncated bool) ([]deliveryRun, deliveryRunsMeta) {
	byKey := map[string]*dispatch{}
	var order []*dispatch
	for _, row := range rows {
		key := dispatchKey(row)
		d := byKey[key]
		if d == nil {
			d = &dispatch{key: key}
			byKey[key] = d
			order = append(order, d)
		}
		d.rows = append(d.rows, row)
		d.epoch = max(d.epoch, docInt64(row["date_epoch"]))
	}
	// First-seen order is already newest first; a stable sort only guards
	// against a backend whose tie order puts an older dispatch ahead.
	sort.SliceStable(order, func(i, j int) bool { return order[i].epoch > order[j].epoch })
	for i, d := range order {
		d.signature = runSignature(d, i)
	}

	meta := deliveryRunsMeta{Dispatches: len(order), Truncated: truncated}
	if len(order) > 0 {
		meta.LastEpoch = order[0].epoch
		meta.FirstEpoch = order[len(order)-1].epoch
		meta.IntervalS = medianGap(dispatchEpochs(order))
	}

	// Fold per notification: two notifications routing the same alert fire
	// interleaved, and folding the merged stream would alternate A, B, A, B
	// and never form a run. Each notification's own stream is folded, then
	// the runs are merged newest first.
	streams := map[string][]*dispatch{}
	var streamOrder []string
	for _, d := range order {
		k := notificationKey(d.rows[0])
		if _, seen := streams[k]; !seen {
			streamOrder = append(streamOrder, k)
		}
		streams[k] = append(streams[k], d)
	}
	var runs []deliveryRun
	for _, k := range streamOrder {
		stream := streams[k]
		markFirstReferences(stream)
		for start := 0; start < len(stream); {
			end := start + 1
			for end < len(stream) && stream[end].signature == stream[start].signature {
				end++
			}
			members := stream[start:end]
			run := deliveryRun{
				Key:        members[len(members)-1].key,
				Dispatches: len(members),
				FirstEpoch: members[len(members)-1].epoch,
				LastEpoch:  members[0].epoch,
				IntervalS:  medianGap(dispatchEpochs(members)),
				Latest:     members[0].rows,
				Truncated:  truncated && end == len(stream),
			}
			for _, m := range members {
				run.Sends += len(m.rows)
			}
			runs = append(runs, run)
			start = end
		}
	}
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].LastEpoch > runs[j].LastEpoch })
	meta.Total = len(runs)
	if runs == nil {
		runs = []deliveryRun{}
	}
	return runs, meta
}

// dispatchKey mirrors group.ts's groupKey: notification identity, the
// dispatch's queue time, escalation, batch flag and alert set.
func dispatchKey(row db.Document) string {
	when := docInt64(row["queued_epoch"])
	if when == 0 {
		when = docInt64(row["date_epoch"])
	}
	esc := fmt.Sprintf("%d/%s", docInt64(row["escalation_count"]), docString(row["escalation_reason"]))
	return strings.Join([]string{notificationKey(row), strconv.FormatInt(when, 10), esc, batchFlag(row), alertSetKey(row)}, "|")
}

// runSignature is what two dispatches of one notification must share to sit
// in one run: the same alerts, batch flag, action set and escalation kind — and no
// failure. A dispatch with a failed send gets a signature of its own (the
// position makes it unique), so a failure always stands alone; so does one
// that produced a new ticket reference (see markFirstReferences).
func runSignature(d *dispatch, position int) string {
	actions := make([]string, 0, len(d.rows))
	for _, row := range d.rows {
		if docString(row["status"]) == "error" {
			return fmt.Sprintf("failed#%d", position)
		}
		name := docString(row["action"])
		if name == "" {
			name = docString(row["notifier"])
		}
		if !slices.Contains(actions, name) {
			actions = append(actions, name)
		}
	}
	sort.Strings(actions)
	first := d.rows[0]
	escalated := docInt64(first["escalation_count"]) > 0
	return strings.Join([]string{
		alertSetKey(first), batchFlag(first),
		strings.Join(actions, "\x1f"),
		strconv.FormatBool(escalated), docString(first["escalation_reason"]),
	}, "|")
}

// markFirstReferences gives a dispatch its own run when one of its sends
// carries a ticket reference that action had not produced before in this
// stream: the Jira action that opened AD-775 is the one dispatch of hundreds
// worth finding, and folded into the run around it its link was hidden behind
// the run's newest (link-less) dispatch. Later dispatches repeating the same
// reference still fold. stream is newest first, so it is walked oldest first.
func markFirstReferences(stream []*dispatch) {
	seen := map[string]bool{}
	for i := len(stream) - 1; i >= 0; i-- {
		d := stream[i]
		first := false
		for _, row := range d.rows {
			id := refIdentity(row)
			if id == "" {
				continue
			}
			key := docString(row["action"]) + "\x1f" + id
			if !seen[key] {
				seen[key] = true
				first = true
			}
		}
		if first {
			d.signature = "ref#" + d.key
		}
	}
}

// refIdentity names the external artifact a send points at — its ticket key,
// else its link — or "" for none. Plumbing refs (a chat thread's message ids)
// carry neither and never count: they identify nothing an operator opens.
func refIdentity(row db.Document) string {
	ref, _ := row["ref"].(map[string]any)
	if k := docString(ref["issue_key"]); k != "" {
		return k
	}
	return docString(ref["url"])
}

func notificationKey(row db.Document) string {
	if k := joinSorted(row["notification_uids"]); k != "" {
		return k
	}
	return joinSorted(row["notification_names"])
}

func alertSetKey(row db.Document) string {
	if k := joinSorted(row["alert_hashes"]); k != "" {
		return "h:" + k
	}
	if k := joinSorted(row["alert_uids"]); k != "" {
		return "u:" + k
	}
	return "n:" + strconv.FormatInt(docInt64(row["alert_count"]), 10)
}

func batchFlag(row db.Document) string {
	if b, _ := row["batch"].(bool); b {
		return "b"
	}
	return ""
}

// foldCommentRuns folds consecutive automatic comments (newest first) with
// the same type and message.
func foldCommentRuns(rows []db.Document, truncated bool) []commentRun {
	runs := []commentRun{}
	for start := 0; start < len(rows); {
		end := start + 1
		if sig, ok := autoCommentSignature(rows[start]); ok {
			for end < len(rows) {
				next, ok := autoCommentSignature(rows[end])
				if !ok || next != sig {
					break
				}
				end++
			}
		}
		members := rows[start:end]
		epochs := make([]int64, len(members))
		for i, m := range members {
			epochs[i] = docInt64(m["date_epoch"])
		}
		oldest := members[len(members)-1]
		runs = append(runs, commentRun{
			Key:        docString(oldest["uid"]),
			Count:      len(members),
			FirstEpoch: epochs[len(epochs)-1],
			LastEpoch:  epochs[0],
			IntervalS:  medianGap(epochs),
			Latest:     members[0],
			Truncated:  truncated && end == len(rows),
		})
		start = end
	}
	return runs
}

// autoCommentSignature is (type, message) of an automatic comment; ok is
// false for anything a person wrote, which never folds.
func autoCommentSignature(c db.Document) (string, bool) {
	if auto, _ := c["auto"].(bool); !auto {
		return "", false
	}
	return docString(c["type"]) + "\x1f" + docString(c["message"]), true
}

func dispatchEpochs(ds []*dispatch) []int64 {
	out := make([]int64, len(ds))
	for i, d := range ds {
		out[i] = d.epoch
	}
	return out
}

// medianGap is the median distance between consecutive epochs (sorted
// newest first), 0 with fewer than two. The median, not the mean: one pause
// of a week inside a quarter-hourly run must not read as "every 3 hours".
func medianGap(epochs []int64) int64 {
	if len(epochs) < 2 {
		return 0
	}
	gaps := make([]int64, 0, len(epochs)-1)
	for i := 1; i < len(epochs); i++ {
		gaps = append(gaps, epochs[i-1]-epochs[i])
	}
	slices.Sort(gaps)
	return gaps[len(gaps)/2]
}

// joinSorted joins a document's string array sorted, "" when absent.
func joinSorted(v any) string {
	var out []string
	switch arr := v.(type) {
	case []any:
		for _, x := range arr {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	case []string:
		out = append(out, arr...)
	}
	sort.Strings(out)
	return strings.Join(out, "\x1f")
}

func docString(v any) string {
	s, _ := v.(string)
	return s
}

// docInt64 reads a numeric field whatever the backend decoded it as.
func docInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	}
	return 0
}
