package mq

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcmongo "github.com/testcontainers/testcontainers-go/modules/mongodb"
)

// startMongo brings up a single-node replica-set mongo container and returns
// its connection URI.
func startMongo(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("mongo mq tests require a mongo container")
	}
	ctx := context.Background()
	container, err := tcmongo.Run(ctx, "mongo:7", tcmongo.WithReplicaSet("rs0"))
	if err != nil {
		t.Skipf("testcontainers mongo unavailable: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	uri, err := container.ConnectionString(ctx)
	require.NoError(t, err)
	return uri
}

// TestMongo_PublishSubscribe is the smoke test: round-trip a message
// through a real MongoDB container.
func TestMongo_PublishSubscribe(t *testing.T) {
	uri := startMongo(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	bus, err := NewMongo(ctx, MongoConfig{
		URI:                    uri,
		Database:               "snoozetest",
		ServerSelectionTimeout: 15 * time.Second,
	})
	require.NoError(t, err)
	defer bus.Close()

	received := make(chan Message, 4)
	require.NoError(t, bus.Subscribe(ctx, "q1", SubscribeOpts{
		BatchSize:   2,
		BatchTimer:  500 * time.Millisecond,
		Concurrency: 1,
	}, func(_ context.Context, m Message) error {
		received <- m
		return nil
	}))

	require.NoError(t, bus.Publish(ctx, "q1", map[string]string{"hello": "world"}))

	select {
	case m := <-received:
		require.Equal(t, "q1", m.Queue)
		var body map[string]string
		require.NoError(t, json.Unmarshal(m.Payload, &body))
		require.Equal(t, "world", body["hello"])
	case <-time.After(15 * time.Second):
		t.Fatal("no message received")
	}
}
