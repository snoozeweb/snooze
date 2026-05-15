package housekeeper

import (
	"context"
	"time"

	"github.com/japannext/snooze/internal/condition"
	"github.com/japannext/snooze/internal/db"
	"github.com/japannext/snooze/internal/syncer"
)

// cleanupStubDriver is the narrowest db.Driver implementation that lets the
// CleanupSnoozeJob / CleanupNotificationJob unit tests assert "the factory
// called the right driver method" without standing up a real backend.
// snoozeFn / notificationFn capture the per-test expectation; every other
// method is a zero-value passthrough.
type cleanupStubDriver struct {
	snoozeFn       func() (int, error)
	notificationFn func() (int, error)
}

func (c *cleanupStubDriver) Search(context.Context, string, condition.Cond, db.Page) ([]db.Document, int, error) {
	return nil, 0, nil
}
func (c *cleanupStubDriver) GetOne(context.Context, string, db.Document) (db.Document, error) {
	return nil, nil
}
func (c *cleanupStubDriver) Convert(context.Context, condition.Cond, []string) (db.DriverQuery, error) {
	return nil, nil
}
func (c *cleanupStubDriver) Write(context.Context, string, []db.Document, db.WriteOptions) (db.WriteResult, error) {
	return db.WriteResult{}, nil
}
func (c *cleanupStubDriver) ReplaceOne(context.Context, string, db.Document, db.Document, bool) (int, error) {
	return 0, nil
}
func (c *cleanupStubDriver) UpdateOne(context.Context, string, string, db.Document, bool) error {
	return nil
}
func (c *cleanupStubDriver) Delete(context.Context, string, condition.Cond, bool) (int, error) {
	return 0, nil
}
func (c *cleanupStubDriver) BulkIncrement(context.Context, string, []db.IncrementOp, bool) error {
	return nil
}
func (c *cleanupStubDriver) IncMany(context.Context, string, string, condition.Cond, int64) (int, error) {
	return 0, nil
}
func (c *cleanupStubDriver) SetFields(context.Context, string, db.Document, condition.Cond) (int, error) {
	return 0, nil
}
func (c *cleanupStubDriver) AppendList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (c *cleanupStubDriver) PrependList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (c *cleanupStubDriver) RemoveList(context.Context, string, map[string][]any, condition.Cond) (int, error) {
	return 0, nil
}
func (c *cleanupStubDriver) CreateIndex(context.Context, string, []string) error { return nil }
func (c *cleanupStubDriver) ListCollections(context.Context) ([]string, error)   { return nil, nil }
func (c *cleanupStubDriver) Drop(context.Context, string) error                  { return nil }
func (c *cleanupStubDriver) Backup(context.Context, string, []string) error      { return nil }
func (c *cleanupStubDriver) CleanupTimeout(context.Context, string) (int, error) { return 0, nil }
func (c *cleanupStubDriver) CleanupComments(context.Context) (int, error)        { return 0, nil }
func (c *cleanupStubDriver) CleanupOrphans(context.Context, string) (int, error) { return 0, nil }
func (c *cleanupStubDriver) CleanupAuditLogs(context.Context, time.Duration) (int, error) {
	return 0, nil
}
func (c *cleanupStubDriver) CleanupSnooze(context.Context) (int, error) {
	if c.snoozeFn != nil {
		return c.snoozeFn()
	}
	return 0, nil
}
func (c *cleanupStubDriver) CleanupNotification(context.Context) (int, error) {
	if c.notificationFn != nil {
		return c.notificationFn()
	}
	return 0, nil
}
func (c *cleanupStubDriver) ComputeStats(context.Context, string, time.Time, time.Time, string) ([]db.StatsBucket, error) {
	return nil, nil
}
func (c *cleanupStubDriver) RenumberField(context.Context, string, string) error { return nil }
func (c *cleanupStubDriver) Watcher() syncer.Bus                                 { return nil }
func (c *cleanupStubDriver) Close() error                                        { return nil }
