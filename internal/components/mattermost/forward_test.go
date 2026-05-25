package mattermost

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeSnooze implements snoozeAPI with an in-memory record of the last call.
// Tests use it to assert that ParseCommand → Forward emits the expected
// REST verb / payload without spinning up an httptest server.
type fakeSnooze struct {
	method string
	path   string
	body   any
	err    error
}

func (f *fakeSnooze) Post(_ context.Context, path string, body, _ any) error {
	f.method = "POST"
	f.path = path
	f.body = body
	return f.err
}

func (f *fakeSnooze) Do(_ context.Context, method, path string, body, _ any) error {
	f.method = method
	f.path = path
	f.body = body
	return f.err
}

func TestParseCommand(t *testing.T) {
	cases := []struct {
		name      string
		line      string
		wantKind  CommandKind
		wantUID   string
		wantMsg   string
		stripped  bool // whether the slash prefix was stripped
		wantNoUID bool
	}{
		{name: "help", line: "/snooze help", wantKind: CmdHelp},
		{name: "empty after prefix", line: "/snooze", wantKind: CmdHelp},
		{name: "ack with uid", line: "/snooze ack abc123", wantKind: CmdAck, wantUID: "abc123"},
		{name: "ack with msg", line: "/snooze ack abc123 looking now", wantKind: CmdAck, wantUID: "abc123", wantMsg: "looking now"},
		{name: "close synonym", line: "/snooze done abc123", wantKind: CmdClose, wantUID: "abc123"},
		{name: "reopen synonym", line: "/snooze re-open abc123", wantKind: CmdReopen, wantUID: "abc123"},
		{name: "comment", line: "/snooze comment abc123 looks bad", wantKind: CmdComment, wantUID: "abc123", wantMsg: "looks bad"},
		{name: "unknown verb", line: "/snooze frob abc123", wantKind: CmdUnknown},
		{name: "no prefix", line: "ack abc123", wantKind: CmdAck, wantUID: "abc123"},
		{name: "at-mention prefix", line: "@snooze ack abc123 hi", wantKind: CmdAck, wantUID: "abc123", wantMsg: "hi"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseCommand(tc.line)
			if got.Kind != tc.wantKind {
				t.Fatalf("kind: got %v want %v", got.Kind, tc.wantKind)
			}
			if got.UID != tc.wantUID {
				t.Fatalf("uid: got %q want %q", got.UID, tc.wantUID)
			}
			if got.Message != tc.wantMsg {
				t.Fatalf("msg: got %q want %q", got.Message, tc.wantMsg)
			}
		})
	}
}

func TestForward(t *testing.T) {
	t.Run("help bypasses snooze", func(t *testing.T) {
		sc := &fakeSnooze{}
		reply := Forward(context.Background(), sc, Command{Kind: CmdHelp}, "alice")
		if !strings.Contains(reply, "available commands") {
			t.Fatalf("expected help reply, got %q", reply)
		}
		if sc.path != "" {
			t.Fatalf("help should not hit snooze; got %s %s", sc.method, sc.path)
		}
	})

	t.Run("ack hits /api/v1/comment", func(t *testing.T) {
		sc := &fakeSnooze{}
		cmd := ParseCommand("/snooze ack abc123 looking")
		reply := Forward(context.Background(), sc, cmd, "alice")
		if sc.method != "POST" || sc.path != "/api/v1/comment" {
			t.Fatalf("expected POST /api/v1/comment, got %s %s", sc.method, sc.path)
		}
		body, ok := sc.body.(commentRequest)
		if !ok {
			t.Fatalf("body type: %T", sc.body)
		}
		if body.RecordUID != "abc123" || body.Type != "ack" || body.Name != "alice" || body.Method != "mattermost" {
			t.Fatalf("unexpected body: %+v", body)
		}
		if body.Message != "looking" {
			t.Fatalf("message: %q", body.Message)
		}
		if !strings.Contains(reply, "acknowledged") {
			t.Fatalf("reply should confirm ack: %q", reply)
		}
	})

	t.Run("missing UID returns error reply without hitting snooze", func(t *testing.T) {
		sc := &fakeSnooze{}
		reply := Forward(context.Background(), sc, Command{Kind: CmdAck}, "alice")
		if sc.path != "" {
			t.Fatalf("should not hit snooze with missing UID")
		}
		if !strings.Contains(reply, "Missing alert UID") {
			t.Fatalf("expected missing-UID error; got %q", reply)
		}
	})

	t.Run("snooze error is surfaced", func(t *testing.T) {
		sc := &fakeSnooze{err: errors.New("boom")}
		cmd := ParseCommand("/snooze close abc123")
		reply := Forward(context.Background(), sc, cmd, "alice")
		if !strings.Contains(reply, "rejected") || !strings.Contains(reply, "boom") {
			t.Fatalf("expected error surfaced; got %q", reply)
		}
	})

	t.Run("unknown command", func(t *testing.T) {
		sc := &fakeSnooze{}
		reply := Forward(context.Background(), sc, Command{Kind: CmdUnknown}, "alice")
		if !strings.Contains(reply, "Unknown command") {
			t.Fatalf("expected unknown reply; got %q", reply)
		}
		if sc.path != "" {
			t.Fatalf("unknown should not hit snooze")
		}
	})
}
