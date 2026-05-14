package smtp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/japannext/snooze/pkg/snoozeclient"
	"github.com/japannext/snooze/pkg/snoozetypes"
)

// ReceivedMessage is the daemon's intermediate representation of an accepted
// inbound mail. It bundles SMTP envelope data and the parsed RFC 5322 body so
// forward.go can map both into a snoozetypes.Record.
type ReceivedMessage struct {
	// MailFrom is the address from the SMTP "MAIL FROM:<...>" verb.
	MailFrom string
	// RcptTo is the list of accepted RCPT TO addresses.
	RcptTo []string
	// Peer is the client's "host:port" as seen by the listener.
	Peer string
	// Helo is the value the client used in HELO/EHLO.
	Helo string
	// Auth records the AUTH PLAIN username, if AUTH succeeded.
	Auth string
	// Data is the raw DATA blob (RFC 5322 message, including headers).
	Data []byte
}

// Forwarder converts ReceivedMessage values into snoozetypes.Record and POSTs
// them via pkg/snoozeclient. It is a tiny shim so the SMTP server doesn't need
// to know anything about HTTP.
type Forwarder struct {
	client *snoozeclient.Client
	cfg    Config
}

// NewForwarder returns a Forwarder bound to c. The client must already be
// configured for login; the daemon calls Login during startup.
func NewForwarder(c *snoozeclient.Client, cfg Config) *Forwarder {
	return &Forwarder{client: c, cfg: cfg}
}

// Forward maps msg to a Record and POSTs it to /api/v1/alerts.
func (f *Forwarder) Forward(ctx context.Context, msg ReceivedMessage) error {
	if f.client == nil {
		return fmt.Errorf("smtp: forwarder has no client configured")
	}
	rec, err := ToRecord(msg, f.cfg)
	if err != nil {
		return fmt.Errorf("smtp: map record: %w", err)
	}
	if _, err := f.client.PostAlert(ctx, rec); err != nil {
		return fmt.Errorf("smtp: post alert: %w", err)
	}
	return nil
}

// ToRecord parses the RFC 5322 mail in msg.Data and converts it (plus the SMTP
// envelope) into the canonical snoozetypes.Record. Exposed so tests can
// exercise the field-mapping logic without spinning up an HTTP client.
func ToRecord(msg ReceivedMessage, cfg Config) (snoozetypes.Record, error) {
	parsed, perr := mail.ReadMessage(bytes.NewReader(msg.Data))
	// We tolerate malformed mail: if the parser fails outright we still build a
	// record from the envelope. This keeps the daemon useful for senders that
	// emit non-conforming bodies (e.g. monitoring appliances with broken
	// libraries).
	headers := make(map[string]string)
	var subject string
	var dateHeader string
	body := ""
	if perr == nil && parsed != nil {
		for k, vs := range parsed.Header {
			if len(vs) == 0 {
				continue
			}
			headers[k] = strings.Join(vs, ", ")
		}
		subject = decodeMIME(parsed.Header.Get("Subject"))
		dateHeader = parsed.Header.Get("Date")
		body = extractTextBody(parsed.Header.Get("Content-Type"), parsed.Body)
	}

	host, fqdn := deriveHost(msg.MailFrom, headers["Received"], cfg)
	severity := guessSeverity(subject)
	timestamp := parseDate(dateHeader)

	raw := map[string]any{
		"from":    msg.MailFrom,
		"to":      msg.RcptTo,
		"peer":    msg.Peer,
		"helo":    msg.Helo,
		"subject": subject,
		"headers": headers,
	}
	if cc := headers["Cc"]; cc != "" {
		raw["cc"] = cc
	}
	if fqdn != "" && fqdn != host {
		raw["fqdn"] = fqdn
	}
	if msg.Auth != "" {
		raw["auth_user"] = msg.Auth
	}

	process := subject
	if process == "" {
		process = "smtp"
	}

	return snoozetypes.Record{
		Host:      host,
		Source:    "smtp",
		Process:   process,
		Severity:  severity,
		Message:   body,
		Timestamp: timestamp,
		Raw:       raw,
	}, nil
}

