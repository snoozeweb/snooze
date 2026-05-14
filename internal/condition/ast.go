// Package condition implements the Snooze condition DSL: AST, parser, evaluator,
// and JSON marshaling. Each DB driver translates the AST to its native query
// language.
package condition

// Op is the discriminator of a Cond node.
//
// Operator semantics mirror the Python implementation:
//
//	""           AlwaysTrue (matches everything; default Cond zero value)
//	AND OR NOT   Boolean logic
//	= !=         Equality (deep, type-aware)
//	> >= < <=    Numeric / lexicographic comparison
//	MATCHES      Case-insensitive regex match
//	EXISTS       Field is present (and not nil)
//	CONTAINS     Substring or regex inside a string or any element of a list
//	IN           Membership; Value may be a list literal or a sub-Cond (matches
//	             when *any* element of an array field satisfies the sub-Cond)
//	SEARCH       Full-text search across the record (driver-specific)
type Op string

const (
	OpAlwaysTrue Op = ""
	OpAnd        Op = "AND"
	OpOr         Op = "OR"
	OpNot        Op = "NOT"
	OpEq         Op = "="
	OpNeq        Op = "!="
	OpGt         Op = ">"
	OpGte        Op = ">="
	OpLt         Op = "<"
	OpLte        Op = "<="
	OpMatches    Op = "MATCHES"
	OpExists     Op = "EXISTS"
	OpContains   Op = "CONTAINS"
	OpIn         Op = "IN"
	OpSearch     Op = "SEARCH"
)

// Cond is a single node of the condition AST. The Op field discriminates which
// of the other fields are meaningful:
//
//   - AND/OR: Children only.
//   - NOT: Children[0] only.
//   - Binary ops (=, !=, >, …, MATCHES, CONTAINS, IN): Field and Value.
//   - EXISTS: Field only.
//   - SEARCH: Value only (a string).
//   - AlwaysTrue: zero value.
type Cond struct {
	Op       Op     `json:"op"`
	Field    string `json:"field,omitempty"`
	Value    any    `json:"value,omitempty"`
	Children []Cond `json:"children,omitempty"`
}

// IsZero reports whether c is the empty AlwaysTrue node.
func (c Cond) IsZero() bool { return c.Op == OpAlwaysTrue && c.Field == "" && c.Value == nil && len(c.Children) == 0 }

// And builds a conjunction. Single-child input is returned unwrapped.
func And(children ...Cond) Cond {
	if len(children) == 1 {
		return children[0]
	}
	return Cond{Op: OpAnd, Children: children}
}

// Or builds a disjunction.
func Or(children ...Cond) Cond {
	if len(children) == 1 {
		return children[0]
	}
	return Cond{Op: OpOr, Children: children}
}

// Not negates a Cond.
func Not(c Cond) Cond { return Cond{Op: OpNot, Children: []Cond{c}} }

// Equals builds a `field = value` condition.
func Equals(field string, value any) Cond { return Cond{Op: OpEq, Field: field, Value: value} }

// Exists builds a `field?` condition.
func Exists(field string) Cond { return Cond{Op: OpExists, Field: field} }
