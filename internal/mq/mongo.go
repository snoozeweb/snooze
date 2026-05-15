package mq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoConfig configures the MongoDB-backed Bus.
type MongoConfig struct {
	URI      string
	Database string
	// Collection names the queue table. Defaults to "mq_messages".
	Collection string
	// VisibilityTimeout is the duration after which a locked-but-unacked
	// message becomes claimable again. Defaults to 5 minutes.
	VisibilityTimeout time.Duration
	// ConsumerID identifies this consumer in the locked_by field. Defaults
	// to a random uuid.
	ConsumerID             string
	ServerSelectionTimeout time.Duration
	// Client is an optional pre-built client. When set, URI is ignored and
	// the Bus does not own the connection.
	Client *mongo.Client
}

// mongoBus implements Bus over a MongoDB collection. It uses change streams
// (when available — replica set required) to wake consumers and an atomic
// findOneAndUpdate to claim messages.
type mongoBus struct {
	cfg      MongoConfig
	client   *mongo.Client
	coll     *mongo.Collection
	ownsConn bool

	closeOnce sync.Once
	closeCh   chan struct{}
	wg        sync.WaitGroup
}

// NewMongo constructs the MongoDB-backed Bus.
func NewMongo(ctx context.Context, cfg MongoConfig) (Bus, error) {
	if cfg.Database == "" {
		cfg.Database = "snooze"
	}
	if cfg.Collection == "" {
		cfg.Collection = "mq_messages"
	}
	if cfg.VisibilityTimeout <= 0 {
		cfg.VisibilityTimeout = 5 * time.Minute
	}
	if cfg.ConsumerID == "" {
		cfg.ConsumerID = uuid.NewString()
	}
	if cfg.ServerSelectionTimeout <= 0 {
		cfg.ServerSelectionTimeout = 10 * time.Second
	}
	client := cfg.Client
	owns := false
	if client == nil {
		if cfg.URI == "" {
			return nil, errors.New("mq mongo: either URI or Client must be set")
		}
		opts := options.Client().ApplyURI(cfg.URI).SetServerSelectionTimeout(cfg.ServerSelectionTimeout)
		c, err := mongo.Connect(opts)
		if err != nil {
			return nil, fmt.Errorf("mq mongo: connect: %w", err)
		}
		client = c
		owns = true
	}
	b := &mongoBus{
		cfg:      cfg,
		client:   client,
		coll:     client.Database(cfg.Database).Collection(cfg.Collection),
		ownsConn: owns,
		closeCh:  make(chan struct{}),
	}
	if err := b.ensureIndexes(ctx); err != nil {
		if owns {
			_ = client.Disconnect(context.Background())
		}
		return nil, err
	}
	return b, nil
}

// ensureIndexes creates the indexes used by the claim query. Idempotent.
func (b *mongoBus) ensureIndexes(ctx context.Context) error {
	models := []mongo.IndexModel{
		{Keys: bson.D{{Key: "queue", Value: 1}, {Key: "ack_at", Value: 1}}},
		{Keys: bson.D{{Key: "created_at", Value: 1}}},
	}
	if _, err := b.coll.Indexes().CreateMany(ctx, models); err != nil {
		return fmt.Errorf("mq mongo: indexes: %w", err)
	}
	return nil
}

// mqDocument is the BSON shape of a queue row.
type mqDocument struct {
	ID        string            `bson:"_id"`
	Queue     string            `bson:"queue"`
	Payload   bson.Raw          `bson:"payload"`
	Headers   map[string]string `bson:"headers,omitempty"`
	LockedAt  *time.Time        `bson:"locked_at,omitempty"`
	LockedBy  string            `bson:"locked_by,omitempty"`
	AckAt     *time.Time        `bson:"ack_at,omitempty"`
	CreatedAt time.Time         `bson:"created_at"`
}

// Publish inserts a new queue document.
func (b *mongoBus) Publish(ctx context.Context, queue string, payload any) error {
	enc, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("mq mongo: marshal: %w", err)
	}
	var raw bson.Raw
	if err := bson.UnmarshalExtJSON(enc, true, &raw); err != nil {
		// Wrap the json bytes as a binary value when it isn't a JSON
		// object (e.g. a string payload).
		raw = bson.Raw(enc)
	}
	doc := bson.M{
		"_id":        uuid.NewString(),
		"queue":      queue,
		"payload":    raw,
		"created_at": time.Now().UTC(),
	}
	if _, err := b.coll.InsertOne(ctx, doc); err != nil {
		return fmt.Errorf("mq mongo: insert: %w", err)
	}
	return nil
}

