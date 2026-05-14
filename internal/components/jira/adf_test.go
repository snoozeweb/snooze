package jira

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTextADF_paragraphsPerLine(t *testing.T) {
	doc := textADF("Hello\nWorld\n\nFooter")
	require.Equal(t, "doc", doc.Type)
	require.Equal(t, 1, doc.Version)
	require.Len(t, doc.Content, 4)
	require.Equal(t, "Hello", doc.Content[0].Content[0].Text)
	require.Equal(t, "World", doc.Content[1].Content[0].Text)
	require.Empty(t, doc.Content[2].Content, "blank line produces empty paragraph")
	require.Equal(t, "Footer", doc.Content[3].Content[0].Text)
}

func TestBuildDescriptionADF_hasLinkAndFields(t *testing.T) {
	rec := recordSummary{
		"host":      "srv-1",
		"source":    "syslog",
		"process":   "kernel",
		"severity":  "critical",
		"timestamp": "2026-05-14T10:00:00Z",
		"message":   "disk full",
		"hash":      "abc123",
	}
	doc := buildDescriptionADF(rec, "https://snooze.example.com")
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	s := string(raw)
	require.Contains(t, s, "Snooze Alert")
	require.Contains(t, s, "srv-1")
	require.Contains(t, s, "disk full")
	// json.Marshal escapes "&" as & — check the parts of the URL that
	// are not affected by HTML-escaping instead.
	require.Contains(t, s, "/web/?#/record?tab=All")
	require.Contains(t, s, "hash%3Dabc123")
	require.Contains(t, s, `"type":"link"`)
}

func TestBuildDescriptionADF_unknownFieldsFallback(t *testing.T) {
	doc := buildDescriptionADF(recordSummary{}, "")
	raw, _ := json.Marshal(doc)
	require.Contains(t, string(raw), "Unknown")
	// No link block when snooze URL is empty.
	require.NotContains(t, string(raw), `"type":"link"`)
}

func TestADFJSONRoundTrip(t *testing.T) {
	// JIRA insists on `version: 1` at the root — make sure marshal preserves it.
	doc := textADF("hi")
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	require.True(t, strings.Contains(string(raw), `"version":1`))
}
