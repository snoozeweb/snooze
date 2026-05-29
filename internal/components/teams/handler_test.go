package teams

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/pkg/snoozeclient"
)

// stubCommentClient captures every PostComment / CreateSnooze call so the
// tests can assert on the exact wire shape the handler builds.
type stubCommentClient struct {
	mu        sync.Mutex
	comments  []snoozeclient.Comment
	snoozes   []snoozeclient.Snooze
	postErr   error
	snoozeErr error
}

func (s *stubCommentClient) PostComment(_ context.Context, c snoozeclient.Comment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.postErr != nil {
		return s.postErr
	}
	s.comments = append(s.comments, c)
	return nil
}

func (s *stubCommentClient) CreateSnooze(_ context.Context, sn snoozeclient.Snooze) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snoozeErr != nil {
		return s.snoozeErr
	}
	s.snoozes = append(s.snoozes, sn)
	return nil
}

// stubReplyPoster captures the body of every in-thread reply so we can
// assert the handler posts the right confirmation message and that
// ReplyToID threads it correctly under the user's command.
type stubReplyPoster struct {
	mu      sync.Mutex
	posts   []stubReply
	sendErr error
}

type stubReply struct {
	team, channel, body, replyTo string
}

func (s *stubReplyPoster) sendMessage(_ context.Context, team, channel, body string, opts sendOpts) (graphMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sendErr != nil {
		return graphMessage{}, s.sendErr
	}
	s.posts = append(s.posts, stubReply{team, channel, body, opts.ReplyToID})
	return graphMessage{ID: "reply-" + opts.ReplyToID}, nil
}

func newTestHandler(t *testing.T, populate map[string]string) (*handler, *stubCommentClient, *stubReplyPoster) {
	t.Helper()
	cli := &stubCommentClient{}
	gc := &stubReplyPoster{}
	cache := newThreadCache(0)
	const team, channel = "T", "C"
	for thread, uid := range populate {
		cache.Put("teams/"+team+"/channels/"+channel, thread, uid)
	}
	h := newHandler(cli, gc, cache, team, channel, "SnoozeBot",
		slog.New(slog.NewTextHandler(testingDiscardWriter{}, nil)))
	return h, cli, gc
}

// testingDiscardWriter swallows handler log output to keep `go test -v` clean.
type testingDiscardWriter struct{}

