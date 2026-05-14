// Package schema defines the per-section structs of the bootstrap configuration.
package schema

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// Duration is a time.Duration that accepts either a Go-style duration string
// (e.g. "5m", "172800s") or a bare number expressed in seconds (e.g. 172800
// or 172800.0). The number form is what the legacy Python YAML config emits
// because Pydantic serialises timedeltas as floats.
type Duration time.Duration

// String returns the canonical Go duration representation.
func (d Duration) String() string { return time.Duration(d).String() }

// AsDuration returns the underlying time.Duration value.
func (d Duration) AsDuration() time.Duration { return time.Duration(d) }

// UnmarshalText satisfies encoding.TextUnmarshaler so koanf (via mapstructure)
// and other text-based decoders can parse Duration values.
func (d *Duration) UnmarshalText(text []byte) error {
	s := string(text)
	if s == "" {
		*d = 0
		return nil
	}
	// Try a Go duration first.
	if v, err := time.ParseDuration(s); err == nil {
		*d = Duration(v)
		return nil
	}
	// Fall back to interpreting the value as seconds.
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		*d = Duration(time.Duration(n * float64(time.Second)))
		return nil
	}
	return fmt.Errorf("invalid duration %q", s)
}

// MarshalText satisfies encoding.TextMarshaler.
func (d Duration) MarshalText() ([]byte, error) {
	return []byte(time.Duration(d).String()), nil
}

// UnmarshalJSON also accepts JSON numbers and strings so callers can decode
// settings coming from the DB-backed runtime store.
func (d *Duration) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		*d = 0
		return nil
	}
	// String form: "5m".
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		return d.UnmarshalText([]byte(s))
	}
	// Number form: 172800 (seconds).
	var n float64
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*d = Duration(time.Duration(n * float64(time.Second)))
	return nil
}

// MarshalJSON renders the value as a Go duration string.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}
