package syncer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/japannext/snooze/internal/version"
)

// nodesCollection names the storage collection that holds heartbeat rows.
// Each row corresponds to one running Snooze instance.
const nodesCollection = "nodes"

// defaultHeartbeatInterval is the cadence at which NodeHeartbeat refreshes
// last_seen if Interval is zero.
const defaultHeartbeatInterval = 5 * time.Second

// HeartbeatPersister is the narrow contract NodeHeartbeat depends on. It is
// implemented by a thin adapter around db.Driver wired in by the caller —
// keeping the dependency one-directional avoids the import cycle that would
// otherwise arise from internal/db already importing internal/syncer for the
// Bus contract. The caller (typically core.NewServer) passes a closure such as
//
//	func(ctx, coll, doc) error {
//	    _, err := drv.Write(ctx, coll, []db.Document{doc},
//	        db.WriteOptions{Primary: []string{"node"}, UpdateTime: true})
//	    return err
//	}
type HeartbeatPersister func(ctx context.Context, collection string, doc map[string]any) error

// NodeHeartbeat writes a heartbeat row every Interval into the `nodes`
// collection so peers can discover this instance via the syncer API.
type NodeHeartbeat struct {
	Persist  HeartbeatPersister
	Node     string
	Version  string
	Interval time.Duration
	Logger   *slog.Logger
}

// Run advertises this instance on `nodes` every Interval until ctx is cancelled.
// The first heartbeat is written synchronously so callers can rely on the row
// existing immediately after Run begins; subsequent updates happen on the timer.
func (n *NodeHeartbeat) Run(ctx context.Context) error {
	if n.Persist == nil {
		return fmt.Errorf("syncer/nodes: nil Persist")
	}
	node := n.Node
	if node == "" {
		host, err := os.Hostname()
		if err != nil || host == "" {
			host = "unknown"
		}
		node = host
	}
	ver := n.Version
	if ver == "" {
		ver = version.String()
	}
	interval := n.Interval
	if interval <= 0 {
		interval = defaultHeartbeatInterval
	}
	logger := n.Logger
	if logger == nil {
		logger = slog.Default()
	}

	startedAt := time.Now().UTC()
	if err := n.write(ctx, node, ver, startedAt); err != nil {
		// First write is best-effort but reported — the caller decides
		// whether to abort or keep ticking.
		logger.Warn("syncer/nodes: initial heartbeat failed", "node", node, "err", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := n.write(ctx, node, ver, startedAt); err != nil {
				logger.Warn("syncer/nodes: heartbeat failed", "node", node, "err", err)
			}
		}
	}
}

// write upserts the heartbeat row keyed on `node`.
func (n *NodeHeartbeat) write(ctx context.Context, node, ver string, startedAt time.Time) error {
	doc := map[string]any{
		"node":       node,
		"version":    ver,
		"started_at": startedAt.Format(time.RFC3339Nano),
		"last_seen":  time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := n.Persist(ctx, nodesCollection, doc); err != nil {
		return fmt.Errorf("syncer/nodes: write: %w", err)
	}
	return nil
}