// severityKeywords maps subject-line keywords to the canonical Record.Severity
// value. The first match wins (case-insensitive whole-word).
var severityKeywords = []struct {
	kw   string
	sev  string
	rxre *regexp.Regexp
}{
	{kw: "fatal", sev: "crit"},
	{kw: "critical", sev: "crit"},
	{kw: "crit", sev: "crit"},
	{kw: "alert", sev: "alert"},
	{kw: "emerg", sev: "emerg"},
	{kw: "emergency", sev: "emerg"},
	{kw: "error", sev: "err"},
	{kw: "err", sev: "err"},
	{kw: "warning", sev: "warning"},
	{kw: "warn", sev: "warning"},
	{kw: "notice", sev: "notice"},
	{kw: "info", sev: "info"},
	{kw: "debug", sev: "debug"},
	{kw: "ok", sev: "info"},
	{kw: "success", sev: "info"},
}

func init() {
	for i := range severityKeywords {
		severityKeywords[i].rxre = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(severityKeywords[i].kw) + `\b`)
	}
}

// guessSeverity inspects subject for a known severity keyword. When none is
// found it returns "warning" — the documented default for unannotated alerts.
func guessSeverity(subject string) string {
	for _, e := range severityKeywords {
		if e.rxre.MatchString(subject) {
			return e.sev
		}
	}
	return "warning"
}

