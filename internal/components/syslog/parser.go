package syslog

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/leodido/go-syslog/v4/rfc3164"
	"github.com/leodido/go-syslog/v4/rfc5424"
)

// severityNames is the canonical lowercase mapping for the syslog severity
// numerical level. Index = severity number per RFC 5424 §6.2.1.
var severityNames = [...]string{
	"emerg",   // 0
	"alert",   // 1
	"crit",    // 2
	"err",     // 3
	"warning", // 4
	"notice",  // 5
	"info",    // 6
	"debug",   // 7
}

// facilityNames maps the syslog facility numerical level to its short name.
// We only need this for the Raw envelope; the Record's Severity field is the
// one consumers care about.
var facilityNames = [...]string{
	"kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news",
	"uucp", "cron", "authpriv", "ftp", "ntp", "audit", "alert", "clock",
	"local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7",
}

// ParsedMessage is the parser's intermediate representation. It is a
// thin, format-agnostic shape that forward.go maps to snoozetypes.Record.
type ParsedMessage struct {
	Format    string // "rfc3164" or "rfc5424"
	Facility  string
	Severity  string
	Hostname  string
	AppName   string
	ProcID    string
	MsgID     string
	Message   string
	Timestamp time.Time
	HasTime   bool
	// Structured carries RFC5424 SD-ELEMENT data, flattened into a map suitable
	// for the Record.Raw envelope. Always nil for RFC3164.
	Structured map[string]any
	// Raw is the original line as received (trimmed of the trailing newline).
	Raw string
}

// MessageParser parses one syslog line at a time. Construct it with NewParser.
// MessageParser is safe for concurrent use because the underlying leodido
// machines are created lazily per-call (cheap and stateless).
type MessageParser struct {
	mode string // "auto" | "rfc3164" | "rfc5424"
}

// NewParser builds a MessageParser. The mode argument must be one of "auto",
// "rfc3164" or "rfc5424"; an empty string is treated as "auto".
func NewParser(mode string) (*MessageParser, error) {
	if mode == "" {
		mode = "auto"
	}
	switch mode {
	case "auto", "rfc3164", "rfc5424":
		return &MessageParser{mode: mode}, nil
	default:
		return nil, fmt.Errorf("syslog: invalid parser mode %q", mode)
	}
}

// Parse parses a single syslog message and returns its normalised
// representation. Trailing CR/LF are trimmed automatically.
func (p *MessageParser) Parse(line []byte) (ParsedMessage, error) {
	trimmed := bytes.TrimRight(line, "\r\n\x00 ")
	if len(trimmed) == 0 {
		return ParsedMessage{}, fmt.Errorf("syslog: empty message")
	}

	mode := p.mode
	if mode == "auto" {
		mode = detectFormat(trimmed)
	}

	switch mode {
	case "rfc5424":
		return parseRFC5424(trimmed)
	case "rfc3164":
		return parseRFC3164(trimmed)
	default:
		return ParsedMessage{}, fmt.Errorf("syslog: unknown parser mode %q", mode)
	}
}

// detectFormat looks at the bytes immediately after the priority block to
// decide whether the line is RFC5424 (VERSION digit + space) or RFC3164. The
// 5424 framing is "<PRI>1 …", so we scan for the closing '>' and check the
// next two bytes.
func detectFormat(line []byte) string {
	if len(line) < 3 || line[0] != '<' {
		// No PRI at all — fall back to 3164 which is more permissive.
		return "rfc3164"
	}
	end := bytes.IndexByte(line, '>')
	if end < 0 || end+2 >= len(line) {
		return "rfc3164"
	}
	// RFC5424 §6: after '>' comes the VERSION (always "1" today) and a SP.
	if line[end+1] >= '1' && line[end+1] <= '9' && line[end+2] == ' ' {
		return "rfc5424"
	}
	return "rfc3164"
}

