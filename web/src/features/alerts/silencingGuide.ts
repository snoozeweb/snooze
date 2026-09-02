import type { ActionType } from "./ActionDialog";

/**
 * The four ways to make an alert stop bothering you — Close, Snooze, Shelve,
 * Shelve permanently — plus Unshelve, described once and reused everywhere:
 * the row kebab, the right-click menu, the ShelveDialog cross-link and the
 * "Close, snooze or shelve?" How-to dialog.
 *
 * Every sentence below is a statement about what the Go server actually does,
 * not what the verb sounds like. The load-bearing citations:
 *
 *  - Close → `state="close"`; the notification plugin skips `ack`/`close`
 *    (internal/pluginimpl/notification/plugin.go:292-296), so a closed record
 *    is quiet. But the next hit on the same aggregate AUTO RE-OPENS the same
 *    row — it does not create a second one — and carries on down the pipeline
 *    to `notification` (internal/pluginimpl/aggregaterule/plugin.go:536-550,
 *    with rec["uid"] = prevUID at :459 and an upsert on uid at
 *    internal/core/pipeline.go:163-166). So Close means "quiet until it
 *    happens again", not "quiet".
 *  - Snooze → a stored condition, evaluated against every INCOMING record
 *    (internal/pluginimpl/snooze/plugin.go:205-231); it aborts before the
 *    notification plugin, so matches never page. It tags `snoozed` with the
 *    rule name (:210-213) rather than touching `state`. Existing rows are
 *    untouched unless the operator explicitly retro-applies
 *    (internal/api/routes_snooze_retro.go:84-100). The kebab item does not
 *    create anything: it navigates to the snooze editor with the host+message
 *    condition prefilled (see `snoozeRows` in AlertsPage).
 *  - Shelve → `state="shelved"` plus a `shelve_until` deadline
 *    (internal/pluginimpl/comment/transition.go:44-47,
 *    internal/pluginimpl/comment/plugin.go:256-267); a housekeeper sweep flips
 *    it back to `open` when the deadline passes
 *    (internal/housekeeper/jobs_default.go:344-383). The deadline is
 *    `now + duration` from the window the operator picked in ShelveDialog, and
 *    only falls back to the configured `housekeeping.shelve_timeout` when the
 *    comment names no duration — so the copy may promise the chosen window.
 *    Note what it does NOT do: `notification.Process` skips only `ack` and
 *    `close`, so a shelved record that recurs still notifies. Shelving is "get
 *    it off my screen", not "stop paging me" — that is Snooze's job, and the
 *    copy says so.
 *  - Shelve permanently → a different mechanism entirely: `ttl` flipped
 *    negative, `state` untouched. No sweep returns it (the unshelve sweep
 *    guards on `shelve_until > 0`, jobs_default.go:339-341) and a negative ttl
 *    is also exempt from cleanup (internal/db/sqlite/cleanup.go:21-23,44-45).
 *    Nothing on the server ever undoes it — but Unshelve does, by flipping the
 *    ttl back positive (see the Unshelve note below), so it is reversible by
 *    hand and the guide says so.
 *  - Unshelve → `state="open"`, unconditionally — never back to whatever the
 *    record was before it was shelved; there is no stored previous state
 *    (internal/pluginimpl/comment/transition.go:46, plugin.go:195-198,268-270).
 *    The comment only touches `state`/`shelve_until`, so the kebab item ALSO
 *    PATCHes a negative `ttl` back to its positive magnitude when the row has
 *    one (`rowActions` in AlertsPage). Without that second call a permanently
 *    shelved row stayed hidden after "Unshelve": the Shelved tab and the
 *    default Alerts preset filter on `ttl < 0` as well as on the state
 *    (tabs.ts).
 */

/** Kebab / context-menu second lines. Kept under ~70 characters: they are a
 *  hint under a label, not documentation — the How-to dialog is the long form. */
export const SILENCE_DESCRIPTIONS = {
  close: "Mark it resolved. A new matching alert re-opens this same row.",
  snooze: "Drafts a rule to mute future alerts matching this host and message.",
  shelve: "Parks it under Shelved until the timer you set returns it to open.",
  shelveForever: "Hides it from the queue with no timer. It never returns on its own.",
  unshelve: "Ends any shelve, timed or permanent, and sets this alert to open.",
} as const;

/** Descriptions keyed by lifecycle ActionType, for the loop over
 *  CANDIDATE_ROW_ACTIONS. Only Close is ambiguous enough to need one —
 *  Acknowledge / Re-escalate / Re-open say what they do, and a description on
 *  every item would cost the menu the scannability it is meant to gain. */
export const ROW_ACTION_DESCRIPTIONS: Partial<Record<ActionType, string>> = {
  close: SILENCE_DESCRIPTIONS.close,
};

export type SilencingGuideRow = {
  verb: string;
  what: string;
  affects: string;
  undo: string;
};

/** Long form, for the "Close, snooze or shelve?" How-to dialog. */
export const SILENCING_GUIDE: SilencingGuideRow[] = [
  {
    verb: "Close",
    what: "Marks it resolved. A closed alert doesn't notify.",
    affects: "This alert only.",
    undo: "Re-open it — or leave it: the next matching alert re-opens this same row by itself.",
  },
  {
    verb: "Snooze this alert",
    what: "Opens the snooze editor with this host and message filled in. Alerts matching a saved snooze never notify.",
    affects:
      "Every future alert that matches the rule, not just this one. Alerts already in the queue only if you retro-apply.",
    undo: "Disable or delete the rule on the Snoozes page.",
  },
  {
    verb: "Shelve",
    what: "Sets the state to Shelved for the duration you pick, then puts it back to open on its own.",
    affects: "This alert only. It does not stop notifications — that's what Snooze is for.",
    undo: "Unshelve, or wait for the duration to run out.",
  },
  {
    verb: "Shelve permanently",
    what: "Hides it from the Alerts queue with no timer, and keeps housekeeping from ever deleting it.",
    affects: "This alert only.",
    undo: "Unshelve (or the Undo in the toast).",
  },
];

/** Footnote under the table. Unshelve is not a fifth way to silence an alert —
 *  it's the exit — so it's a note rather than a row. */
export const SILENCING_GUIDE_NOTE =
  "Unshelve returns a shelved alert to open — always open, never the state it was in before it was shelved.";

/** One line under the ShelveDialog title, pointing at the verb the operator
 *  probably wants when they reach for Shelve for the wrong reason. */
export const SHELVE_VS_SNOOZE_HINT =
  "Shelving hides this one alert; it doesn't stop notifications. To silence future alerts like it, use Snooze instead.";
