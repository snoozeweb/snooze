package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
	"github.com/snoozeweb/snooze/internal/protected"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

// agenticHarness brings up a SQLite-backed Router with only the agentic
// routes mounted, seeded with one record.
func agenticHarness(t *testing.T) (chi.Router, db.Driver, string) {
	t.Helper()
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	d, err := sqlite.New(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "snooze.db")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	// The driver assigns the uid: passing one on a Write means "update that
	// row" and is rejected when it does not exist yet.
	res, err := d.Write(ctx, "record", []db.Document{{
		"host": "srv-1", "message": "disk full", "date_epoch": float64(1000),
	}}, db.WriteOptions{})
	require.NoError(t, err)
	require.Len(t, res.Added, 1)
	uid := res.Added[0]

	rt := &Router{DB: d}
	r := chi.NewRouter()
	rt.mountAgentic(r)
	return r, d, uid
}

// agenticReq issues a request carrying both the read permission and the
// literal protected-write permission — the claim set the analysis endpoint is
// designed for. Permission-boundary cases use rawReq to pick their own set.
func agenticReq(t *testing.T, r chi.Router, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf []byte
	if body != nil {
		var err error
		buf, err = json.Marshal(body)
		require.NoError(t, err)
	}
	return rawReq(t, r, method, target, buf, "ro_record", protected.WritePermission)
}

// rawReq sends a pre-encoded body with an explicit permission set.
func rawReq(t *testing.T, r chi.Router, method, target string, body []byte, perms ...string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(method, target, body, perms...))
	return rec
}

// validAgentic is a payload that passes validation.
var validAgentic = map[string]any{
	"root_cause": map[string]any{
		"summary":    "/var filled by unrotated nginx logs",
		"scope":      "srv-1:/var",
		"evidence":   []string{"df -h /var: 100%", "du: /var/log/nginx 38G"},
		"confidence": "high",
	},
	"remediation_plan": map[string]any{
		"steps":       []map[string]any{{"action": "rotate and compress", "command": "logrotate -f /etc/logrotate.d/nginx", "risk": "low"}},
		"rollback":    []map[string]any{{"action": "none needed", "risk": "low"}},
		"automatable": true,
	},
	"source": "alert-rca",
}

func TestAgenticPutStoresAndGetReturns(t *testing.T) {
	r, d, uid := agenticHarness(t)

	rec := agenticReq(t, r, http.MethodPut, "/api/v1/record/"+uid+"/agentic", validAgentic)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var put agenticResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &put))
	require.Equal(t, uid, put.UID)

	// The server stamps provenance the client never sent.
	analysis, ok := put.Agentic["analysis"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "tester", analysis["by"])
	require.Equal(t, "alert-rca", analysis["source"])
	require.NotEmpty(t, analysis["at"])

	// And it is readable back through GET.
	rec = agenticReq(t, r, http.MethodGet, "/api/v1/record/"+uid+"/agentic", nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var got agenticResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, put.Agentic, got.Agentic)

	// Storing an analysis must not make the alert look freshly seen.
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	doc, err := d.GetOne(ctx, "record", db.Document{"uid": uid})
	require.NoError(t, err)
	require.EqualValues(t, 1000, doc["date_epoch"], "date_epoch must be untouched by an analysis write")
}

func TestAgenticPutReplacesRatherThanMerges(t *testing.T) {
	r, _, uid := agenticHarness(t)
	require.Equal(t, http.StatusOK,
		agenticReq(t, r, http.MethodPut, "/api/v1/record/"+uid+"/agentic", validAgentic).Code)

	lean := map[string]any{
		"root_cause": map[string]any{"summary": "second look: it was the backup job", "confidence": "medium"},
		"remediation_plan": map[string]any{
			"steps": []map[string]any{{"action": "cap the backup retention", "risk": "medium"}},
		},
	}
	rec := agenticReq(t, r, http.MethodPut, "/api/v1/record/"+uid+"/agentic", lean)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got agenticResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	rc := got.Agentic["root_cause"].(map[string]any)
	require.NotContains(t, rc, "evidence", "stale evidence from the first analysis must not survive")
	require.NotContains(t, rc, "scope")
	plan := got.Agentic["remediation_plan"].(map[string]any)
	require.NotContains(t, plan, "rollback")
	require.NotContains(t, plan, "automatable")
}

func TestAgenticPutValidationErrorListsEveryPath(t *testing.T) {
	r, _, uid := agenticHarness(t)
	bad := map[string]any{
		"root_cause":       map[string]any{"confidence": "certain"},
		"remediation_plan": map[string]any{"steps": []map[string]any{}},
	}
	rec := agenticReq(t, r, http.MethodPut, "/api/v1/record/"+uid+"/agentic", bad)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	var env snoozetypes.ErrEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.Equal(t, "validation_error", env.Error.Code)
	require.Equal(t, "is required", env.Error.Details["root_cause.summary"])
	require.Equal(t, "must be one of high|medium|low", env.Error.Details["root_cause.confidence"])
	require.Equal(t, "must hold at least one step", env.Error.Details["remediation_plan.steps"])
}

