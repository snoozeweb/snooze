package condition

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// MarshalJSON emits the object form: {"op":"...","field":"...","value":...,"children":[...]}.
func (c Cond) MarshalJSON() ([]byte, error) {
	type alias Cond
	return json.Marshal(alias(c))
}

// UnmarshalJSON auto-detects the input shape:
//   - `null` or `[]` → AlwaysTrue
//   - `[...]`        → legacy list form
//   - `{...}`        → object form
func (c *Cond) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*c = Cond{}
		return nil
	}
	switch trimmed[0] {
	case '[':
		var lst []any
		if err := json.Unmarshal(trimmed, &lst); err != nil {
			return fmt.Errorf("condition: %w", err)
		}
		v, err := FromList(lst)
		if err != nil {
			return err
		}
		*c = v
		return nil
	case '{':
		// Decode into a wire-friendly intermediate that accepts both `op`
		// (Go/Python-internal canonical) and `type` (the React frontend's
		// discriminated-union key). Without the `type` alias, every
		// frontend-posted condition silently degrades to AlwaysTrue.
		var wire struct {
			Op       string          `json:"op"`
			Type     string          `json:"type"`
			Field    string          `json:"field,omitempty"`
			Value    any             `json:"value,omitempty"`
			Args     json.RawMessage `json:"args,omitempty"`
			Arg      json.RawMessage `json:"arg,omitempty"`
			Children []Cond          `json:"children,omitempty"`
		}
		if err := json.Unmarshal(trimmed, &wire); err != nil {
			return fmt.Errorf("condition: %w", err)
		}
		op := wire.Op
		if op == "" {
			op = wire.Type
		}
		// Frontend ops are spelled out (ALWAYS_TRUE, EQUALS, NOT_EQUALS,
		// LT, LE, GT, GE). Map to the canonical short Go ops.
		switch op {
		case "ALWAYS_TRUE":
			op = string(OpAlwaysTrue) // ""
		case "EQUALS":
			op = string(OpEq)
		case "NOT_EQUALS":
			op = string(OpNeq)
		case "LT":
			op = string(OpLt)
		case "LE":
			op = string(OpLte)
		case "GT":
			op = string(OpGt)
		case "GE":
			op = string(OpGte)
		}
		out := Cond{Op: Op(op), Field: wire.Field, Value: wire.Value, Children: wire.Children}
		// The frontend's NOT shape is {type:"NOT", arg: <Condition>} (single
		// arg, not children). And/Or sometimes arrive as {type:"AND", args:[...]}
		// instead of children. Normalise both into Children.
		if len(out.Children) == 0 && len(wire.Args) > 0 {
			var args []Cond
			if err := json.Unmarshal(wire.Args, &args); err != nil {
				return fmt.Errorf("condition: args: %w", err)
			}
			out.Children = args
		}
		if len(out.Children) == 0 && len(wire.Arg) > 0 {
			var arg Cond
			if err := json.Unmarshal(wire.Arg, &arg); err != nil {
				return fmt.Errorf("condition: arg: %w", err)
			}
			out.Children = []Cond{arg}
		}
		if err := out.validate(); err != nil {
			return err
		}
		*c = out
		return nil
	}
	return fmt.Errorf("condition: unsupported JSON shape: %s", string(trimmed))
}

// validate rejects malformed object-form conditions at the decode boundary so a
// bad condition cannot reach a backend translator/evaluator. It guards the
// current node only — each child re-enters UnmarshalJSON and is validated on
// its own, and the legacy list form is validated structurally by FromList.
//
// The three rejections mirror what the shared SQL builder refuses:
//   - an empty operator that carries a field/value/children (a bare "" Op is
//     AlwaysTrue and must be otherwise empty — otherwise it would lower to
//     "match everything", e.g. DELETE … WHERE TRUE);
//   - NOT without exactly one child (object form can carry several; the engine
//     would silently use only the first);
//   - any operator outside the known set (post-mapping, every valid frontend op
//     is one of these, so unknown means malformed).
//
// Finer per-op arity (a leaf op needs a field, AND/OR may be empty) is left to
// the builder/evaluator, matching FromList and the engine's existing tolerance.
func (c Cond) validate() error {
	switch c.Op {
	case OpAlwaysTrue:
		if c.Field != "" || c.Value != nil || len(c.Children) != 0 {
			return &InvalidConditionError{Op: string(c.Op), Args: c, Message: "empty operator must not carry a field, value, or children"}
		}
	case OpNot:
		if len(c.Children) != 1 {
			return &InvalidConditionError{Op: string(c.Op), Args: c, Message: fmt.Sprintf("NOT expects exactly one child, got %d", len(c.Children))}
		}
	case OpAnd, OpOr, OpEq, OpNeq, OpGt, OpGte, OpLt, OpLte, OpMatches, OpExists, OpContains, OpIn, OpSearch:
		// Known operators; finer arity is deferred to the builder/evaluator.
	default:
		return &InvalidConditionError{Op: string(c.Op), Args: c, Message: "unsupported operator"}
	}
	return nil
}

