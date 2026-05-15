package jira

import "strings"

// ADF is the Atlassian Document Format envelope JIRA Cloud expects for issue
// descriptions and comments. We model only the subset we actually emit
// (paragraphs, headings, marked text) — the format is large but the daemon
// never needs more than this.
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

// textADF builds an ADF document from plain text. Each newline-delimited
// non-empty line becomes a paragraph; blank lines become empty paragraphs so
// formatting is preserved across multi-line description templates.
func textADF(text string) ADF {
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

// recordSummary is the shape of an inbound Snooze record. We use a free-form
// map (not pkg/snoozetypes.Record) because the webhook payload carries
// plugin-injected fields like `hash` and `snooze_webhook_responses` that
// don't live on the typed struct.
type recordSummary = map[string]any

// strField returns rec[key] as a trimmed string, or fallback when the key is
// missing or not a string. Float64 and int values are also formatted to keep
// JSON-decoded numbers usable.
func strField(rec recordSummary, key, fallback string) string {
	v, ok := rec[key]
	if !ok || v == nil {
		return fallback
	}
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return fallback
		}
		return s
	default:
		return fallback
	}
}

// buildDescriptionADF renders the default rich-text description for a new
// issue: a "Snooze Alert" heading, a row per canonical field, the message
// (if any), and a clickable link back to the Snooze UI.
func buildDescriptionADF(rec recordSummary, snoozeURL string) ADF {
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
		doc.Content = append(doc.Content, labeledLine(kv.label, strField(rec, kv.key, "Unknown")))
	}
	if msg := strField(rec, "message", ""); msg != "" {
		doc.Content = append(doc.Content, labeledLine("Message", msg))
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

// labeledLine returns a paragraph with a bold "<label>: " prefix followed by
// the value. Used by the default description renderer.
func labeledLine(label, value string) ADFBlock {
	return ADFBlock{
		Type: "paragraph",
		Content: []ADFInline{
			{Type: "text", Text: label + ": ", Marks: []ADFMark{{Type: "strong"}}},
			{Type: "text", Text: value},
		},
	}
}

// appendStrongLine appends a bold-prefixed paragraph to doc and returns it.
// Used by the daemon to splice extras like "Custom message" / notification
// origin onto a description after the canonical fields.
func appendStrongLine(doc ADF, label, value string) ADF {
	doc.Content = append(doc.Content, labeledLine(label, value))
	return doc
}

// appendPlainLine appends a single-paragraph plain-text line to doc.
func appendPlainLine(doc ADF, text string) ADF {
	doc.Content = append(doc.Content, ADFBlock{
		Type:    "paragraph",
		Content: []ADFInline{{Type: "text", Text: text}},
	})
	return doc
}