// parseDate parses an RFC 5322 Date header with a few common fallbacks. When
// the header is missing or unparseable the function returns time.Now().UTC().
func parseDate(s string) time.Time {
	if s == "" {
		return time.Now().UTC()
	}
	if t, err := mail.ParseDate(s); err == nil {
		return t
	}
	// Last-resort: try a few common non-conforming layouts.
	for _, layout := range []string{
		time.RFC1123Z,
		time.RFC1123,
		time.RFC3339,
		"2006-01-02 15:04:05 -0700",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Now().UTC()
}

// deriveHost picks the Record.Host value. Preference order:
//  1. domain portion of MAIL FROM, with the local-domain stripped when it
//     matches one of cfg.LocalDomains.
//  2. "from" portion of the first Received header (parsed best-effort).
//  3. literal MAIL FROM (when no '@' is present).
//
// fqdn is the un-stripped fully-qualified hostname; callers store it in Raw
// when it differs from host.
func deriveHost(mailfrom, received string, cfg Config) (host, fqdn string) {
	if _, dom, ok := splitAddress(strings.ToLower(strings.TrimSpace(mailfrom))); ok {
		fqdn = dom
		host = dom
		// If domain is short.localdomain and localdomain is owned, strip it.
		if i := strings.IndexByte(dom, '.'); i > 0 {
			suffix := dom[i+1:]
			if cfg.isLocalDomain(suffix) {
				host = dom[:i]
			}
		}
		return host, fqdn
	}
	if r := parseReceivedFrom(received); r != "" {
		return r, r
	}
	if mailfrom != "" {
		return mailfrom, ""
	}
	return "", ""
}

// receivedFromRe captures the "from <host>" component at the start of a
// Received header. The regex is intentionally permissive — Received is a
// notoriously freeform field.
var receivedFromRe = regexp.MustCompile(`(?i)^from\s+([^\s(]+)`)

// parseReceivedFrom returns the host from the first "from ..." token of a
// Received header, or "" when the header is empty or malformed.
func parseReceivedFrom(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if m := receivedFromRe.FindStringSubmatch(s); len(m) == 2 {
		return strings.ToLower(m[1])
	}
	return ""
}

// decodeMIME runs the input through mime.WordDecoder so encoded-word headers
// (=?UTF-8?B?...?=) come back as plain UTF-8. Malformed inputs are returned
// unchanged.
func decodeMIME(s string) string {
	if s == "" {
		return ""
	}
	dec := new(mime.WordDecoder)
	out, err := dec.DecodeHeader(s)
	if err != nil {
		return s
	}
	return out
}

// extractTextBody returns a best-effort plain-text rendering of the body. It
// handles:
//   - text/plain                 — returned verbatim
//   - text/html                  — HTML tags stripped
//   - multipart/{alternative,mixed,related} — first text/plain part wins,
//     falling back to text/html → stripped.
//
// Anything else is returned as-is.
func extractTextBody(contentType string, body io.Reader) string {
	if body == nil {
		return ""
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType == "" {
		mediaType = "text/plain"
	}
	switch {
	case strings.HasPrefix(mediaType, "multipart/"):
		raw, _ := io.ReadAll(body)
		return pickMultipart(raw, params["boundary"])
	case mediaType == "text/html":
		raw, _ := io.ReadAll(body)
		return stripHTML(string(decodeBodyBytes(raw, params["charset"])))
	default:
		raw, _ := io.ReadAll(body)
		return string(decodeBodyBytes(raw, params["charset"]))
	}
}

// pickMultipart walks the multipart body and returns the first text/plain
// part, falling back to text/html (stripped) when no plain part exists.
func pickMultipart(raw []byte, boundary string) string {
	if boundary == "" {
		return string(raw)
	}
	mr := multipart.NewReader(bytes.NewReader(raw), boundary)
	var htmlBody string
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		ct := part.Header.Get("Content-Type")
		mediaType, params, perr := mime.ParseMediaType(ct)
		if perr != nil {
			mediaType = "text/plain"
		}
		data, _ := io.ReadAll(part)
		_ = part.Close()
		decoded := decodeBodyBytes(data, params["charset"])
		switch {
		case mediaType == "text/plain":
			return string(decoded)
		case mediaType == "text/html" && htmlBody == "":
			htmlBody = stripHTML(string(decoded))
		case strings.HasPrefix(mediaType, "multipart/"):
			// Nested multipart — recurse.
			if sub := pickMultipart(decoded, params["boundary"]); sub != "" {
				return sub
			}
		}
	}
	return htmlBody
}

// decodeBodyBytes returns raw unchanged. Charset transcoding (e.g. ISO-8859-1
// → UTF-8) is intentionally NOT performed: the dependency footprint of
// golang.org/x/text would be disproportionate, and the receive side of the
// Snooze ecosystem is overwhelmingly UTF-8 / ASCII. Callers wanting full
// charset support can layer it later.
func decodeBodyBytes(raw []byte, _ string) []byte { return raw }

// htmlTagRe matches a single HTML tag (open, close, or self-closing).
var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

// htmlWSRe collapses runs of whitespace introduced by tag removal.
var htmlWSRe = regexp.MustCompile(`[ \t]+`)

// stripHTML returns a crude plain-text version of an HTML fragment. It is not
// a full renderer — it removes tags, decodes a handful of entities and
// collapses whitespace. Good enough for the "alert body in an HTML email"
// case that monitoring senders typically produce.
func stripHTML(s string) string {
	out := htmlTagRe.ReplaceAllString(s, "")
	out = strings.ReplaceAll(out, "&nbsp;", " ")
	out = strings.ReplaceAll(out, "&lt;", "<")
	out = strings.ReplaceAll(out, "&gt;", ">")
	out = strings.ReplaceAll(out, "&amp;", "&")
	out = strings.ReplaceAll(out, "&quot;", `"`)
	out = strings.ReplaceAll(out, "&#39;", "'")
	out = htmlWSRe.ReplaceAllString(out, " ")
	// Drop blank lines a tag removal often leaves behind.
	lines := strings.Split(out, "\n")
	kept := lines[:0]
	for _, l := range lines {
		if t := strings.TrimSpace(l); t != "" {
			kept = append(kept, t)
		}
	}
	return strings.Join(kept, "\n")
}
