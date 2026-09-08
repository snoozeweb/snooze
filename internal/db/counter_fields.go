package db

// counterFieldsByCollection lists, per collection, the write-only display
// counters a plugin stamps onto its OWN documents as a side effect of matching
// an alert. None of them is read back into a cached rule (docToRule /
// decodeEntry ignore them), so an update touching nothing else cannot change
// any plugin's behaviour — and therefore must not trigger a plugin reload.
//
//   - snooze.hits:            bumped by the snooze plugin on every live match.
//   - notification.hits:      bumped by the dispatcher on every successful delivery.
//   - notification.last_sent: stamped by the dispatcher on every successful delivery.
//
// The set is deliberately PER COLLECTION: `hits` on a collection that is not
// listed here (or a future field named `last_sent` elsewhere) is a real edit
// and must still publish. Add a collection only when you have verified the
// field is never read back into cached plugin state.
var counterFieldsByCollection = map[string]map[string]struct{}{
	"notification": {"hits": {}, "last_sent": {}},
	"snooze":       {"hits": {}},
}

// IsCounterOnlyPatch reports whether a patch/update touches ONLY the display
// counters of the given collection (a non-empty subset of that collection's
// counter set) and nothing else.
//
// Callers use it to suppress the change notification a counter bump would
// otherwise emit: without it, the read-modify-write the snooze/notification
// plugins issue against their own collection on every match feeds back into a
// full plugin reload, and on a busy server that becomes a self-induced reload
// storm (on Postgres, one `pg_notify` fan-out to every node per delivery).
//
// It returns false for an empty patch, for an unlisted collection, and for any
// patch carrying a semantically meaningful field — including when that field
// rides along with a counter.
func IsCounterOnlyPatch(collection string, fields map[string]any) bool {
	set, ok := counterFieldsByCollection[collection]
	if !ok || len(fields) == 0 {
		return false
	}
	for k := range fields {
		if _, isCounter := set[k]; !isCounter {
			return false
		}
	}
	return true
}
