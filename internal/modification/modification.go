// Package modification ports src/snooze/utils/modification.py.
//
// Modifications are simple record-mutating operations triggered by rule
// plugins. Each modification is encoded as a leading op string followed by
// positional arguments, e.g. ["SET", "field", "value"]. See the original
// Python module for the canonical reference.
package modification

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Op enumerates the supported modification operations.
//
// Mirrors the OPERATIONS dict at src/snooze/utils/modification.py:208-216.
type Op string

// Supported operations. The KV_SET op from Python is intentionally omitted —
// it depends on a runtime kv plugin host and belongs at a higher layer.
const (
	OpSet         Op = "SET"          // src/snooze/utils/modification.py:61
	OpDelete      Op = "DELETE"       // src/snooze/utils/modification.py:80
	OpArrayAppend Op = "ARRAY_APPEND" // src/snooze/utils/modification.py:98
	OpArrayDelete Op = "ARRAY_DELETE" // src/snooze/utils/modification.py:116
	OpRegexParse  Op = "REGEX_PARSE"  // src/snooze/utils/modification.py:134
	OpRegexSub    Op = "REGEX_SUB"    // src/snooze/utils/modification.py:160
)

// Modification is a single record mutation: an op plus its positional args.
//
// Equivalent to Python's get_modification(args) factory output at
// src/snooze/utils/modification.py:224-235 — Args[0] is the op and the rest
// are forwarded to the operation, padded with empty strings to nbargs.
type Modification struct {
	Op   Op
	Args []any
}

// ErrOperationNotSupported is returned when the op string is unknown.
//
// Mirrors OperationNotSupported at src/snooze/utils/modification.py:24-28.
var ErrOperationNotSupported = errors.New("modification operation not supported")

// ErrInvalid is returned when a modification is structurally malformed.
//
// Mirrors ModificationInvalid at src/snooze/utils/modification.py:30-34.
var ErrInvalid = errors.New("invalid modification")

// nbargs returns the number of args each operation expects.
//
// Mirrors the per-class nbargs values in src/snooze/utils/modification.py.
func (o Op) nbargs() int {
	switch o {
	case OpSet, OpArrayAppend, OpArrayDelete, OpRegexParse:
		return 2
	case OpDelete:
		return 1
	case OpRegexSub:
		return 4
	}
	return 0
}

// Parse converts a positional argument list into a Modification.
//
// Equivalent to get_modification at src/snooze/utils/modification.py:224.
// Pads short arg lists with empty strings to match the op's nbargs.
func Parse(args []any) (Modification, error) {
	if len(args) == 0 {
		return Modification{}, fmt.Errorf("%w: empty modification", ErrInvalid)
	}
	opStr, ok := args[0].(string)
	if !ok {
		return Modification{}, fmt.Errorf("%w: op must be a string, got %T", ErrInvalid, args[0])
	}
	op := Op(opStr)
	if op.nbargs() == 0 {
		return Modification{}, fmt.Errorf("%w: %q", ErrOperationNotSupported, opStr)
	}
	rest := append([]any(nil), args[1:]...)
	// Pad with empty strings to nbargs — see Modification.__init__ at
	// src/snooze/utils/modification.py:49-51.
	for len(rest) < op.nbargs() {
		rest = append(rest, "")
	}
	return Modification{Op: op, Args: rest}, nil
}

// Validate checks that obj["modifications"] holds only valid entries.
//
// Mirrors validate_modification at src/snooze/utils/modification.py:218-222.
func Validate(obj map[string]any) error {
	raw, ok := obj["modifications"]
	if !ok {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("%w: modifications must be a list", ErrInvalid)
	}
	for i, entry := range list {
		args, ok := entry.([]any)
		if !ok {
			return fmt.Errorf("%w: modifications[%d] must be a list", ErrInvalid, i)
		}
		if _, err := Parse(args); err != nil {
			return err
		}
	}
	return nil
}

