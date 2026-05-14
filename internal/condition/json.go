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
		// Plain object form — decode into an alias to avoid recursion.
		type alias Cond
		var a alias
		if err := json.Unmarshal(trimmed, &a); err != nil {
			return fmt.Errorf("condition: %w", err)
		}
		*c = Cond(a)
		return nil
	}
	return fmt.Errorf("condition: unsupported JSON shape: %s", string(trimmed))
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
