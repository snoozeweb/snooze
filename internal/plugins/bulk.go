// bulk.go declares the opt-in hook a plugin implements to allow — and police —
// the whole-query bulk mutation endpoints (POST /api/v1/{plugin}/bulk_update,
// POST /api/v1/record/bulk_state).
//
// It exists because the per-document write hooks (WriteTransformer, WriteGuard)
// are structurally unusable on a bulk path and dangerous when forced onto one:
//
//   - TransformWrite is defined per document. A bulk `set` is one partial merge
//     fanned out over every matched row, so running the transform on it makes
//     the stamps identity hooks add (savedsearch's `owner`, comment's `user` /
//     `method`) apply to rows belonging to other people — a bulk edit would
//     silently reassign authorship of every match.
//   - GuardWrite is defined per uid and most implementations authorize against
//     the PRIOR state of that row. A bulk call never loads the rows, so the
//     only uid it could pass is "": apikey's guard rejects "" outright (turning
//     every apikey bulk_update into a 403 with a nonsense message about key
//     creation), while user's last-admin / platform_admin protection, role's
//     reserved-role protection and comment's state-transition checks all
//     degrade to no-ops. That is worse than no guard: it looks enforced.
//
// So the bulk handlers never call those two hooks. A collection that carries
// either one is simply not bulk-writable until it opts in here with semantics
// it has actually thought through.

package plugins

import (
	"context"

	"github.com/snoozeweb/snooze/internal/db"
)

// BulkWriteGuard plugins opt their collection into the bulk-mutation endpoints
// and authorize each request as a whole.
//
// Contract:
//
//   - GuardBulkWrite is called exactly ONCE per bulk request, before any driver
//     call, with the caller's request context (claims and tenant attached).
//   - set is the partial field merge to apply to every matched row (nil/empty
//     when the request only tags/untags); tag and untag are the tag lists to
//     add and remove. The condition selecting the rows is NOT passed: the guard
//     authorizes the mutation, not the selection, which the driver already
//     tenant-scopes.
//   - The implementation must NOT mutate set, tag or untag — a bulk request is
//     one mutation shared by every row, so a rewrite here rewrites all of them.
//   - Whatever per-row semantics the collection needs (loading prior state,
//     protecting reserved rows, rejecting field/state combinations) are the
//     implementation's own responsibility; the handler does no per-row work.
//   - A non-nil error aborts the request with 403 and the error's message; the
//     handler has made no driver call at that point, so nothing is written.
//
// A plugin that implements neither this interface nor WriteGuard/WriteTransformer
// is bulk-writable with no hook at all (the historical behaviour, and the case
// of the `record` collection the web UI drives). A plugin that implements
// WriteGuard or WriteTransformer but NOT this interface has its bulk requests
// refused outright — see the rationale above.
type BulkWriteGuard interface {
	Plugin
	GuardBulkWrite(ctx context.Context, set db.Document, tag, untag []string) error
}