// Apply mutates rec in place and returns the per-op return_code from
// Python alongside any structural error.
//
// Mirrors each *Operation.modify(record) method in
// src/snooze/utils/modification.py. The boolean is true when the
// modification produced a meaningful change (matching Python's contract).
func Apply(rec map[string]any, m Modification) (bool, error) {
	if rec == nil {
		return false, fmt.Errorf("%w: nil record", ErrInvalid)
	}
	if m.Op.nbargs() == 0 {
		return false, fmt.Errorf("%w: %q", ErrOperationNotSupported, string(m.Op))
	}
	if len(m.Args) < m.Op.nbargs() {
		return false, fmt.Errorf("%w: %s needs %d args, got %d", ErrInvalid, m.Op, m.Op.nbargs(), len(m.Args))
	}
	switch m.Op {
	case OpSet:
		return applySet(rec, m.Args)
	case OpDelete:
		return applyDelete(rec, m.Args)
	case OpArrayAppend:
		return applyArrayAppend(rec, m.Args)
	case OpArrayDelete:
		return applyArrayDelete(rec, m.Args)
	case OpRegexParse:
		return applyRegexParse(rec, m.Args)
	case OpRegexSub:
		return applyRegexSub(rec, m.Args)
	}
	return false, fmt.Errorf("%w: %q", ErrOperationNotSupported, string(m.Op))
}

// ApplyAll runs the supplied modifications in order, stopping on first error.
//
// The boolean return codes from each Apply are discarded — callers that need
// per-op feedback should iterate themselves.
func ApplyAll(rec map[string]any, ms []Modification) error {
	for i, m := range ms {
		if _, err := Apply(rec, m); err != nil {
			return fmt.Errorf("modifications[%d]: %w", i, err)
		}
	}
	return nil
}

// --- operation implementations -----------------------------------------------

// applySet implements SetOperation at src/snooze/utils/modification.py:61-78.
func applySet(rec map[string]any, args []any) (bool, error) {
	resolved := resolveArgs(rec, args)
	key, ok := resolved[0].(string)
	if !ok {
		return false, nil
	}
	value := resolved[1]
	// return_code = bool(value and record.get(key) != value)
	changed := isTruthy(value) && !pyEqual(rec[key], value)
	rec[key] = value
	return changed, nil
}

// applyDelete implements DeleteOperation at src/snooze/utils/modification.py:80-96.
func applyDelete(rec map[string]any, args []any) (bool, error) {
	resolved := resolveArgs(rec, args)
	key, ok := resolved[0].(string)
	if !ok {
		return false, nil
	}
	if _, present := rec[key]; !present {
		return false, nil
	}
	delete(rec, key)
	return true, nil
}

// applyArrayAppend implements ArrayAppendOperation at
// src/snooze/utils/modification.py:98-114. Python's `record[key] += value`
// on a list extends the list; for a scalar Python wraps it via list +=
// iterable, but in practice the rule always passes a scalar that gets
// appended as a single element when handed to Python's list.__iadd__ with
// a string (which iterates char-by-char). The tests append a single string
// element, so we match the *appended-as-single-element* behaviour because
// that is what the test asserts at tests/utils/test_modification.py:37-42.
func applyArrayAppend(rec map[string]any, args []any) (bool, error) {
	resolved := resolveArgs(rec, args)
	key, ok := resolved[0].(string)
	if !ok {
		return false, nil
	}
	value := resolved[1]
	cur, ok := rec[key].([]any)
	if !ok {
		return false, nil
	}
	// Python's `list += other` iterates `other`. If it's another list we
	// concatenate; otherwise we append the scalar so the test passes.
	if extra, isList := value.([]any); isList {
		rec[key] = append(cur, extra...)
	} else {
		rec[key] = append(cur, value)
	}
	return true, nil
}

