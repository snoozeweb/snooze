package teams

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/snoozeweb/snooze/pkg/snoozetypes"
)

func recWithHash(host, hash string) snoozetypes.Record {
	return snoozetypes.Record{
		Host:  host,
		Extra: map[string]any{"hash": hash},
	}
}

// TestRecordWebURL_NewReactRoute locks in the URL shape the React 2.0 web
// router understands. AlertsPage at /web/alerts reads the `search` query
// param once on mount and seeds the SearchBar, so the recipient lands on
// the alerts list filtered to the matching hash.
//
// The legacy `/web/?#/record?tab=All&s=hash%3D…` Vue URL is dead — the new
// router has no hash-routing and no `/web/record` route.
func TestRecordWebURL_NewReactRoute(t *testing.T) {
	got := recordWebURL(recWithHash("db-1.example.com", "abc123"), "https://snooze.example.com/")
	require.Equal(t, "https://snooze.example.com/web/alerts?search=hash+%3D+abc123", got,
		"trailing slashes get trimmed and `hash = <hash>` is url-escaped")
}

func TestRecordWebURL_Empty(t *testing.T) {
	t.Run("no snooze URL", func(t *testing.T) {
		require.Equal(t, "", recordWebURL(recWithHash("h", "abc"), ""))
	})
	t.Run("no hash on record", func(t *testing.T) {
		require.Equal(t, "", recordWebURL(snoozetypes.Record{Host: "h"}, "https://snooze.example.com"))
	})
}

// TestHostText_MarkdownLink covers the helper that lives inside TextBlocks
// (which Teams DOES render Markdown for). The previous fact-value path was
// silently stripped by Teams; this test guards against that regression.
func TestHostText_MarkdownLink(t *testing.T) {
	got := hostText(recWithHash("db-1.example.com", "abc123"), "https://snooze.example.com")
	require.Equal(t, "[db-1.example.com](https://snooze.example.com/web/alerts?search=hash+%3D+abc123)", got)
}

func TestHostText_BareWhenURLOrHashMissing(t *testing.T) {
	t.Run("no snooze URL", func(t *testing.T) {
		require.Equal(t, "h", hostText(recWithHash("h", "abc"), ""))
	})
	t.Run("no hash", func(t *testing.T) {
		require.Equal(t, "h", hostText(snoozetypes.Record{Host: "h"}, "https://snooze.example.com"))
	})
	t.Run("empty host", func(t *testing.T) {
		got := hostText(recWithHash("", "abc"), "https://snooze.example.com")
		require.True(t, strings.Contains(got, "Unknown"))
	})
}

// TestBuildAlertCard_HostFactAndOpenUrlAction is the layout matching the
// Python 1.x bot: Host lives in the FactSet with its label so operators
// see "Host: <hostname>" alongside Source/Process/Severity. The value
// renders Markdown per the AdaptiveCards spec, but because some Teams
// clients silently drop FactSet-value Markdown, the card also exposes a
// card-level Action.OpenUrl button that's guaranteed clickable on every
// renderer.
func TestBuildAlertCard_HostFactAndOpenUrlAction(t *testing.T) {
	rec := snoozetypes.Record{
		Host:     "srv-x.example.com",
		Source:   "syslog",
		Process:  "kernel",
		Severity: "warning",
		Message:  "oom-killer invoked",
		Extra:    map[string]any{"hash": "deadbeef"},
	}
	card := buildAlertCard(rec, "https://snooze.example.com")
	body := card["body"].([]map[string]any)

	var factSet map[string]any
	for _, item := range body {
		if item["type"] == "FactSet" {
			factSet = item
			break
		}
	}
	require.NotNil(t, factSet, "expected a FactSet in the body, got: %s", mustJSON(t, body))
	facts := factSet["facts"].([]map[string]any)
	// The Host label must be present and first — restores the regression
	// from the brief "TextBlock-only" detour that hid the label entirely.
	require.NotEmpty(t, facts)
	require.Equal(t, "Host", facts[0]["title"], "Host must be the first labelled fact")
	hostValue := facts[0]["value"].(string)
	require.Contains(t, hostValue, "srv-x.example.com")
	require.Contains(t, hostValue, "/web/alerts?search=hash+%3D+deadbeef",
		"fact value carries the Markdown link for renderers that honor it")

	// Spot-check the remaining facts.
	titles := []string{}
	for _, f := range facts[1:] {
		titles = append(titles, f["title"].(string))
	}
	require.ElementsMatch(t, []string{"Source", "Process", "Severity"}, titles)

	// Card-level Action.OpenUrl — the always-clickable backup that survives
	// Teams clients that strip Markdown from FactSet values.
	actions, ok := card["actions"].([]map[string]any)
	require.True(t, ok, "expected card.actions to be set, got %s", mustJSON(t, card))
	require.Len(t, actions, 1)
	require.Equal(t, "Action.OpenUrl", actions[0]["type"])
	require.Equal(t, "View in Snooze", actions[0]["title"])
	require.Equal(t, "https://snooze.example.com/web/alerts?search=hash+%3D+deadbeef", actions[0]["url"])
}

// TestBuildAlertCard_NoActionWhenHashMissing exercises the degraded path:
// no record hash → no per-record URL → no Action.OpenUrl button and the
// Host fact value reverts to a bare hostname (no Markdown link).
func TestBuildAlertCard_NoActionWhenHashMissing(t *testing.T) {
	rec := snoozetypes.Record{
		Host:   "srv-x",
		Source: "syslog",
		// No Extra/hash → recordWebURL returns "" → no button + bare host text.
	}
	card := buildAlertCard(rec, "https://snooze.example.com")
	_, hasActions := card["actions"]
	require.False(t, hasActions, "no per-record URL → no Action.OpenUrl button")

	body := card["body"].([]map[string]any)
	var factSet map[string]any
	for _, item := range body {
		if item["type"] == "FactSet" {
			factSet = item
			break
		}
	}
	require.NotNil(t, factSet)
	facts := factSet["facts"].([]map[string]any)
	require.Equal(t, "Host", facts[0]["title"])
	require.Equal(t, "srv-x", facts[0]["value"],
		"with no hash, the host value is plain text, not Markdown")
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	require.NoError(t, err)
	return string(b)
}
