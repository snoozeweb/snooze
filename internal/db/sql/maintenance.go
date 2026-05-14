package sql

// MaintenanceDialect bundles per-driver helpers used by the housekeeper-style
// maintenance routines (CleanupTimeout, CleanupComments, CleanupOrphans,
// CleanupAuditLogs, ComputeStats). The shared logic in this package wires
// these into common SQL skeletons; per-driver fine-tuning lives in each
// driver package.
type MaintenanceDialect interface {
	// CollectionTable maps a logical Snooze collection name to its physical
	// table identifier. dots are rewritten to "__".
	CollectionTable(collection string) string

	// EpochNow returns an expression that yields the current epoch seconds.
	// Postgres: EXTRACT(EPOCH FROM NOW()); SQLite: strftime('%s','now').
	EpochNow() string

	// DateTrunc returns an expression that truncates the given epoch column
	// to the bucket granularity ("hour", "day", "month").
	DateTrunc(epochColumn, bucket string) string
}

// TableName returns the physical table for a logical collection. dots are
// replaced with "__" to keep us inside SQL identifier syntax across dialects.
func TableName(collection string) string {
	out := make([]byte, 0, len(collection)+4)
	for i := 0; i < len(collection); i++ {
		c := collection[i]
		if c == '.' {
			out = append(out, '_', '_')
			continue
		}
		out = append(out, c)
	}
	return "snooze_" + string(out)
}