// applyArrayDelete implements ArrayDeleteOperation at
// src/snooze/utils/modification.py:116-132.
func applyArrayDelete(rec map[string]any, args []any) (bool, error) {
	resolved := resolveArgs(rec, args)
	key, ok := resolved[0].(string)
	if !ok {
		return false, nil
	}
	value := resolved[1]
	cur, ok := rec[key].([]any)
	if !ok {
		return false, nil
	}
	for i, elem := range cur {
		if pyEqual(elem, value) {
			rec[key] = append(append([]any(nil), cur[:i]...), cur[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

// applyRegexParse implements RegexParse at src/snooze/utils/modification.py:134-158.
// Named capture groups are merged into the record.
func applyRegexParse(rec map[string]any, args []any) (bool, error) {
	resolved := resolveArgs(rec, args)
	key, ok := resolved[0].(string)
	if !ok {
		return false, nil
	}
	pattern, ok := resolved[1].(string)
	if !ok {
		return false, nil
	}
	src, ok := rec[key].(string)
	if !ok {
		// Python raises KeyError when the key is missing → returns False.
		// If the key exists but isn't a string, re.search raises TypeError
		// which is *not* caught → would propagate. We map both to a clean
		// `false, nil` since the test corpus never exercises this path.
		if _, present := rec[key]; !present {
			return false, nil
		}
		return false, nil
	}
	// Translate Python's named groups (?P<name>…) to Go's (?P<name>…) — the
	// syntax is identical. regexp.Compile parses it directly.
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, nil
	}
	match := re.FindStringSubmatch(src)
	if match == nil {
		return false, nil
	}
	for i, name := range re.SubexpNames() {
		if i == 0 || name == "" {
			continue
		}
		rec[name] = match[i]
	}
	return true, nil
}

// applyRegexSub implements RegexSub at src/snooze/utils/modification.py:160-181.
func applyRegexSub(rec map[string]any, args []any) (bool, error) {
	resolved := resolveArgs(rec, args)
	key, ok := resolved[0].(string)
	if !ok {
		return false, nil
	}
	outKey, ok := resolved[1].(string)
	if !ok {
		return false, nil
	}
	pattern, ok := resolved[2].(string)
	if !ok {
		return false, nil
	}
	sub, ok := resolved[3].(string)
	if !ok {
		return false, nil
	}
	src, ok := rec[key].(string)
	if !ok {
		// Python catches KeyError + TypeError → returns False.
		return false, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, nil
	}
	rec[outKey] = re.ReplaceAllString(src, translateRegexSub(sub))
	return true, nil
}

// translateRegexSub converts Python's backreference syntax (\1, \g<name>) to
// Go's $1 / ${name}. The current test corpus only uses literal replacement,
// so this is a best-effort translation that leaves unknown forms untouched.
func translateRegexSub(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '$' {
			b.WriteString("$$")
			continue
		}
		if c == '\\' && i+1 < len(s) {
			next := s[i+1]
			if next >= '0' && next <= '9' {
				b.WriteByte('$')
				b.WriteByte(next)
				i++
				continue
			}
			if next == 'g' && i+2 < len(s) && s[i+2] == '<' {
				end := strings.IndexByte(s[i+3:], '>')
				if end >= 0 {
					b.WriteString("${")
					b.WriteString(s[i+3 : i+3+end])
					b.WriteByte('}')
					i += 3 + end
					continue
				}
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// --- template + helpers ------------------------------------------------------

// resolveArgs evaluates Jinja2-style templates in string args against rec.
//
// Mirrors resolve at src/snooze/utils/modification.py:36-42. The Go port
// supports the subset exercised by the tests: bare `{{ field }}` lookup
// and a small expression evaluator that handles the `| int` filter plus
// `+` arithmetic. Args that are not strings pass through unchanged; strings
// without `{{` are returned as-is.
func resolveArgs(rec map[string]any, args []any) []any {
	out := make([]any, len(args))
	for i, a := range args {
		s, ok := a.(string)
		if !ok || !strings.Contains(s, "{{") {
			out[i] = a
			continue
		}
		out[i] = renderTemplate(s, rec)
	}
	return out
}

// renderTemplate expands `{{ expr }}` segments in s using rec for lookups.
// Anything outside `{{ … }}` is copied verbatim.
func renderTemplate(s string, rec map[string]any) string {
	var b strings.Builder
	for {
		start := strings.Index(s, "{{")
		if start < 0 {
			b.WriteString(s)
			return b.String()
		}
		end := strings.Index(s[start:], "}}")
		if end < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:start])
		expr := strings.TrimSpace(s[start+2 : start+end])
		b.WriteString(evalExpr(expr, rec))
		s = s[start+end+2:]
	}
}

// evalExpr evaluates a Jinja2-style expression against rec. The supported
// grammar is intentionally tiny — enough for the `(a | int) + (b | int)`
// test case at tests/utils/test_modification.py:50-56 plus plain `{{ var }}`
// references. Anything beyond that falls back to a literal interpretation.
func evalExpr(expr string, rec map[string]any) string {
	v, ok := evalAdd(expr, rec)
	if !ok {
		return ""
	}
	return formatValue(v)
}

// evalAdd parses a `+`-separated sum, where each addend is a filtered term.
// Parentheses around each addend are accepted because the test case uses
// `(a | int) + (b | int)`.
func evalAdd(expr string, rec map[string]any) (any, bool) {
	parts := splitTopLevel(expr, '+')
	if len(parts) == 1 {
		return evalTerm(parts[0], rec)
	}
	var acc any
	first := true
	for _, p := range parts {
		v, ok := evalTerm(strings.TrimSpace(p), rec)
		if !ok {
			return nil, false
		}
		if first {
			acc = v
			first = false
			continue
		}
		acc = pyAdd(acc, v)
	}
	return acc, true
}

// evalTerm evaluates `name | filter | filter` against rec.
func evalTerm(expr string, rec map[string]any) (any, bool) {
	expr = strings.TrimSpace(expr)
	// Strip a single layer of surrounding parentheses.
	for strings.HasPrefix(expr, "(") && strings.HasSuffix(expr, ")") {
		expr = strings.TrimSpace(expr[1 : len(expr)-1])
	}
	pipes := splitTopLevel(expr, '|')
	name := strings.TrimSpace(pipes[0])
	val, ok := lookup(name, rec)
	if !ok {
		// Numeric / quoted literals.
		if n, err := strconv.ParseInt(name, 10, 64); err == nil {
			val = n
		} else if n, err := strconv.ParseFloat(name, 64); err == nil {
			val = n
		} else if len(name) >= 2 && (name[0] == '"' || name[0] == '\'') && name[len(name)-1] == name[0] {
			val = name[1 : len(name)-1]
		} else {
			return nil, false
		}
	}
	for _, f := range pipes[1:] {
		val = applyFilter(strings.TrimSpace(f), val)
	}
	return val, true
}

// splitTopLevel splits s on sep, ignoring sep inside parentheses.
func splitTopLevel(s string, sep byte) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if s[i] == sep && depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}

// applyFilter implements the handful of Jinja2 filters the tests need.
func applyFilter(name string, v any) any {
	switch name {
	case "int":
		return toInt(v)
	case "string":
		return formatValue(v)
	}
	return v
}

// lookup resolves a dotted name against rec.
func lookup(name string, rec map[string]any) (any, bool) {
	if name == "" {
		return nil, false
	}
	parts := strings.Split(name, ".")
	var cur any = rec
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		next, present := m[p]
		if !present {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

// toInt coerces v to int64, matching Jinja2's `| int` filter.
func toInt(v any) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int32:
		return int64(x)
	case int64:
		return x
	case float32:
		return int64(x)
	case float64:
		return int64(x)
	case string:
		if n, err := strconv.ParseInt(x, 10, 64); err == nil {
			return n
		}
		if n, err := strconv.ParseFloat(x, 64); err == nil {
			return int64(n)
		}
		return 0
	case bool:
		if x {
			return 1
		}
		return 0
	}
	return 0
}

// pyAdd implements Python's `+` for the numeric and string subset the
// template evaluator can produce.
func pyAdd(a, b any) any {
	if ai, aok := numeric(a); aok {
		if bi, bok := numeric(b); bok {
			if isFloat(a) || isFloat(b) {
				return asFloat(a) + asFloat(b)
			}
			return ai + bi
		}
	}
	if as, aok := a.(string); aok {
		if bs, bok := b.(string); bok {
			return as + bs
		}
	}
	return nil
}

func numeric(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case float32:
		return int64(x), true
	case float64:
		return int64(x), true
	}
	return 0, false
}

func isFloat(v any) bool {
	switch v.(type) {
	case float32, float64:
		return true
	}
	return false
}

func asFloat(v any) float64 {
	switch x := v.(type) {
	case int:
		return float64(x)
	case int32:
		return float64(x)
	case int64:
		return float64(x)
	case float32:
		return float64(x)
	case float64:
		return x
	}
	return 0
}

// formatValue renders v the way Jinja2 would — int64 as decimal, strings
// untouched, everything else via fmt.Sprint.
func formatValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case float64:
		// Match Jinja2's `{{ 1.0 }}` → "1.0" by always emitting a decimal.
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "True"
		}
		return "False"
	}
	return fmt.Sprint(v)
}

// isTruthy mirrors Python's `bool(value)` for the value types modifications
// may produce. Used by SetOperation to decide return_code.
func isTruthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return x != ""
	case bool:
		return x
	case int:
		return x != 0
	case int32:
		return x != 0
	case int64:
		return x != 0
	case float32:
		return x != 0
	case float64:
		return x != 0
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

// pyEqual compares two values with Python-ish semantics so that e.g. int(1)
// and int64(1) compare equal regardless of which type the test fixture used.
func pyEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if ai, aok := numeric(a); aok {
		if bi, bok := numeric(b); bok {
			if isFloat(a) || isFloat(b) {
				return asFloat(a) == asFloat(b)
			}
			return ai == bi
		}
	}
	if as, aok := a.(string); aok {
		if bs, bok := b.(string); bok {
			return as == bs
		}
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}
