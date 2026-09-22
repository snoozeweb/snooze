package rule

import (
	"context"
	"fmt"
	"strings"

	"github.com/snoozeweb/snooze/internal/modification"
	"github.com/snoozeweb/snooze/internal/protected"
)

// GuardWrite implements plugins.WriteGuard: it refuses to store a rule whose
// modifications would write a protected field (internal/protected).
//
// The runtime engine already refuses such a modification (modification.Apply
// returns ErrProtectedTarget), but refusing at save time is what an operator
// can act on: a rule that stores fine and then quietly does nothing is a
// support ticket, while a 403 naming the field is a fixed rule a minute later.
//
// Only STATIC targets are caught here — a field name computed from a template
// at runtime cannot be resolved against a record that does not exist yet.
// That case is exactly what the runtime guard is for; the two layers are
// complementary, not redundant.
func (p *Plugin) GuardWrite(_ context.Context, _ string, doc map[string]any, _ bool) error {
	return guardModifications(doc["modifications"])
}

// GuardBulkWrite implements plugins.BulkWriteGuard, applying the same check to
// the attributes a bulk_update would set.
//
// It is not reachable today: bulk_update refuses any collection whose plugin
// is not a plugins.DataModel, and the rule plugin is a Processor. It exists so
// the guard travels with the plugin rather than having to be remembered — the
// day `rule` gains Schema/Validate, its bulk path is already covered instead of
// silently becoming the one way around GuardWrite.
func (p *Plugin) GuardBulkWrite(_ context.Context, set map[string]any, _, _ []string) error {
	return guardModifications(set["modifications"])
}

// guardModifications parses a rule's `modifications` value and reports every
// protected target it finds. A value that does not parse is left to the loader
// to reject — this guard has one job.
//
// KV_SET is checked alongside the standard ops even though it is not a
// modification.Modification: parseModifications routes it to its own slice
// (the op needs host access, so it lives in the rule plugin rather than the
// modification package), and its `out_field` is a write target like any other.
// Leaving it out would make `["KV_SET", dict, key, "agentic"]` the one rule
// shape that could still forge a protected field.
func guardModifications(raw any) error {
	if raw == nil {
		return nil
	}
	mods, kvs, err := parseModifications(raw)
	if err != nil {
		return nil
	}
	var blocked []string
	for _, m := range mods {
		for _, target := range m.Targets(nil) {
			if protected.IsProtected(target) {
				blocked = append(blocked, string(m.Op)+" "+target)
			}
		}
	}
	for _, kv := range kvs {
		if protected.IsProtected(kv.OutField) {
			blocked = append(blocked, "KV_SET "+kv.OutField)
		}
	}
	if len(blocked) == 0 {
		return nil
	}
	return fmt.Errorf("rule modifications may not target protected field(s): %s (%w)",
		strings.Join(blocked, ", "), modification.ErrProtectedTarget)
}
