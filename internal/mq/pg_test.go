package mq

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// startPG spins up a postgres container and returns the connection DSN.
func startPG(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("pg mq tests require a postgres container")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("snooze"),
		tcpostgres.WithUsername("snooze"),
		tcpostgres.WithPassword("snooze"),
		tcpostgres.BasicWaitStrategies(),
		tcpostgres.WithSQLDriver("pgx"),
	)
	if err != nil {
		t.Skipf("postgres container unavailable: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	if err := wait.ForListeningPort("5432/tcp").WaitUntilReady(ctx, container); err != nil {
		t.Fatalf("waiting for postgres: %v", err)
	}
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	return dsn
}

// TestPG_PublishSubscribe is the smoke test: round-trip a message through
// a real Postgres container.
func TestPG_PublishSubscribe(t *testing.T) {
	dsn := startPG(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	bus, err := NewPG(ctx, PGConfig{DSN: dsn, ApplicationName: "snooze-mq-test"})
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

// TestPG_AckMarksRow verifies ack_at is set on successful handler.
func TestPG_AckMarksRow(t *testing.T) {
	dsn := startPG(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	bus, err := NewPG(ctx, PGConfig{DSN: dsn})
	require.NoError(t, err)
	defer bus.Close()

	done := make(chan struct{}, 1)
	require.NoError(t, bus.Subscribe(ctx, "q-ack", SubscribeOpts{
		BatchSize: 1, BatchTimer: 300 * time.Millisecond, Concurrency: 1,
	}, func(context.Context, Message) error {
		done <- struct{}{}
		return nil
	}))
	require.NoError(t, bus.Publish(ctx, "q-ack", "v"))
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("no message")
	}
	// Give ack a moment to land.
	time.Sleep(300 * time.Millisecond)

	pb := bus.(*pgBus)
	var pending int
	require.NoError(t, pb.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM mq_messages WHERE queue='q-ack' AND ack_at IS NULL`).Scan(&pending))
	require.Equal(t, 0, pending, "expected ack_at to be set")
}
