// Package jiraadf builds the Atlassian Document Format (ADF) documents JIRA
// Cloud expects for issue descriptions and comments. Shared by the snooze-jira
// daemon (internal/components/jira) and the in-process jira notifier
// (internal/pluginimpl/jira).
package jiraadf

import (
	"fmt"
	"strings"
)

// ADF is the document envelope JIRA Cloud expects for issue descriptions and
// comments. We model only the subset we emit (paragraphs, headings, marked
// text).
type ADF struct {
	Type    string     `json:"type"`
	Version int        `json:"version"`
	Content []ADFBlock `json:"content"`
}

// ADFBlock is one block-level node (paragraph, heading, …).
type ADFBlock struct {
	Type    string         `json:"type"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Content []ADFInline    `json:"content,omitempty"`
}

// ADFInline is one inline node (text run, optionally with marks).
type ADFInline struct {
	Type  string    `json:"type"`
	Text  string    `json:"text,omitempty"`
	Marks []ADFMark `json:"marks,omitempty"`
}

// ADFMark is a formatting mark on a text run ("strong", "em", "link", …).
type ADFMark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// RecordSummary is the free-form shape of an inbound Snooze record (the webhook
// payload carries plugin-injected fields like `hash` that don't live on the
// typed struct).
type RecordSummary = map[string]any

// TextADF builds an ADF document from plain text. Each newline-delimited
// non-empty line becomes a paragraph; blank lines become empty paragraphs.
func TextADF(text string) ADF {
	doc := ADF{Type: "doc", Version: 1}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			doc.Content = append(doc.Content, ADFBlock{Type: "paragraph"})
			continue
		}
		doc.Content = append(doc.Content, ADFBlock{
			Type:    "paragraph",
			Content: []ADFInline{{Type: "text", Text: line}},
		})
	}
	return doc
}

// strField returns rec[key] as a trimmed string, or fallback when missing/empty.
func strField(rec RecordSummary, key, fallback string) string {
	v, ok := rec[key]
	if !ok || v == nil {
		return fallback
	}
	if s, ok := v.(string); ok {
		if t := strings.TrimSpace(s); t != "" {
			return t
		}
	}
	return fallback
}

// BuildDescriptionADF renders the default rich-text description for a new issue:
// a "Snooze Alert" heading, a row per canonical field, the message (if any), and
// a clickable link back to the Snooze UI when snoozeURL is non-empty.
func BuildDescriptionADF(rec RecordSummary, snoozeURL string) ADF {
	doc := ADF{Type: "doc", Version: 1}
	doc.Content = append(doc.Content, ADFBlock{
		Type:    "heading",
		Attrs:   map[string]any{"level": 3},
		Content: []ADFInline{{Type: "text", Text: "Snooze Alert"}},
	})
	for _, kv := range [...]struct{ key, label string }{
		{"host", "Host"},
		{"source", "Source"},
		{"process", "Process"},
		{"severity", "Severity"},
		{"timestamp", "Timestamp"},
	} {
		doc.Content = append(doc.Content, LabeledLine(kv.label, strField(rec, kv.key, "Unknown")))
	}
	if msg := strField(rec, "message", ""); msg != "" {
		doc.Content = append(doc.Content, LabeledLine("Message", msg))
	}
	if snoozeURL != "" {
		hash := strField(rec, "hash", "")
		link := snoozeURL + "/web/?#/record?tab=All&s=hash%3D" + hash
		doc.Content = append(doc.Content, ADFBlock{
			Type: "paragraph",
			Content: []ADFInline{{
				Type:  "text",
				Text:  "View in Snooze",
				Marks: []ADFMark{{Type: "link", Attrs: map[string]any{"href": link}}},
			}},
		})
	}
	return doc
}

// LabeledLine returns a paragraph with a bold "<label>: " prefix followed by value.
func LabeledLine(label, value string) ADFBlock {
	return ADFBlock{
		Type: "paragraph",
		Content: []ADFInline{
			{Type: "text", Text: label + ": ", Marks: []ADFMark{{Type: "strong"}}},
			{Type: "text", Text: value},
		},
	}
}

// AppendStrongLine appends a bold-prefixed paragraph to doc and returns it.
func AppendStrongLine(doc ADF, label, value string) ADF {
	doc.Content = append(doc.Content, LabeledLine(label, value))
	return doc
}

// AppendPlainLine appends a single-paragraph plain-text line to doc.
func AppendPlainLine(doc ADF, text string) ADF {
	doc.Content = append(doc.Content, ADFBlock{
		Type:    "paragraph",
		Content: []ADFInline{{Type: "text", Text: text}},
	})
	return doc
}

// EscalationComment carries the context rendered into a re-escalation comment
// body. Every field is optional: the zero value produces the minimal
// "Re-escalation" header plus the record's own fields, which is what a caller
// with no escalation bookkeeping (the pre-escalation daemon path) produced.
type EscalationComment struct {
	// Ordinal is the human-facing escalation number ("#3"), or "" when the
	// caller does not track one.
	Ordinal string
	// Reason names the escalation producer: "timeout", "manual", "watchlist".
	Reason string
	// Actor is the operator who escalated, on a manual escalation only.
	Actor string
	// NotificationName / NotificationMsg attribute the escalation to the
	// notification rule that fired it.
	NotificationName string
	NotificationMsg  string
	// CustomMessage is an operator- or action-supplied addendum.
	CustomMessage string
}

// BuildEscalationComment renders the plain-text body posted as a comment on an
// issue Snooze has already created for this alert, instead of opening a second
// one. Plain text on purpose: the callers wrap it in a single-paragraph ADF
// document (see Client.AddComment), and a re-escalation comment is a running
// log entry rather than a formatted report — the issue description already
// carries the structured view.
//
// Shared by the snooze-jira daemon (internal/components/jira) and the
// in-process jira notifier (internal/pluginimpl/jira) so the two modes produce
// the same audit trail on the same ticket.
func BuildEscalationComment(rec RecordSummary, e EscalationComment) string {
	var b strings.Builder

	header := "Re-escalation"
	if e.Ordinal != "" {
		header += " " + e.Ordinal
	}
	if timestamp := strField(rec, "timestamp", ""); timestamp != "" {
		fmt.Fprintf(&b, "%s at %s\n", header, timestamp)
	} else {
		b.WriteString(header + "\n")
	}
	if e.Reason != "" {
		fmt.Fprintf(&b, "Reason: %s\n", e.Reason)
	}
	if e.Actor != "" {
		fmt.Fprintf(&b, "Escalated by: %s\n", e.Actor)
	}
	if e.NotificationName != "" {
		fmt.Fprintf(&b, "From %s\n", e.NotificationName)
		if e.NotificationMsg != "" {
			fmt.Fprintf(&b, "%s\n", e.NotificationMsg)
		}
	}
	fmt.Fprintf(&b, "Host: %s\n", strField(rec, "host", "Unknown"))
	fmt.Fprintf(&b, "Severity: %s\n", strField(rec, "severity", "Unknown"))
	fmt.Fprintf(&b, "Message: %s", strField(rec, "message", "No message"))
	if e.CustomMessage != "" {
		fmt.Fprintf(&b, "\nCustom message: %s", e.CustomMessage)
	}
	return b.String()
}
