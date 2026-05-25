package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// OrderedFields is a YAML-mapping-shaped collection that preserves the
// declaration order of its entries through both YAML unmarshal and JSON
// marshal. Used for `action_form` and `setting_form` in metadata.yaml: the
// frontend renders fields top-to-bottom in the order they appear here, so a
// plain map[string]any (whose JSON marshal would alphabetise keys) silently
// reorders the UI relative to the YAML the operator wrote.
//
// The JSON wire shape stays a JSON object — JS Object.keys preserves
// insertion order for string keys — so existing frontend callers iterating
// with `Object.entries(action_form)` work unchanged.
//
// Iteration via the Pairs slice keeps both the key and the field descriptor
// addressable in order; the Map mirror is provided for O(1) lookups by name.
type OrderedFields struct {
	// Pairs holds (key, value) in YAML declaration order.
	Pairs []OrderedField
	// Map mirrors Pairs for fast lookup-by-key access; updated together
	// with Pairs by UnmarshalYAML.
	Map map[string]any
}

// OrderedField is a single (key, value) pair from a YAML mapping.
type OrderedField struct {
	Key   string
	Value any
}

// Len returns the number of pairs. Used by require.NotEmpty and friends.
func (o OrderedFields) Len() int { return len(o.Pairs) }

// Get returns the value for key, or (nil, false) when absent. O(1) via Map.
func (o OrderedFields) Get(key string) (any, bool) {
	if o.Map == nil {
		return nil, false
	}
	v, ok := o.Map[key]
	return v, ok
}

// UnmarshalYAML accepts a YAML mapping node and captures its entries in
// declaration order. A nil or non-mapping input becomes a zero-value
// OrderedFields rather than an error so optional fields can be absent from
// the YAML without forcing every consumer to special-case nil.
func (o *OrderedFields) UnmarshalYAML(value *yaml.Node) error {
	if value == nil || value.Kind == 0 {
		*o = OrderedFields{}
		return nil
	}
	// Tolerate `null` explicitly written into YAML for an action_form.
	if value.Tag == "!!null" {
		*o = OrderedFields{}
		return nil
	}
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("OrderedFields: expected mapping node, got kind=%d", value.Kind)
	}
	n := len(value.Content) / 2
	pairs := make([]OrderedField, 0, n)
	m := make(map[string]any, n)
	for i := 0; i < len(value.Content); i += 2 {
		var k string
		if err := value.Content[i].Decode(&k); err != nil {
			return fmt.Errorf("OrderedFields: key #%d: %w", i/2, err)
		}
		var v any
		if err := value.Content[i+1].Decode(&v); err != nil {
			return fmt.Errorf("OrderedFields: value for %q: %w", k, err)
		}
		pairs = append(pairs, OrderedField{Key: k, Value: v})
		m[k] = v
	}
	o.Pairs = pairs
	o.Map = m
	return nil
}

// MarshalJSON writes the entries as a JSON object preserving the order of
// Pairs. Goes through encoding/json for each value so nested maps/arrays
// follow their usual rules. An empty/zero OrderedFields marshals to null;
// the parent `omitempty` tag then keeps it out of the wire payload.
func (o OrderedFields) MarshalJSON() ([]byte, error) {
	if len(o.Pairs) == 0 {
		return []byte("null"), nil
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, p := range o.Pairs {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(p.Key)
		if err != nil {
			return nil, fmt.Errorf("OrderedFields: marshal key %q: %w", p.Key, err)
		}
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := json.Marshal(p.Value)
		if err != nil {
			return nil, fmt.Errorf("OrderedFields: marshal value for %q: %w", p.Key, err)
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UnmarshalJSON is the inverse of MarshalJSON, intended for the rare round
// trip (the metadata HTTP layer never reads the field back, but tests do).
// JSON itself doesn't preserve object key order, so this falls back to the
// document-order Go's decoder provides, which is the source order in modern
// encoders. Good enough for tests; the YAML path is authoritative for the
// production order.
func (o *OrderedFields) UnmarshalJSON(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok == nil {
		*o = OrderedFields{}
		return nil
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return fmt.Errorf("OrderedFields: expected '{', got %v", tok)
	}
	var pairs []OrderedField
	m := make(map[string]any)
	for dec.More() {
		ktok, err := dec.Token()
		if err != nil {
			return err
		}
		k, ok := ktok.(string)
		if !ok {
			return fmt.Errorf("OrderedFields: non-string key %v", ktok)
		}
		var v any
		if err := dec.Decode(&v); err != nil {
			return fmt.Errorf("OrderedFields: decode value for %q: %w", k, err)
		}
		pairs = append(pairs, OrderedField{Key: k, Value: v})
		m[k] = v
	}
	// Consume the closing '}'.
	if _, err := dec.Token(); err != nil {
		return err
	}
	o.Pairs = pairs
	o.Map = m
	return nil
}
