package plugins

import (
	"context"
	"time"

	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/asyncwriter"
)

// StatsCollection is the counter collection the dashboard aggregates.
const StatsCollection = "stats"

// AsyncWriterHost is the optional capability a Host satisfies to expose the
// batched-increment coalescer. Mirrors the private interface aggregaterule
// uses; declared here so RecordStat can stay backend-agnostic.
type AsyncWriterHost interface {
	AsyncWriter() *asyncwriter.Writer
}

// RecordStat enqueues one upsert-increment per non-empty label into the
// `stats` collection, bucketed to the UTC hour containing eventEpoch. No-op
// when metrics are disabled or the host exposes no async writer, so call sites
// never need to guard. Doc shape: {metric, dim, key, bucket, value}; the upsert
// search is {metric, dim, key, bucket} and `value` is incremented by n. The
// tenant is extracted from ctx by the asyncwriter so stats counter rows are
// tenant-partitioned automatically.
func RecordStat(ctx context.Context, host Host, eventEpoch int64, metric string, labels map[string]string, n int64) {
	if host == nil || metric == "" || n == 0 {
		return
	}
	cfg := host.Config()
	if cfg == nil || !cfg.General.MetricsEnabled {
		return
	}
	wh, ok := host.(AsyncWriterHost)
	if !ok {
		return
	}
	w := wh.AsyncWriter()
	if w == nil {
		return
	}
	bucket := time.Unix(eventEpoch, 0).UTC().Truncate(time.Hour).Unix()
	for dim, key := range labels {
		if key == "" {
			continue
		}
		w.Increment(ctx, StatsCollection, "value", db.Document{
			"metric": metric,
			"dim":    dim,
			"key":    key,
			"bucket": bucket,
		}, n)
	}
}