// MarshalListJSON encodes the Cond in the legacy nested-list form. Returns
// `[]` for the AlwaysTrue zero value.
func (c Cond) MarshalListJSON() ([]byte, error) {
	v := c.ToList()
	return json.Marshal(v)
}

// ToList converts the Cond to the legacy nested list shape, matching Python.
func (c Cond) ToList() any {
	switch c.Op {
	case OpAlwaysTrue:
		return []any{}
	case OpAnd, OpOr:
		out := []any{string(c.Op)}
		for _, k := range c.Children {
			out = append(out, k.ToList())
		}
		return out
	case OpNot:
		out := []any{string(c.Op)}
		if len(c.Children) > 0 {
			out = append(out, c.Children[0].ToList())
		}
		return out
	case OpExists:
		return []any{string(c.Op), c.Field}
	case OpSearch:
		return []any{string(c.Op), c.Value}
	case OpIn:
		// Legacy: ['IN', value, field]
		return []any{string(c.Op), c.Value, c.Field}
	default:
		// Binary ops: =, !=, >, >=, <, <=, MATCHES, CONTAINS
		return []any{string(c.Op), c.Field, c.Value}
	}
}

// FromList parses the legacy nested-list shape into a Cond.
//
// Mirrors Python's snooze.utils.condition.get_condition.
func FromList(v any) (Cond, error) {
	if v == nil {
		return Cond{}, nil
	}
	lst, ok := v.([]any)
	if !ok {
		return Cond{}, &InvalidConditionError{Op: "UNKNOWN", Args: v, Message: "not a list"}
	}
	if len(lst) == 0 {
		return Cond{}, nil
	}
	head, ok := lst[0].(string)
	if !ok {
		// Python falls back to AlwaysTrue when args[0] is None.
		if lst[0] == nil {
			return Cond{}, nil
		}
		return Cond{}, &InvalidConditionError{Op: "UNKNOWN", Args: v, Message: "head is not a string"}
	}
	op := Op(head)
	switch op {
	case OpAlwaysTrue:
		return Cond{}, nil
	case OpAnd, OpOr:
		if len(lst) < 2 {
			return Cond{}, &InvalidConditionError{Op: head, Args: v, Message: "needs at least one child"}
		}
		kids := make([]Cond, 0, len(lst)-1)
		for _, child := range lst[1:] {
			ck, err := FromList(child)
			if err != nil {
				return Cond{}, err
			}
			kids = append(kids, ck)
		}
		return Cond{Op: op, Children: kids}, nil
	case OpNot:
		if len(lst) < 2 {
			return Cond{}, &InvalidConditionError{Op: head, Args: v, Message: "NOT needs one child"}
		}
		ck, err := FromList(lst[1])
		if err != nil {
			return Cond{}, err
		}
		return Cond{Op: op, Children: []Cond{ck}}, nil
	case OpExists:
		if len(lst) < 2 {
			return Cond{}, &InvalidConditionError{Op: head, Args: v, Message: "EXISTS needs a field"}
		}
		f, ok := lst[1].(string)
		if !ok {
			return Cond{}, &InvalidConditionError{Op: head, Args: v, Message: "field must be a string"}
		}
		return Cond{Op: op, Field: f}, nil
	case OpSearch:
		if len(lst) < 2 {
			return Cond{}, &InvalidConditionError{Op: head, Args: v, Message: "SEARCH needs a value"}
		}
		return Cond{Op: op, Value: lst[1]}, nil
	case OpIn:
		if len(lst) < 3 {
			return Cond{}, &InvalidConditionError{Op: head, Args: v, Message: "IN needs value and field"}
		}
		f, ok := lst[2].(string)
		if !ok {
			return Cond{}, &InvalidConditionError{Op: head, Args: v, Message: "field must be a string"}
		}
		return Cond{Op: op, Field: f, Value: lst[1]}, nil
	case OpEq, OpNeq, OpGt, OpGte, OpLt, OpLte, OpMatches, OpContains:
		if len(lst) < 3 {
			return Cond{}, &InvalidConditionError{Op: head, Args: v, Message: "needs field and value"}
		}
		f, ok := lst[1].(string)
		if !ok || f == "" {
			return Cond{}, &InvalidConditionError{Op: head, Args: v, Message: "field is not a valid non-null string"}
		}
		return Cond{Op: op, Field: f, Value: lst[2]}, nil
	}
	return Cond{}, &InvalidConditionError{Op: head, Args: v, Message: "unsupported operator"}
}
