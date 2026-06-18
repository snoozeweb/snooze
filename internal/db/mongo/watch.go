package mongo

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/snoozeweb/snooze/internal/syncer"
)

// changeStream is the slice of *mongo.ChangeStream that runStream needs.
// Defined as an interface (rather than using the concrete type directly) so
// tests can substitute a stub via mongoBus.open — see watch_test.go.
type changeStream interface {
	Next(ctx context.Context) bool
	Decode(v any) error
	Close(ctx context.Context) error
}

// mongoBus is the syncer.Bus implementation that fans MongoDB change-stream
// events out to in-process subscribers.
//
// IMPORTANT: change streams require the connected MongoDB to be a replica set
// or a sharded cluster. Against a standalone mongod the watch call fails. The
// bus then retries with exponential backoff so the system self-heals if the
// operator promotes mongod to a replica set without restarting snooze-server.
//
// Streams are opened lazily on first Subscribe call per collection.
type mongoBus struct {
	d        *Driver
	logger   *slog.Logger
	mu       sync.Mutex
	subs     []*subscription
	streams  map[string]context.CancelFunc // collection -> stop fn for that watcher
	closed   bool
	rootCtx  context.Context
	rootStop context.CancelFunc

	// open is the function runStream calls to open a change stream. Default
	// wraps the live mongo driver; tests replace it with a stub.
	open func(ctx context.Context, collection string) (changeStream, error)

	// retryInitial / retryMax bound the exponential backoff applied between
	// failed Watch attempts. Fields rather than constants so tests can
	// shrink them to keep the test fast.
	retryInitial time.Duration
	retryMax     time.Duration
}

type subscription struct {
	prefix string
	ch     chan syncer.Event
	ctx    context.Context
}

// newMongoBus constructs an unstarted bus tied to the given driver.
func newMongoBus(d *Driver, logger *slog.Logger) *mongoBus {
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	b := &mongoBus{
		d:            d,
		logger:       logger,
		streams:      make(map[string]context.CancelFunc),
		rootCtx:      ctx,
		rootStop:     cancel,
		retryInitial: 2 * time.Second,
		retryMax:     30 * time.Second,
	}
	b.open = b.openLiveStream
	return b
}

// openLiveStream is the production implementation of mongoBus.open: it asks
// the underlying mongo.Collection for a change stream configured to deliver
// the full document on updates (so dispatch can extract uids without an
// extra round-trip).
func (b *mongoBus) openLiveStream(ctx context.Context, collection string) (changeStream, error) {
	opts := options.ChangeStream().SetFullDocument(options.UpdateLookup)
	return b.d.coll(collection).Watch(ctx, mongo.Pipeline{}, opts)
}

// Publish does nothing here: Mongo change streams are populated by the
// database itself. The method satisfies syncer.Bus.Publish so callers can use
// the unified API.
func (b *mongoBus) Publish(_ context.Context, _ syncer.Event) error { return nil }

// Subscribe registers a topic-prefix subscriber. The returned channel is
// closed when ctx is cancelled (subscriber-scoped) or when Close is called
// (bus-scoped).
func (b *mongoBus) Subscribe(ctx context.Context, topicPrefix string) (<-chan syncer.Event, error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, errBusClosed
	}
	sub := &subscription{
		prefix: topicPrefix,
		ch:     make(chan syncer.Event, 32),
		ctx:    ctx,
	}
	b.subs = append(b.subs, sub)
	// Open a change stream for any "collection.<name>" prefix if not already.
	if coll, ok := topicCollection(topicPrefix); ok {
		b.ensureStreamLocked(coll)
	}
	b.mu.Unlock()
	go func() {
		<-ctx.Done()
		b.dropSub(sub)
	}()
	return sub.ch, nil
}

// Close cancels every active change stream and closes every subscriber channel.
// Idempotent.
func (b *mongoBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	b.rootStop()
	for _, cancel := range b.streams {
		cancel()
	}
	b.streams = nil
	for _, s := range b.subs {
		safeClose(s.ch)
	}
	b.subs = nil
	return nil
}

// ensureStreamLocked starts a single watcher goroutine per collection.
// Caller must hold b.mu.
func (b *mongoBus) ensureStreamLocked(collection string) {
	if _, ok := b.streams[collection]; ok {
		return
	}
	ctx, cancel := context.WithCancel(b.rootCtx) //nolint:gosec
	b.streams[collection] = cancel
	go b.runStream(ctx, collection)
}

// runStream reads change events from one collection and dispatches them.
// Watch failures (the common one: mongod is standalone, no change streams) are
// logged at ERROR and retried with exponential backoff so the bus self-heals
// when the operator later promotes mongod to a replica set without restarting
// snooze-server. Returns only on ctx cancellation.
func (b *mongoBus) runStream(ctx context.Context, collection string) {
	backoff := b.retryInitial
	for {
		if ctx.Err() != nil {
			return
		}
		stream, err := b.open(ctx, collection)
		if err != nil {
			b.logger.Error("mongo: change-stream watch failed; retrying",
				slog.String("collection", collection),
				slog.Duration("retry_in", backoff),
				slog.Any("err", err))
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < b.retryMax {
				backoff *= 2
				if backoff > b.retryMax {
					backoff = b.retryMax
				}
			}
			continue
		}
		// Successful watch — reset backoff so the next failure starts fresh.
		backoff = b.retryInitial
		b.consumeStream(ctx, collection, stream)
		// consumeStream returned: either ctx is done or the stream broke.
		// Loop will re-check ctx and either return or re-Watch.
	}
}

