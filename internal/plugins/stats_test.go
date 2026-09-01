package plugins

import (
	"context"
	"testing"
	"time"

	"github.com/snoozeweb/snooze/internal/config"
	"github.com/snoozeweb/snooze/internal/config/schema"
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/asyncwriter"
	"github.com/stretchr/testify/require"
)

// capturedInc records one BulkIncrement operation decoded into named fields.
type capturedInc struct {
	collection, field string
	search            db.Document
	delta             int64
}

// incCaptureDriver wraps memDB and overrides BulkIncrement to capture calls.
type incCaptureDriver struct {
	*memDB
	calls *[]capturedInc
}

func (d *incCaptureDriver) BulkIncrement(_ context.Context, collection string, ops []db.IncrementOp, _ bool) error {
	for _, op := range ops {
		for field, delta := range op.Deltas {
			*d.calls = append(*d.calls, capturedInc{
				collection: collection,
				field:      field,
				search:     op.Search,
				delta:      delta,
			})
		}
	}
	return nil
}

// newCapturingWriter constructs an asyncwriter.Writer backed by an
// incCaptureDriver. The returned slice pointer accumulates all increments that
// reach BulkIncrement after a Flush call.
func newCapturingWriter() (*asyncwriter.Writer, *[]capturedInc) {
	calls := &[]capturedInc{}
	drv := &incCaptureDriver{memDB: newMemDB(), calls: calls}
	w := asyncwriter.New(drv, time.Hour, asyncwriter.NewMockClock(time.Unix(0, 0)),
		asyncwriter.WithUpsert(true))
	return w, calls
}

// statTestHost implements plugins.Host and AsyncWriterHost.
type statTestHost struct {
	nullHost       // embed for the full Host surface
	writer         *asyncwriter.Writer
	metricsEnabled bool
}

func (h *statTestHost) Config() *config.Config {
	cfg := config.Default()
	cfg.General = schema.General{MetricsEnabled: h.metricsEnabled}
	return cfg
}

func (h *statTestHost) AsyncWriter() *asyncwriter.Writer {
	return h.writer
}

// noAsyncWriterHost implements plugins.Host but does NOT implement AsyncWriterHost.
// Used to test the type-assertion guard in RecordStat.
type noAsyncWriterHost struct {
	nullHost
}

func TestRecordStat_WritesOneDocPerLabel_HourBucketed(t *testing.T) {
	w, calls := newCapturingWriter()
	h := &statTestHost{writer: w, metricsEnabled: true}
	// eventEpoch 1780302245 -> hour bucket 1780300800
	RecordStat(context.Background(), h, 1780302245, "alert_hit", map[string]string{
		"source":      "syslog",
		"severity":    "critical",
		"environment": "", // empty -> skipped
	}, 1)
	require.NoError(t, w.Flush(context.Background()))
	got := *calls
	require.Len(t, got, 2)
	// Build a dim->key map from captured calls; this is order-independent and
	// will fail if dim or key are swapped or omitted.
	dimKey := make(map[string]string, len(got))
	for _, c := range got {
		require.Equal(t, "stats", c.collection)
		require.Equal(t, "value", c.field)
		require.Equal(t, int64(1), c.delta)
		require.Equal(t, "alert_hit", c.search["metric"])
		require.Equal(t, int64(1780300800), c.search["bucket"])
		dim, _ := c.search["dim"].(string)
		key, _ := c.search["key"].(string)
		dimKey[dim] = key
	}
	require.Equal(t, map[string]string{"source": "syslog", "severity": "critical"}, dimKey)
}

// A record that has not been persisted yet carries DateEpoch == 0 (the storage
// driver stamps date_epoch at write time), and the alert pipeline records its
// counters before that happens. Bucketing those at epoch 0 filed every counter
// under 1970-01-01 — outside every dashboard window — so the dashboard's whole
// counter half read as empty on a live instance. The counter must land in the
// current hour instead.
func TestRecordStat_MissingEventEpochBucketsToNow(t *testing.T) {
	w, calls := newCapturingWriter()
	h := &statTestHost{writer: w, metricsEnabled: true}
	before := time.Now().UTC().Truncate(time.Hour).Unix()
	RecordStat(context.Background(), h, 0, "alert_hit", map[string]string{"source": "syslog"}, 1)
	require.NoError(t, w.Flush(context.Background()))
	after := time.Now().UTC().Truncate(time.Hour).Unix()

	got := *calls
	require.Len(t, got, 1)
	bucket, ok := got[0].search["bucket"].(int64)
	require.True(t, ok, "bucket must be an int64, got %T", got[0].search["bucket"])
	require.NotZero(t, bucket, "epoch-0 bucket is invisible to every dashboard window")
	// Tolerates an hour rollover between the two reads.
	require.GreaterOrEqual(t, bucket, before)
	require.LessOrEqual(t, bucket, after)
}

func TestRecordStat_NoopWhenMetricsDisabled(t *testing.T) {
	w, calls := newCapturingWriter()
	h := &statTestHost{writer: w, metricsEnabled: false}
	RecordStat(context.Background(), h, 1780302245, "alert_snoozed", map[string]string{"name": "f"}, 1)
	require.NoError(t, w.Flush(context.Background()))
	require.Empty(t, *calls)
}

func TestRecordStat_NoopWhenNoAsyncWriter(_ *testing.T) {
	// noAsyncWriterHost does not implement AsyncWriterHost, so RecordStat must
	// return early at the type-assertion guard without panicking.
	h := &noAsyncWriterHost{}
	h.nullHost = *newNullHost(newMemDB())
	h.cfg.General.MetricsEnabled = true
	RecordStat(context.Background(), h, 1, "alert_hit", map[string]string{"source": "x"}, 1)
}

func TestRecordStat_NoopWhenWriterNil(_ *testing.T) {
	// statTestHost implements AsyncWriterHost but returns a nil Writer.
	h := &statTestHost{writer: nil, metricsEnabled: true}
	RecordStat(context.Background(), h, 1, "alert_hit", map[string]string{"source": "x"}, 1)
}
