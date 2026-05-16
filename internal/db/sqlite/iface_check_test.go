// Compile-time assertion that *Driver fully implements db.Driver. If a
// method goes missing or its signature drifts, the test binary will fail
// to compile rather than the bug surfacing on first call.

package sqlite_test

import (
	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/db/sqlite"
)

var _ db.Driver = (*sqlite.Driver)(nil)