// parseRFC5424 wraps leodido's RFC5424 parser. BestEffort is enabled so that
// partially-malformed messages still produce a useful record.
func parseRFC5424(line []byte) (ParsedMessage, error) {
	m, err := rfc5424.NewParser(rfc5424.WithBestEffort()).Parse(line)
	if err != nil && m == nil {
		return ParsedMessage{}, fmt.Errorf("syslog: rfc5424 parse: %w", err)
	}
	sm, ok := m.(*rfc5424.SyslogMessage)
	if !ok {
		return ParsedMessage{}, fmt.Errorf("syslog: rfc5424 parse: unexpected type %T", m)
	}
	out := ParsedMessage{Format: "rfc5424", Raw: string(line)}
	if sm.Facility != nil {
		out.Facility = facilityName(*sm.Facility)
	}
	if sm.Severity != nil {
		out.Severity = severityName(*sm.Severity)
	}
	if sm.Hostname != nil {
		out.Hostname = *sm.Hostname
	}
	if sm.Appname != nil {
		out.AppName = *sm.Appname
	}
	if sm.ProcID != nil {
		out.ProcID = *sm.ProcID
	}
	if sm.MsgID != nil {
		out.MsgID = *sm.MsgID
	}
	if sm.Message != nil {
		out.Message = *sm.Message
	}
	if sm.Timestamp != nil {
		out.Timestamp = *sm.Timestamp
		out.HasTime = true
	}
	if sd := sm.StructuredData; sd != nil && len(*sd) > 0 {
		flat := make(map[string]any, len(*sd))
		for sdid, params := range *sd {
			inner := make(map[string]any, len(params))
			for k, v := range params {
				inner[k] = v
			}
			flat[sdid] = inner
		}
		out.Structured = flat
	}
	return out, nil
}

// parseRFC3164 wraps leodido's RFC3164 parser. The library auto-rolls the
// missing year onto the parsed timestamp so we don't need a fallback path
// beyond HasTime=false.
func parseRFC3164(line []byte) (ParsedMessage, error) {
	m, err := rfc3164.NewParser(rfc3164.WithBestEffort()).Parse(line)
	if err != nil && m == nil {
		return ParsedMessage{}, fmt.Errorf("syslog: rfc3164 parse: %w", err)
	}
	sm, ok := m.(*rfc3164.SyslogMessage)
	if !ok {
		return ParsedMessage{}, fmt.Errorf("syslog: rfc3164 parse: unexpected type %T", m)
	}
	out := ParsedMessage{Format: "rfc3164", Raw: string(line)}
	if sm.Facility != nil {
		out.Facility = facilityName(*sm.Facility)
	}
	if sm.Severity != nil {
		out.Severity = severityName(*sm.Severity)
	}
	if sm.Hostname != nil {
		out.Hostname = *sm.Hostname
	}
	if sm.Appname != nil {
		out.AppName = *sm.Appname
	}
	if sm.ProcID != nil {
		out.ProcID = *sm.ProcID
	}
	if sm.Message != nil {
		out.Message = *sm.Message
	}
	if sm.Timestamp != nil {
		out.Timestamp = *sm.Timestamp
		out.HasTime = true
	}
	// RFC3164 hostnames sometimes carry an embedded port suffix when relayed
	// — strip it for stable Record.Host values.
	if i := strings.IndexByte(out.Hostname, ':'); i > 0 {
		out.Hostname = out.Hostname[:i]
	}
	return out, nil
}

// severityName returns the canonical short name for severity v. Out-of-range
// values fall back to "info" so consumers always have a usable label.
func severityName(v uint8) string {
	if int(v) < len(severityNames) {
		return severityNames[v]
	}
	return "info"
}

// facilityName returns the short name for facility v, or "facility-N" for
// values outside the standard table (some vendors emit custom facilities).
func facilityName(v uint8) string {
	if int(v) < len(facilityNames) {
		return facilityNames[v]
	}
	return fmt.Sprintf("facility-%d", v)
}
