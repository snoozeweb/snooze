package housekeeper

import (
	"context"

	"github.com/snoozeweb/snooze/internal/condition"
)

// CountBy stubs keep the housekeeper test fakes satisfying db.Driver; no
// housekeeper job groups records.
func (d *escalateFakeDriver) CountBy(context.Context, string, condition.Cond, string) (map[string]int, error) {
	return map[string]int{}, nil
}

func (c *cleanupStubDriver) CountBy(context.Context, string, condition.Cond, string) (map[string]int, error) {
	return map[string]int{}, nil
}