func (testingDiscardWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestHandler_AckPostsCommentAndReplies(t *testing.T) {
	h, cli, gc := newTestHandler(t, map[string]string{"root-1": "rec-uid-1"})

	ok := h.Handle(context.Background(), command{
		Verb:     "ack",
		Args:     "investigated",
		Speaker:  "alice",
		ThreadID: "root-1",
	})
	require.True(t, ok)

	require.Len(t, cli.comments, 1)
	c := cli.comments[0]
	require.Equal(t, "rec-uid-1", c.RecordUID)
	require.Equal(t, "ack", c.Type)
	require.Equal(t, "investigated", c.Message)
	require.Contains(t, c.Name, "alice")

	require.Len(t, gc.posts, 1)
	require.Equal(t, "root-1", gc.posts[0].replyTo,
		"confirmation must reply under the user's thread root")
	require.Contains(t, gc.posts[0].body, "acknowledged")
	require.Contains(t, gc.posts[0].body, "alice")
}

func TestHandler_SnoozeCreatesEntryAndAcks(t *testing.T) {
	h, cli, gc := newTestHandler(t, map[string]string{"root-1": "rec-uid-1"})

	ok := h.Handle(context.Background(), command{
		Verb:     "snooze",
		Args:     "6h",
		Speaker:  "alice",
		ThreadID: "root-1",
	})
	require.True(t, ok)

	require.Len(t, cli.snoozes, 1, "expected one snooze entry")
	snooze := cli.snoozes[0]
	require.Contains(t, snooze.Name, "6 hour(s)")
	require.Contains(t, snooze.Name, "alice")
	// Condition narrows the snooze to this specific record.
	cond := snooze.Condition.([]any)
	require.Equal(t, []any{"=", "uid", "rec-uid-1"}, cond)
	require.NotNil(t, snooze.TimeConstraints, "finite snooze must carry a window")

	// Best-effort ack accompanies the snooze.
	require.Len(t, cli.comments, 1)
	require.Equal(t, "ack", cli.comments[0].Type)

	require.Len(t, gc.posts, 1)
	require.Contains(t, gc.posts[0].body, "Snoozed for")
	require.Contains(t, gc.posts[0].body, "6 hour(s)")
}

func TestHandler_SnoozeForeverNoWindow(t *testing.T) {
	h, cli, _ := newTestHandler(t, map[string]string{"root-1": "rec-uid-1"})
	ok := h.Handle(context.Background(), command{
		Verb:     "snooze",
		Args:     "forever",
		Speaker:  "alice",
		ThreadID: "root-1",
	})
	require.True(t, ok)
	require.Len(t, cli.snoozes, 1)
	require.Nil(t, cli.snoozes[0].TimeConstraints,
		"forever snooze omits time_constraints — matches Python")
}

func TestHandler_SnoozeRejectsBadDuration(t *testing.T) {
	h, cli, gc := newTestHandler(t, map[string]string{"root-1": "rec-uid-1"})
	ok := h.Handle(context.Background(), command{
		Verb:     "snooze",
		Args:     "garbage",
		Speaker:  "alice",
		ThreadID: "root-1",
	})
	require.True(t, ok)
	require.Empty(t, cli.snoozes, "bad duration must not create a snooze")
	require.Empty(t, cli.comments, "and must not piggyback an ack")
	require.Len(t, gc.posts, 1)
	require.Contains(t, gc.posts[0].body, "Invalid Snooze duration")
}

func TestHandler_EscParsesModifications(t *testing.T) {
	h, cli, gc := newTestHandler(t, map[string]string{"root-1": "rec-uid-1"})
	ok := h.Handle(context.Background(), command{
		Verb:     "esc",
		Args:     `severity = critical please re-check`,
		Speaker:  "alice",
		ThreadID: "root-1",
	})
	require.True(t, ok)
	require.Len(t, cli.comments, 1)
	c := cli.comments[0]
	require.Equal(t, "esc", c.Type)
	require.Equal(t, [][]any{{"SET", "severity", "critical"}}, c.Modifications)
	require.Equal(t, "please re-check", c.Message)
	require.Len(t, gc.posts, 1)
	require.Contains(t, gc.posts[0].body, "re-escalated")
}

func TestHandler_UnknownThreadRepliesHelpfully(t *testing.T) {
	// No cache population — the lookup will miss and the handler should
	// surface a friendly error instead of silently dropping the action.
	h, cli, gc := newTestHandler(t, nil)
	ok := h.Handle(context.Background(), command{
		Verb:     "ack",
		Speaker:  "alice",
		ThreadID: "unknown-thread",
	})
	require.True(t, ok)
	require.Empty(t, cli.comments)
	require.Len(t, gc.posts, 1)
	require.Contains(t, gc.posts[0].body, "don't recognise")
}

func TestHandler_DefaultIsPlainComment(t *testing.T) {
	h, cli, gc := newTestHandler(t, map[string]string{"root-1": "rec-uid-1"})
	ok := h.Handle(context.Background(), command{
		Verb:     "anyword",
		Args:     "the rest",
		Speaker:  "alice",
		ThreadID: "root-1",
	})
	require.True(t, ok)
	require.Len(t, cli.comments, 1)
	c := cli.comments[0]
	require.Equal(t, "", c.Type, "unrecognised verb falls through to a plain comment (no state change)")
	require.Equal(t, "anyword the rest", c.Message)
	require.Len(t, gc.posts, 1)
	require.Contains(t, gc.posts[0].body, "Comment added")
}

func TestHandler_HelpDoesNotNeedThread(t *testing.T) {
	h, cli, gc := newTestHandler(t, nil) // empty cache: help still works
	ok := h.Handle(context.Background(), command{
		Verb:     "help",
		Speaker:  "alice",
		ThreadID: "root-1",
	})
	require.True(t, ok)
	require.Empty(t, cli.comments)
	require.Empty(t, cli.snoozes)
	require.Len(t, gc.posts, 1)
	require.Contains(t, gc.posts[0].body, "ack")
	require.Contains(t, gc.posts[0].body, "snooze")
}

func TestHandler_CommentFailureSurfacesToChat(t *testing.T) {
	h, _, gc := newTestHandler(t, map[string]string{"root-1": "rec-uid-1"})
	// Force the snoozeclient stub to fail every PostComment call so we
	// see the handler's error-path reply.
	(h.cli).(*stubCommentClient).postErr = errors.New("503 service unavailable")

	ok := h.Handle(context.Background(), command{
		Verb:     "ack",
		Speaker:  "alice",
		ThreadID: "root-1",
	})
	require.True(t, ok)
	require.Len(t, gc.posts, 1)
	require.True(t, strings.HasPrefix(gc.posts[0].body, "<p>❌"),
		"failure reply must surface as an error in the channel, got: %s", gc.posts[0].body)
	require.Contains(t, gc.posts[0].body, "503 service unavailable")
}
