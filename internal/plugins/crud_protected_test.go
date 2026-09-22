package plugins

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/db"
)

// protectedBody is a write payload carrying the protected `agentic` field.
var protectedBody = db.Document{
	"host":    "srv-1",
	"agentic": map[string]any{"root_cause": map[string]any{"summary": "forged"}},
}

func TestCRUDCreateRejectsProtectedField(t *testing.T) {
	t.Parallel()
	r, _ := mount(t, &crudPlugin{name: "thing"}, newMemDB())

	rec := doRequest(t, r, "POST", "/api/v1/thing", protectedBody)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "protected_field")
	require.Contains(t, rec.Body.String(), "agentic")
}

// A protected field anywhere in a batch create fails the whole batch: a
// partial write would leave the caller guessing which documents landed.
func TestCRUDCreateRejectsProtectedFieldInBatch(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	r, _ := mount(t, &crudPlugin{name: "thing"}, memo)

	rec := doRequest(t, r, "POST", "/api/v1/thing", []db.Document{
		{"uid": "u1", "host": "clean"},
		protectedBody,
	})
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Empty(t, memo.docs("thing"), "no document from a rejected batch is written")
}

func TestCRUDPatchRejectsProtectedField(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	r, _ := mount(t, &crudPlugin{name: "thing"}, memo)
	require.Equal(t, http.StatusCreated,
		doRequest(t, r, "POST", "/api/v1/thing", db.Document{"uid": "u1", "host": "srv-1"}).Code)

	rec := doRequest(t, r, "PATCH", "/api/v1/thing/u1",
		db.Document{"agentic": map[string]any{"root_cause": "forged"}})
	require.Equal(t, http.StatusForbidden, rec.Code)

	// A patch of ordinary fields is unaffected.
	rec = doRequest(t, r, "PATCH", "/api/v1/thing/u1", db.Document{"host": "srv-2"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestCRUDReplaceRejectsProtectedField(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	r, _ := mount(t, &crudPlugin{name: "thing"}, memo)
	require.Equal(t, http.StatusCreated,
		doRequest(t, r, "POST", "/api/v1/thing", db.Document{"uid": "u1", "host": "srv-1"}).Code)

	rec := doRequest(t, r, "PUT", "/api/v1/thing/u1", protectedBody)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

// A full replace that simply omits the protected field must not delete it —
// otherwise PUT is a back-door clear that skips the endpoint's permission gate.
func TestCRUDReplaceCarriesProtectedFieldForward(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	r, h := mount(t, &crudPlugin{name: "thing"}, memo)
	stored := map[string]any{"root_cause": map[string]any{"summary": "kept"}}
	require.Equal(t, http.StatusCreated, doRequest(t, r, "POST", "/api/v1/thing",
		db.Document{"uid": "u1", "host": "srv-1"}).Code)
	// Seed the protected value the way the dedicated endpoint would: straight
	// into storage, bypassing the CRUD guard.
	memo.docs("thing")[0]["agentic"] = stored

	rec := doRequest(t, r, "PUT", "/api/v1/thing/u1", db.Document{"host": "srv-2"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	doc, err := h.DB().GetOne(context.Background(), "thing", db.Document{"uid": "u1"})
	require.NoError(t, err)
	require.Equal(t, "srv-2", doc["host"])
	require.Equal(t, stored, doc["agentic"])
}

// A read failure while carrying protected fields forward must fail the
// replace, not proceed: treating "could not read" as "nothing to carry" would
// turn a transient DB blip into a silent deletion of the protected data.
func TestCRUDReplaceFailsClosedWhenTheCarryForwardReadFails(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	r, _ := mount(t, &crudPlugin{name: "thing"}, &getOneFailsDB{memDB: memo})
	require.Equal(t, http.StatusCreated,
		doRequest(t, r, "POST", "/api/v1/thing", db.Document{"uid": "u1", "host": "srv-1"}).Code)
	memo.docs("thing")[0]["agentic"] = map[string]any{"root_cause": "kept"}

	rec := doRequest(t, r, "PUT", "/api/v1/thing/u1", db.Document{"host": "srv-2"})
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), "protected fields")
	require.Equal(t, "srv-1", memo.docs("thing")[0]["host"], "the replace must not have landed")
}

// getOneFailsDB is a memDB whose GetOne reports a transport-style failure
// (not ErrNotFound), which is the case the guard has to distinguish.
type getOneFailsDB struct{ *memDB }

func (d *getOneFailsDB) GetOne(context.Context, string, db.Document) (db.Document, error) {
	return nil, errors.New("connection reset")
}

// A read-modify-write client GETs a document and PUTs it back verbatim. The
// echoed protected subtree is identical to the stored one, so nothing changes
// and there is nothing to refuse — rejecting it would make the whole generic
// CRUD surface unusable on any record that carries an analysis.
func TestCRUDReplaceAcceptsAnEchoedProtectedField(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	r, h := mount(t, &crudPlugin{name: "thing"}, memo)
	require.Equal(t, http.StatusCreated, doRequest(t, r, "POST", "/api/v1/thing",
		db.Document{"uid": "u1", "host": "srv-1"}).Code)
	stored := map[string]any{
		"root_cause": map[string]any{"summary": "kept", "confidence": 0.8},
		"version":    int64(3),
	}
	memo.docs("thing")[0]["agentic"] = stored

	// The body is what a client gets back from GET: JSON round-tripped, so the
	// numbers arrive as float64 rather than the stored int64/float64 mix.
	rec := doRequest(t, r, "PUT", "/api/v1/thing/u1", db.Document{
		"host": "srv-2",
		"agentic": map[string]any{
			"root_cause": map[string]any{"summary": "kept", "confidence": 0.8},
			"version":    float64(3),
		},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	doc, err := h.DB().GetOne(context.Background(), "thing", db.Document{"uid": "u1"})
	require.NoError(t, err)
	require.Equal(t, "srv-2", doc["host"])
	require.Equal(t, stored, doc["agentic"], "the stored value wins, unchanged")
}

// Echoing back a DIFFERENT value is an attempted write and stays refused.
func TestCRUDReplaceRejectsAChangedProtectedField(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	r, _ := mount(t, &crudPlugin{name: "thing"}, memo)
	require.Equal(t, http.StatusCreated, doRequest(t, r, "POST", "/api/v1/thing",
		db.Document{"uid": "u1", "host": "srv-1"}).Code)
	memo.docs("thing")[0]["agentic"] = map[string]any{"root_cause": "stored"}

	rec := doRequest(t, r, "PUT", "/api/v1/thing/u1", db.Document{
		"host":    "srv-2",
		"agentic": map[string]any{"root_cause": "forged"},
	})
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "protected_field")
	require.Equal(t, "srv-1", memo.docs("thing")[0]["host"], "the replace must not have landed")
}

// A PUT that would upsert a brand-new row cannot carry a protected field:
// there is no stored value for it to be equal to.
func TestCRUDReplaceRejectsProtectedFieldOnAnAbsentDocument(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	r, _ := mount(t, &crudPlugin{name: "thing"}, memo)

	rec := doRequest(t, r, "PUT", "/api/v1/thing/nope", protectedBody)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "protected_field")
}

// Same read-modify-write story for PATCH: the echoed key is dropped from the
// patch (so it is neither written nor reported as a changed field) and the
// rest of the patch applies normally.
func TestCRUDPatchAcceptsAnEchoedProtectedField(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	p := &transformingPlugin{crudPlugin: crudPlugin{name: "thing"}}
	r, h := mount(t, p, memo)
	require.Equal(t, http.StatusCreated, doRequest(t, r, "POST", "/api/v1/thing",
		db.Document{"uid": "u1", "host": "srv-1"}).Code)
	stored := map[string]any{"root_cause": map[string]any{"summary": "kept"}}
	memo.docs("thing")[0]["agentic"] = stored

	rec := doRequest(t, r, "PATCH", "/api/v1/thing/u1", db.Document{
		"host":    "srv-2",
		"agentic": map[string]any{"root_cause": map[string]any{"summary": "kept"}},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotContains(t, p.lastDoc, "agentic",
		"the echoed key is removed from the patch, so it is never written nor audited")

	doc, err := h.DB().GetOne(context.Background(), "thing", db.Document{"uid": "u1"})
	require.NoError(t, err)
	require.Equal(t, "srv-2", doc["host"])
	require.Equal(t, stored, doc["agentic"], "the stored value is untouched")
}

func TestCRUDPatchRejectsAChangedProtectedField(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	r, _ := mount(t, &crudPlugin{name: "thing"}, memo)
	require.Equal(t, http.StatusCreated, doRequest(t, r, "POST", "/api/v1/thing",
		db.Document{"uid": "u1", "host": "srv-1"}).Code)
	memo.docs("thing")[0]["agentic"] = map[string]any{"root_cause": "stored"}

	rec := doRequest(t, r, "PATCH", "/api/v1/thing/u1", db.Document{
		"host":    "srv-2",
		"agentic": map[string]any{"root_cause": "forged"},
	})
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "protected_field")
	require.Equal(t, "srv-1", memo.docs("thing")[0]["host"], "the patch must not have landed")
}

// The echo check needs a read; a failing read must fail the patch rather than
// fall back to "assume it matches" (which would write the client's value) or
// "assume it differs" (a 403 that misreports a DB outage as a permission
// problem).
func TestCRUDPatchFailsClosedWhenTheEchoCheckReadFails(t *testing.T) {
	t.Parallel()
	memo := newMemDB()
	r, _ := mount(t, &crudPlugin{name: "thing"}, &getOneFailsDB{memDB: memo})
	require.Equal(t, http.StatusCreated,
		doRequest(t, r, "POST", "/api/v1/thing", db.Document{"uid": "u1", "host": "srv-1"}).Code)
	memo.docs("thing")[0]["agentic"] = map[string]any{"root_cause": "kept"}

	rec := doRequest(t, r, "PATCH", "/api/v1/thing/u1", db.Document{
		"host":    "srv-2",
		"agentic": map[string]any{"root_cause": "kept"},
	})
	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "db_error")
	require.Contains(t, rec.Body.String(), "protected fields")
	require.Equal(t, "srv-1", memo.docs("thing")[0]["host"], "the patch must not have landed")
}