func TestAgenticPutRejectsUnknownAndForgedFields(t *testing.T) {
	r, _, uid := agenticHarness(t)
	forged := map[string]any{
		"root_cause":       map[string]any{"summary": "x", "confidence": "high"},
		"remediation_plan": map[string]any{"steps": []map[string]any{{"action": "a", "risk": "low"}}},
		"analysis":         map[string]any{"by": "somebody-else", "at": "1999-01-01T00:00:00Z"},
	}
	rec := agenticReq(t, r, http.MethodPut, "/api/v1/record/"+uid+"/agentic", forged)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	var env snoozetypes.ErrEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	require.Equal(t, "unknown field", env.Error.Details["analysis"])
}

func TestAgenticPutMalformedJSONIsBadRequest(t *testing.T) {
	r, _, uid := agenticHarness(t)
	rec := rawReq(t, r, http.MethodPut, "/api/v1/record/"+uid+"/agentic", []byte(`{"root_cause":`),
		protected.WritePermission)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAgenticPutEmptyBodyIsBadRequest(t *testing.T) {
	r, _, uid := agenticHarness(t)
	rec := rawReq(t, r, http.MethodPut, "/api/v1/record/"+uid+"/agentic", []byte(``),
		protected.WritePermission)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAgenticPutUnknownRecordIs404(t *testing.T) {
	r, _, _ := agenticHarness(t)
	rec := agenticReq(t, r, http.MethodPut, "/api/v1/record/nope/agentic", validAgentic)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestAgenticGetWithoutAnalysisIs404(t *testing.T) {
	r, _, uid := agenticHarness(t)
	rec := agenticReq(t, r, http.MethodGet, "/api/v1/record/"+uid+"/agentic", nil)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), "no agentic analysis")
}

func TestAgenticDelete(t *testing.T) {
	r, d, uid := agenticHarness(t)
	require.Equal(t, http.StatusOK,
		agenticReq(t, r, http.MethodPut, "/api/v1/record/"+uid+"/agentic", validAgentic).Code)

	rec := agenticReq(t, r, http.MethodDelete, "/api/v1/record/"+uid+"/agentic", nil)
	require.Equal(t, http.StatusNoContent, rec.Code)

	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	doc, err := d.GetOne(ctx, "record", db.Document{"uid": uid})
	require.NoError(t, err)
	require.NotContains(t, doc, "agentic", "the key is removed, not blanked")

	// Second delete has nothing to clear.
	require.Equal(t, http.StatusNotFound,
		agenticReq(t, r, http.MethodDelete, "/api/v1/record/"+uid+"/agentic", nil).Code)
}

func TestAgenticWriteRequiresLiteralPermission(t *testing.T) {
	r, _, uid := agenticHarness(t)
	body, err := json.Marshal(validAgentic)
	require.NoError(t, err)

	// The admin wildcard is deliberately not enough.
	rec := rawReq(t, r, http.MethodPut, "/api/v1/record/"+uid+"/agentic", body, "rw_all", "rw_record")
	require.Equal(t, http.StatusForbidden, rec.Code)

	rec = rawReq(t, r, http.MethodDelete, "/api/v1/record/"+uid+"/agentic", nil, "rw_all")
	require.Equal(t, http.StatusForbidden, rec.Code)

	// The literal permission is.
	rec = rawReq(t, r, http.MethodPut, "/api/v1/record/"+uid+"/agentic", body, protected.WritePermission)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestAgenticReadAllowedForPlainReaders(t *testing.T) {
	r, _, uid := agenticHarness(t)
	body, err := json.Marshal(validAgentic)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK,
		rawReq(t, r, http.MethodPut, "/api/v1/record/"+uid+"/agentic", body, protected.WritePermission).Code)

	// A read-only auditor sees the analysis without holding rw_protected,
	// whether their grant is the collection one or the read wildcard.
	for _, perm := range []string{"ro_record", "rw_record", "ro_all", "rw_all"} {
		rec := rawReq(t, r, http.MethodGet, "/api/v1/record/"+uid+"/agentic", nil, perm)
		require.Equalf(t, http.StatusOK, rec.Code, "reader holding %s", perm)
	}

	// Someone with no record permission at all does not.
	rec := rawReq(t, r, http.MethodGet, "/api/v1/record/"+uid+"/agentic", nil, "ro_snooze")
	require.Equal(t, http.StatusForbidden, rec.Code)
}

// Tenancy is enforced at the driver line (db.TenantScope), not in this
// handler — which is exactly why it deserves a test here: a future refactor
// that reads or writes the record through a naked context would leak an
// analysis across tenants, and nothing else in this file would notice.
func TestAgenticIsTenantScoped(t *testing.T) {
	r, d, uid := agenticHarness(t)
	require.Equal(t, http.StatusOK,
		agenticReq(t, r, http.MethodPut, "/api/v1/record/"+uid+"/agentic", validAgentic).Code)

	otherTenant := func(method string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/record/"+uid+"/agentic", bytes.NewReader(body))
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		ctx := snoozetypes.WithTenant(req.Context(), "other-tenant")
		ctx = auth.WithClaims(ctx, snoozetypes.Claims{
			Subject: "intruder", Method: "local", TenantID: "other-tenant",
			Permissions: []string{"rw_record", "ro_record", protected.WritePermission},
		})
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req.WithContext(ctx))
		return rec
	}

	body, err := json.Marshal(map[string]any{
		"root_cause":       map[string]any{"summary": "cross-tenant forgery", "confidence": "high"},
		"remediation_plan": map[string]any{"steps": []map[string]any{{"action": "a", "risk": "low"}}},
	})
	require.NoError(t, err)

	// Same uid, another tenant, full permissions: the record simply is not there.
	require.Equal(t, http.StatusNotFound, otherTenant(http.MethodGet, nil).Code)
	require.Equal(t, http.StatusNotFound, otherTenant(http.MethodPut, body).Code)
	require.Equal(t, http.StatusNotFound, otherTenant(http.MethodDelete, nil).Code)

	// And the owner's analysis is untouched by the attempts.
	ctx := snoozetypes.WithTenant(context.Background(), snoozetypes.DefaultTenant)
	doc, err := d.GetOne(ctx, "record", db.Document{"uid": uid})
	require.NoError(t, err)
	stored := doc["agentic"].(map[string]any)["root_cause"].(map[string]any)
	require.Equal(t, "/var filled by unrotated nginx logs", stored["summary"])
}

