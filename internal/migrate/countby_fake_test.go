package migrate

import (
	"context"

	"github.com/snoozeweb/snooze/internal/condition"
)

// CountBy stub keeps fakeDriver satisfying db.Driver; no migration groups
// records.
func (f *fakeDriver) CountBy(context.Context, string, condition.Cond, string) (map[string]int, error) {
	return map[string]int{}, nil
}
