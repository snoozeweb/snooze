// Package jiraadf builds the Atlassian Document Format (ADF) documents JIRA
// Cloud expects for issue descriptions and comments. Shared by the snooze-jira
// daemon (internal/components/jira) and the in-process jira notifier
// (internal/pluginimpl/jira).
package jiraadf

import "strings"

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