// maximalAgentic builds the largest payload the schema in pkg/snoozetypes
// still accepts: every string field padded to its rune maximum, every list
// filled to its item maximum. The sizes are read off the Max* constants
// rather than hardcoded, so tightening or loosening the schema moves this
// payload with it instead of silently making the test meaningless.
func maximalAgentic() map[string]any {
	pad := func(n int) string { return strings.Repeat("x", n) }

	evidence := make([]string, snoozetypes.MaxEvidenceItems)
	for i := range evidence {
		evidence[i] = pad(snoozetypes.MaxEvidenceLen)
	}
	steps := func() []map[string]any {
		out := make([]map[string]any, snoozetypes.MaxSteps)
		for i := range out {
			out[i] = map[string]any{
				"action":  pad(snoozetypes.MaxActionLen),
				"command": pad(snoozetypes.MaxCommandLen),
				"risk":    snoozetypes.RiskLow,
			}
		}
		return out
	}

	return map[string]any{
		"root_cause": map[string]any{
			"summary":    pad(snoozetypes.MaxSummaryLen),
			"scope":      pad(snoozetypes.MaxScopeLen),
			"evidence":   evidence,
			"confidence": snoozetypes.ConfidenceHigh,
		},
		"remediation_plan": map[string]any{
			"steps":       steps(),
			"rollback":    steps(),
			"automatable": true,
		},
		"source": pad(snoozetypes.MaxSourceLen),
	}
}

// A payload that is schema-valid must never be refused for its size: the
// body cap is a runaway-agent guard, not a second, stricter schema. The
// maxima are counted in runes, so the byte budget they imply is several
// times the character count and the cap has to be derived from that.
func TestAgenticPutAcceptsMaximalSchemaValidPayload(t *testing.T) {
	r, _, uid := agenticHarness(t)

	payload := maximalAgentic()
	require.Empty(t, decodeAgenticForTest(t, payload).Validate(),
		"the payload this test sends must be schema-valid")

	rec := agenticReq(t, r, http.MethodPut, "/api/v1/record/"+uid+"/agentic", payload)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got agenticResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	rc, ok := got.Agentic["root_cause"].(map[string]any)
	require.True(t, ok)
	ev, ok := rc["evidence"].([]any)
	require.True(t, ok)
	require.Len(t, ev, snoozetypes.MaxEvidenceItems)
}

// decodeAgenticForTest round-trips a test payload through the same strict
// decoder the handler uses, so a typo in maximalAgentic surfaces here rather
// than as a confusing 422 from the endpoint.
func decodeAgenticForTest(t *testing.T, payload map[string]any) *snoozetypes.AgenticRequest {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	req, err := snoozetypes.DecodeAgenticRequest(raw)
	require.NoError(t, err)
	return &req
}

// The cap still has to bite: one byte past it is refused before the decoder
// ever sees the body, so an agent streaming megabytes is cut off early.
func TestAgenticPutRejectsBodyOverCap(t *testing.T) {
	r, _, uid := agenticHarness(t)

	// Not schema-valid, and deliberately so — the size check runs first.
	body := append([]byte(`{"pad":"`), bytes.Repeat([]byte("x"), agenticMaxBody+1)...)
	rec := rawReq(t, r, http.MethodPut, "/api/v1/record/"+uid+"/agentic", body,
		protected.WritePermission)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "body exceeds")
}