// consumeStream drains one already-open change stream and dispatches events
// until the stream errors out or ctx is cancelled. Always closes the stream
// before returning.
func (b *mongoBus) consumeStream(ctx context.Context, collection string, stream changeStream) {
	defer stream.Close(context.Background()) //nolint:errcheck
	for stream.Next(ctx) {
		var raw bson.M
		if err := stream.Decode(&raw); err != nil {
			b.logger.Warn("mongo: change-stream decode failed; skipping event",
				slog.String("collection", collection),
				slog.Any("err", err))
			continue
		}
		if hitsOnlyUpdate(raw) {
			// A per-rule `hits` counter bump (e.g. the snooze plugin's
			// bumpHits on every live match) carries no rule-affecting change.
			// Dispatching it would trigger a full plugin Reload, so on a busy
			// server the hit-counter feedback loop becomes a self-induced
			// reload storm. Drop it: nothing downstream needs to reload.
			continue
		}
		ev := changeEventToSyncerEvent(raw, collection)
		b.dispatch(ev)
	}
}

// hitsOnlyUpdate reports whether an update change event modified ONLY the
// per-rule `hits` audit counter and nothing else. Such writes come from the
// synchronous UpdateOne the snooze/notification plugins issue against their own
// collection on every match; the `hits` field is never read into a cached rule
// (docToRule ignores it), so a reload triggered by it is pure waste — and on a
// high-volume server the churn both burns cycles and widens the window for a
// slow reload to stall the single syncer dispatch goroutine. Inserts, deletes,
// replaces, and any update touching a semantically meaningful field
// (condition, enabled, time_constraints, …) are never skipped.
func hitsOnlyUpdate(raw bson.M) bool {
	if op, _ := raw["operationType"].(string); op != "update" {
		return false
	}
	ud, ok := raw["updateDescription"].(bson.M)
	if !ok {
		return false
	}
	updated, ok := ud["updatedFields"].(bson.M)
	if !ok || len(updated) == 0 {
		return false
	}
	for k := range updated {
		if k != "hits" {
			return false
		}
	}
	return true
}

// dispatch forwards e to every subscriber whose prefix matches.
func (b *mongoBus) dispatch(e syncer.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.subs {
		if !strings.HasPrefix(e.Topic, s.prefix) {
			continue
		}
		select {
		case s.ch <- e:
		default:
			// drop on backpressure
		}
	}
}

// dropSub removes one subscription and closes its channel.
func (b *mongoBus) dropSub(s *subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, x := range b.subs {
		if x == s {
			b.subs = append(b.subs[:i], b.subs[i+1:]...)
			safeClose(s.ch)
			return
		}
	}
}

// changeEventToSyncerEvent maps a Mongo change-stream document to syncer.Event.
func changeEventToSyncerEvent(raw bson.M, collection string) syncer.Event {
	op, _ := raw["operationType"].(string)
	switch op {
	case "insert":
		op = "write"
	case "update":
		op = "write"
	case "replace":
		op = "replace"
	case "delete":
		op = "delete"
	}
	uids := []string{}
	var tenant string
	if doc, ok := raw["fullDocument"].(bson.M); ok {
		if uid, ok := doc["uid"].(string); ok && uid != "" {
			uids = append(uids, uid)
		}
		// Resolve the tenant from the stored doc so the syncer can route the
		// reload to the right per-tenant plugin. Global collections carry no
		// tenant_id, leaving tenant empty (the bare topic). UpdateLookup makes
		// fullDocument available on inserts/updates/replaces; deletes have no
		// fullDocument and so carry no tenant (a delete still triggers a reload
		// via the bare-prefix subscription, just without a tenant context).
		if tid, ok := doc["tenant_id"].(string); ok {
			tenant = tid
		}
	}
	if dk, ok := raw["documentKey"].(bson.M); ok {
		if uid, ok := dk["uid"].(string); ok && uid != "" && len(uids) == 0 {
			uids = append(uids, uid)
		}
	}
	return syncer.Event{
		Topic:      syncer.CollectionTopic(collection, tenant),
		Op:         op,
		Collection: collection,
		Tenant:     tenant,
		UIDs:       uids,
		At:         time.Now().UTC(),
	}
}

// topicCollection extracts the collection name from a "collection.<name>..."
// topic prefix.
func topicCollection(prefix string) (string, bool) {
	const tag = "collection."
	if !strings.HasPrefix(prefix, tag) {
		return "", false
	}
	rest := strings.TrimPrefix(prefix, tag)
	rest = strings.Split(rest, ".")[0]
	if rest == "" {
		return "", false
	}
	return rest, true
}

// safeClose closes ch if not already closed.
func safeClose(ch chan syncer.Event) {
	defer func() { _ = recover() }()
	close(ch)
}

// errBusClosed is returned by Subscribe after Close. Defined as a sentinel
// here (rather than reusing db.ErrClosed) so syncer-package callers don't need
// to import internal/db.
type busClosedErr struct{}

func (busClosedErr) Error() string { return "mongo: bus closed" }

var errBusClosed = busClosedErr{}
