package aggregaterule

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/internal/db"
	"github.com/snoozeweb/snooze/internal/plugins"
	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

type closeSpy struct {
	mu     sync.Mutex
	uids   []string
	events []plugins.CloseEvent
}

func (s *closeSpy) Name() string                                 { return "notification" }
func (s *closeSpy) Metadata() plugins.Metadata                   { return plugins.Metadata{Name: "notification"} }
func (s *closeSpy) PostInit(context.Context, plugins.Host) error { return nil }
func (s *closeSpy) Reload(context.Context) error                 { return nil }
func (s *closeSpy) DispatchClose(_ context.Context, uid string, ev plugins.CloseEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.uids = append(s.uids, uid)
	s.events = append(s.events, ev)
}

// An automatic close tells the notifiers once, after the write; a duplicate
// close of an already-closed aggregate does not.
func TestAggregate_AutoCloseDispatchesToNotifiers(t *testing.T) {
	t.Parallel()
	host := newTestHost(t)
	spy := &closeSpy{}
	host.plugs["notification"] = spy
	writeRule(t, host, db.Document{
		"name": "AggN", "condition": []any{"=", "a", "n"},
		"fields": []string{"a"}, "throttle": int64(900),
	})
	p := freshPlugin(t, host)

	runProcess(t, p, host, snoozetypes.Record{Severity: "critical", Extra: map[string]any{"a": "n"}})
	uid := aggregateUID(t, host, "AggN")
	runProcess(t, p, host, snoozetypes.Record{State: "close", Severity: "ok", Extra: map[string]any{"a": "n"}})
	runProcess(t, p, host, snoozetypes.Record{State: "close", Severity: "ok", Extra: map[string]any{"a": "n"}})

	require.Equal(t, []string{uid}, spy.uids)
	require.Equal(t, "", spy.events[0].Actor)
	require.Equal(t, "Severity critical => ok", spy.events[0].Reason)
}
