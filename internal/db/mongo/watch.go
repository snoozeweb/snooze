package mongo

import (
	"context"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/snoozeweb/snooze/internal/syncer"
)

// mongoBus is the syncer.Bus implementation that fans MongoDB change-stream
// events out to in-process subscribers.
//
// IMPORTANT: change streams require the connected MongoDB to be a replica set
// or a sharded cluster. Against a standalone mongod the watch call fails.
//
// Streams are opened lazily on first Subscribe call per collection.
type mongoBus struct {
	d        *Driver
	mu       sync.Mutex
	subs     []*subscription
	streams  map[string]context.CancelFunc // collection -> stop fn for that watcher
	closed   bool
	rootCtx  context.Context
	rootStop context.CancelFunc
}

type subscription struct {
	prefix string
	ch     chan syncer.Event
	ctx    context.Context
}

// newMongoBus constructs an unstarted bus tied to the given driver.
func newMongoBus(d *Driver) *mongoBus {
	ctx, cancel := context.WithCancel(context.Background())
	return &mongoBus{
		d:        d,
		streams:  make(map[string]context.CancelFunc),
		rootCtx:  ctx,
		rootStop: cancel,
	}
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
func (b *mongoBus) runStream(ctx context.Context, collection string) {
	opts := options.ChangeStream().SetFullDocument(options.UpdateLookup)
	stream, err := b.d.coll(collection).Watch(ctx, mongo.Pipeline{}, opts)
	if err != nil {
		// Replica set not configured (or transient). Backoff once.
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
		return
	}
	defer stream.Close(context.Background()) //nolint:errcheck
	for stream.Next(ctx) {
		var raw bson.M
		if err := stream.Decode(&raw); err != nil {
			continue
		}
		ev := changeEventToSyncerEvent(raw, collection)
		b.dispatch(ev)
	}
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
	if doc, ok := raw["fullDocument"].(bson.M); ok {
		if uid, ok := doc["uid"].(string); ok && uid != "" {
			uids = append(uids, uid)
		}
	}
	if dk, ok := raw["documentKey"].(bson.M); ok {
		if uid, ok := dk["uid"].(string); ok && uid != "" && len(uids) == 0 {
			uids = append(uids, uid)
		}
	}
	return syncer.Event{
		Topic:      "collection." + collection,
		Op:         op,
		Collection: collection,
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
