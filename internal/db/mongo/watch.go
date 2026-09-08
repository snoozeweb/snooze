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

	dbpkg "github.com/snoozeweb/snooze/internal/db"
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
	// dropped counts events discarded because ch was full. Guarded by
	// mongoBus.mu; read only for throttled logging in dispatch.
	dropped uint64
}

// dropLogEvery throttles the backpressure-drop warning: the first drop per
// subscriber is logged, then every dropLogEvery-th one.
const dropLogEvery = 100

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

// Subscribe registers a topic-prefix subscriber; the prefix matches on
// dot-delimited segment boundaries (see syncer.TopicMatches). The returned
// channel is closed when ctx is cancelled (subscriber-scoped) or when Close is
// called (bus-scoped).
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
		if counterOnlyUpdate(collection, raw) {
			// A per-rule counter bump (the snooze plugin's bumpHits on every
			// live match; the notification dispatcher's hits/last_sent stamp on
			// every delivery) carries no rule-affecting change. Dispatching it
			// would trigger a full plugin Reload, so on a busy server the
			// counter feedback loop becomes a self-induced reload storm. Drop
			// it: nothing downstream needs to reload.
			continue
		}
		ev := changeEventToSyncerEvent(raw, collection)
		b.dispatch(ev)
	}
}

// counterOnlyUpdate reports whether an update change event modified ONLY the
// display counters of `collection` (see db.IsCounterOnlyPatch for the
// per-collection set) and nothing else. Such writes come from the synchronous
// read-modify-write the snooze/notification plugins issue against their own
// collection on every match; a reload triggered by them is pure waste — and on
// a high-volume server the churn both burns cycles and widens the window for a
// slow reload to stall the single syncer dispatch goroutine. Inserts, deletes,
// replaces, and any update touching a semantically meaningful field
// (condition, enabled, time_constraints, …) are never skipped, including when
// it rides along with a counter.
//
// The field set lives in internal/db/counter_fields.go so the SQLite and
// Postgres publish paths suppress exactly the same writes.
func counterOnlyUpdate(collection string, raw bson.M) bool {
	if op, _ := raw["operationType"].(string); op != "update" {
		return false
	}
	ud, ok := subDocument(raw["updateDescription"])
	if !ok {
		return false
	}
	updated, ok := subDocument(ud["updatedFields"])
	if !ok || len(updated) == 0 {
		return false
	}
	return dbpkg.IsCounterOnlyPatch(collection, updated)
}

// subDocument coerces a nested value of a decoded change event into a keyed
// document.
//
// This exists because of a driver detail that is easy to get wrong and was
// wrong here for months: `stream.Decode(&raw)` into a bson.M decodes the
// TOP-LEVEL document as bson.M, but every nested sub-document lands in an `any`
// slot, and the empty-interface codec materialises those as **bson.D** (an
// ordered []E), not bson.M. A plain `raw["fullDocument"].(bson.M)` therefore
// always failed against a live change stream — while unit tests that hand-build
// bson.M fixtures passed, hiding it.
//
// Consequences of that failed assertion, all silent: every hit-counter bump
// looked like a real edit (counterOnlyUpdate could not see updateDescription), and
// every event lost its tenant and uid — which made the syncer reload with a
// tenant-less context, i.e. a no-op for every tenant-scoped plugin. Net effect:
// no plugin cache ever refreshed and only a restart applied a config change.
//
// Accepts all three shapes so it is correct regardless of how the event was
// produced (live stream, hand-built fixture, or already-normalised map).
func subDocument(v any) (bson.M, bool) {
	switch x := v.(type) {
	case bson.M:
		return x, true
	case map[string]any:
		return x, true
	case bson.D:
		m := make(bson.M, len(x))
		for _, e := range x {
			m[e.Key] = e.Value
		}
		return m, true
	default:
		return nil, false
	}
}

// dispatch forwards e to every subscriber whose prefix matches.
//
// The send stays non-blocking (a wedged subscriber must not back-pressure the
// change stream) and stays under b.mu — the same lock dropSub/Close take before
// closing a subscriber channel, so releasing it here would race a close and
// panic on send. What changed is that the drop is no longer silent: losing an
// event means a config change never reaches its plugin, which is precisely the
// class of failure that is impossible to diagnose after the fact. Logging is
// throttled (first drop, then every 100th per subscriber) so a genuine storm
// cannot flood the journal.
func (b *mongoBus) dispatch(e syncer.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.subs {
		if !syncer.TopicMatches(e.Topic, s.prefix) {
			continue
		}
		select {
		case s.ch <- e:
		default:
			s.dropped++
			if s.dropped == 1 || s.dropped%dropLogEvery == 0 {
				b.logger.Warn("mongo: change-stream subscriber is full; dropping event",
					slog.String("topic", e.Topic),
					slog.String("collection", e.Collection),
					slog.String("tenant", e.Tenant),
					slog.String("subscriber", s.prefix),
					slog.Uint64("dropped_total", s.dropped))
			}
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
	// subDocument, not a bson.M type assertion: a live change stream hands these
	// nested fields over as bson.D. See subDocument for the full story.
	if doc, ok := subDocument(raw["fullDocument"]); ok {
		if uid, ok := doc["uid"].(string); ok && uid != "" {
			uids = append(uids, uid)
		}
		// Resolve the tenant from the stored doc so the syncer can route the
		// reload to the right per-tenant plugin. Global collections carry no
		// tenant_id, leaving tenant empty (the bare topic). UpdateLookup makes
		// fullDocument available on inserts/updates/replaces; deletes have no
		// fullDocument and so carry no tenant — the syncer fans a tenant-less
		// event out to every active tenant so the delete still lands.
		if tid, ok := doc["tenant_id"].(string); ok {
			tenant = tid
		}
	}
	if dk, ok := subDocument(raw["documentKey"]); ok {
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