// Subscribe launches Concurrency workers that watch the collection and
// dispatch messages.
func (b *mongoBus) Subscribe(ctx context.Context, queue string, opts SubscribeOpts, h Handler) error {
	if h == nil {
		return errors.New("mq mongo: nil handler")
	}
	opts = defaults(opts)
	for i := 0; i < opts.Concurrency; i++ {
		b.wg.Add(1)
		go b.consumerLoop(ctx, queue, opts, h)
	}
	return nil
}

// consumerLoop opens a change-stream (best-effort) and polls on each event
// or timer tick.
func (b *mongoBus) consumerLoop(parentCtx context.Context, queue string, opts SubscribeOpts, h Handler) {
	defer b.wg.Done()
	for {
		select {
		case <-parentCtx.Done():
			return
		case <-b.closeCh:
			return
		default:
		}
		// Initial drain.
		if err := b.drain(parentCtx, queue, opts, h); err != nil {
			select {
			case <-parentCtx.Done():
				return
			case <-b.closeCh:
				return
			case <-time.After(time.Second):
			}
			continue
		}
		// Watch (if available) and poll on a timer.
		ctx, cancel := context.WithCancel(parentCtx)
		go func() {
			select {
			case <-b.closeCh:
				cancel()
			case <-ctx.Done():
			}
		}()
		b.watchAndPoll(ctx, queue, opts, h)
		cancel()
		// Loop and reconnect.
	}
}

// watchAndPoll runs a change stream alongside a BatchTimer; either fires
// the claim path.
func (b *mongoBus) watchAndPoll(ctx context.Context, queue string, opts SubscribeOpts, h Handler) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"operationType":      bson.M{"$in": []string{"insert", "update"}},
			"fullDocument.queue": queue,
		}}},
	}
	streamCh := make(chan struct{}, 1)
	go func() { //nolint:gosec
		stream, err := b.coll.Watch(ctx, pipeline, options.ChangeStream().SetFullDocument(options.UpdateLookup))
		if err != nil {
			// Change streams unavailable; rely on the timer.
			return
		}
		defer stream.Close(context.Background()) //nolint:errcheck
		for stream.Next(ctx) {
			select {
			case streamCh <- struct{}{}:
			default:
			}
		}
	}()
	ticker := time.NewTicker(opts.BatchTimer)
	defer ticker.Stop()
	for {
		if err := b.drain(ctx, queue, opts, h); err != nil {
			// Surface up so the outer loop reconnects.
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-streamCh:
		case <-ticker.C:
		}
	}
}

// drain claims as many messages as visible (capped by BatchSize per call)
// and dispatches them, looping until the queue is empty.
func (b *mongoBus) drain(ctx context.Context, queue string, opts SubscribeOpts, h Handler) error {
	for i := 0; i < opts.BatchSize; i++ {
		doc, err := b.claimOne(ctx, queue)
		if err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				return nil
			}
			return err
		}
		if doc == nil {
			return nil
		}
		msg := Message{
			ID:        doc.ID,
			Queue:     queue,
			Payload:   []byte(doc.Payload),
			Headers:   doc.Headers,
			Timestamp: doc.CreatedAt,
		}
		if err := h(ctx, msg); err != nil {
			// Leave the row for retry; visibility timeout reclaims it.
			continue
		}
		now := time.Now().UTC()
		if _, err := b.coll.UpdateByID(ctx, doc.ID, bson.M{"$set": bson.M{"ack_at": now}}); err != nil {
			continue
		}
	}
	return nil
}

// claimOne atomically locks one visible message for this consumer.
func (b *mongoBus) claimOne(ctx context.Context, queue string) (*mqDocument, error) {
	now := time.Now().UTC()
	cutoff := now.Add(-b.cfg.VisibilityTimeout)
	filter := bson.M{
		"queue":  queue,
		"ack_at": nil,
		"$or": []bson.M{
			{"locked_at": nil},
			{"locked_at": bson.M{"$lt": cutoff}},
		},
	}
	update := bson.M{"$set": bson.M{"locked_at": now, "locked_by": b.cfg.ConsumerID}}
	opts := options.FindOneAndUpdate().
		SetSort(bson.D{{Key: "created_at", Value: 1}}).
		SetReturnDocument(options.After)
	res := b.coll.FindOneAndUpdate(ctx, filter, update, opts)
	if err := res.Err(); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, fmt.Errorf("mq mongo: claim: %w", err)
	}
	var d mqDocument
	if err := res.Decode(&d); err != nil {
		return nil, fmt.Errorf("mq mongo: decode: %w", err)
	}
	return &d, nil
}

// Close stops every consumer and (if owned) disconnects the client.
func (b *mongoBus) Close() error {
	b.closeOnce.Do(func() {
		close(b.closeCh)
	})
	b.wg.Wait()
	if b.ownsConn && b.client != nil {
		_ = b.client.Disconnect(context.Background())
		b.client = nil
	}
	return nil
}
