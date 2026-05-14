package condition

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Match evaluates c against rec without any pre-compilation. For hot-path
// callers, prefer Compile+Compiled.Match.
func Match(rec map[string]any, c Cond) bool {
	cp, err := Compile(c)
	if err != nil {
		return false
	}
	return cp.Match(rec)
}

// Compiled is a pre-evaluated condition tree with cached regexes.
type Compiled struct {
	c     Cond
	regex *regexp.Regexp
	kids  []*Compiled
	// inListVals caches a flattened representation of Value for IN/CONTAINS.
	flattenedValue []any
	// inSubcond, when non-nil, indicates IN's Value is itself a condition.
	inSubcond *Compiled
}

// Compile walks a Cond and pre-compiles every Matches regex.
func Compile(c Cond) (*Compiled, error) {
	out := &Compiled{c: c}
	switch c.Op {
	case OpMatches:
		s, _ := c.Value.(string)
		s = unsugarRegex(s)
		re, err := regexp.Compile("(?i)" + s)
		if err != nil {
			return nil, &InvalidConditionError{Op: string(OpMatches), Args: c, Message: err.Error()}
		}
		out.regex = re
	case OpContains:
		// Value can be a scalar or a list; we flatten lazily on demand.
		out.flattenedValue = Flatten([]any{c.Value})
		// Pre-compile each pattern's regex as well, since CONTAINS uses lazy_search.
	case OpIn:
		// Detect condition mode: Value is itself a list with a leading op.
		if sub, ok := looksLikeListCond(c.Value); ok {
			cc, err := Compile(sub)
			if err != nil {
				return nil, err
			}
			out.inSubcond = cc
		} else {
			out.flattenedValue = Flatten([]any{c.Value})
		}
	}
	for _, k := range c.Children {
		ck, err := Compile(k)
		if err != nil {
			return nil, err
		}
		out.kids = append(out.kids, ck)
	}
	return out, nil
}

// Match evaluates the compiled tree against a record.
func (cp *Compiled) Match(rec map[string]any) bool {
	c := cp.c
	switch c.Op {
	case OpAlwaysTrue:
		return true
	case OpAnd:
		for _, k := range cp.kids {
			if !k.Match(rec) {
				return false
			}
		}
		return true
	case OpOr:
		for _, k := range cp.kids {
			if k.Match(rec) {
				return true
			}
		}
		return false
	case OpNot:
		if len(cp.kids) != 1 {
			return false
		}
		return !cp.kids[0].Match(rec)
	case OpEq:
		got, _ := dotDig(rec, c.Field)
		return equalsValue(got, c.Value)
	case OpNeq:
		got, _ := dotDig(rec, c.Field)
		return !equalsValue(got, c.Value)
	case OpGt:
		got, ok := dotDig(rec, c.Field)
		if !ok {
			return false
		}
		r, ok := compareValues(got, c.Value)
		return ok && r > 0
	case OpGte:
		got, ok := dotDig(rec, c.Field)
		if !ok {
			return false
		}
		r, ok := compareValues(got, c.Value)
		return ok && r >= 0
	case OpLt:
		got, ok := dotDig(rec, c.Field)
		if !ok {
			return false
		}
		r, ok := compareValues(got, c.Value)
		return ok && r < 0
	case OpLte:
		got, ok := dotDig(rec, c.Field)
		if !ok {
			return false
		}
		r, ok := compareValues(got, c.Value)
		return ok && r <= 0
	case OpMatches:
		got, _ := dotDig(rec, c.Field)
		if got == nil {
			return false
		}
		s, ok := toString(got)
		if !ok {
			return false
		}
		if cp.regex == nil {
			return false
		}
		return cp.regex.MatchString(s)
	case OpExists:
		got, _ := dotDig(rec, c.Field)
		return got != nil
	case OpContains:
		got, _ := dotDig(rec, c.Field)
		values := cp.flattenedValue
		records := Flatten([]any{got})
		for _, v := range values {
			for _, r := range records {
				if lazySearch(v, r) {
					return true
				}
			}
		}
		return false
	case OpIn:
		got, _ := dotDig(rec, c.Field)
		if cp.inSubcond != nil {
			items, ok := got.([]any)
			if !ok {
				return false
			}
			for _, item := range items {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if cp.inSubcond.Match(m) {
					return true
				}
			}
			return false
		}
		// List mode: any record element ∈ flattened(value)
		records := Flatten([]any{got})
		for _, r := range records {
			for _, v := range cp.flattenedValue {
				if equalsValue(r, v) {
					return true
				}
			}
		}
		return false
	case OpSearch:
		// Stringify the record and search the literal value.
		needle, ok := toString(c.Value)
		if !ok {
			return false
		}
		return strings.Contains(stringifyRecord(rec), needle)
	}
	return false
}

// equalsValue compares values with Python-like permissiveness.
//   - identical scalar types compare directly
//   - numeric: int vs float coerce
//   - everything else: reflect.DeepEqual
func equalsValue(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	// Fast scalar path
	switch av := a.(type) {
	case string:
		if bv, ok := b.(string); ok {
			return av == bv
		}
		return false
	case bool:
		if bv, ok := b.(bool); ok {
			return av == bv
		}
		return false
	}
	if af, aok := toFloat(a); aok {
		if bf, bok := toFloat(b); bok {
			return af == bf
		}
		return false
	}
	return reflect.DeepEqual(a, b)
}

// compareValues returns -1/0/1 if a<b, a==b, a>b. ok=false when not comparable.
func compareValues(a, b any) (int, bool) {
	if as, ok := a.(string); ok {
		if bs, ok := b.(string); ok {
			switch {
			case as < bs:
				return -1, true
			case as > bs:
				return 1, true
			}
			return 0, true
		}
		return 0, false
	}
	if af, aok := toFloat(a); aok {
		if bf, bok := toFloat(b); bok {
			switch {
			case af < bf:
				return -1, true
			case af > bf:
				return 1, true
			}
			return 0, true
		}
	}
	return 0, false
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

func toString(v any) (string, bool) {
	switch s := v.(type) {
	case string:
		return s, true
	case fmt.Stringer:
		return s.String(), true
	}
	return fmt.Sprint(v), true
}

// lazySearch ports Python's lazy_search: regex-search str(value) in str(record),
// case-insensitive.
func lazySearch(value, record any) bool {
	vstr, _ := toString(value)
	rstr, _ := toString(record)
	re, err := compileLazy(vstr)
	if err != nil {
		return false
	}
	return re.MatchString(rstr)
}

var (
	lazyCacheMu sync.RWMutex
	lazyCache   = map[string]*regexp.Regexp{}
)

func compileLazy(pattern string) (*regexp.Regexp, error) {
	lazyCacheMu.RLock()
	re, ok := lazyCache[pattern]
	lazyCacheMu.RUnlock()
	if ok {
		return re, nil
	}
	re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(pattern))
	if err != nil {
		return nil, err
	}
	lazyCacheMu.Lock()
	lazyCache[pattern] = re
	lazyCacheMu.Unlock()
	return re, nil
}

func unsugarRegex(s string) string {
	if len(s) >= 2 && s[0] == '/' && s[len(s)-1] == '/' {
		return s[1 : len(s)-1]
	}
	return s
}

// looksLikeListCond detects whether v is a legacy-form condition list
// (`["=", "a", 1]` or similar) suitable for the sub-Cond mode of IN.
func looksLikeListCond(v any) (Cond, bool) {
	lst, ok := v.([]any)
	if !ok || len(lst) == 0 {
		return Cond{}, false
	}
	head, ok := lst[0].(string)
	if !ok {
		return Cond{}, false
	}
	if !isKnownOp(head) {
		return Cond{}, false
	}
	c, err := FromList(v)
	if err != nil {
		return Cond{}, false
	}
	return c, true
}

func isKnownOp(s string) bool {
	switch Op(s) {
	case OpAnd, OpOr, OpNot, OpEq, OpNeq, OpGt, OpGte, OpLt, OpLte,
		OpMatches, OpExists, OpContains, OpIn, OpSearch:
		return true
	}
	return false
}

// stringifyRecord mirrors Python's str(record) shape closely enough for SEARCH
// equivalence on common record shapes: a Python-dict-style repr.
func stringifyRecord(rec map[string]any) string {
	var b strings.Builder
	pyRepr(&b, rec)
	return b.String()
}

func pyRepr(b *strings.Builder, v any) {
	switch t := v.(type) {
	case nil:
		b.WriteString("None")
	case string:
		b.WriteString("'")
		for _, r := range t {
			if r == '\'' {
				b.WriteString("\\'")
			} else {
				b.WriteRune(r)
			}
		}
		b.WriteString("'")
	case bool:
		if t {
			b.WriteString("True")
		} else {
			b.WriteString("False")
		}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		fmt.Fprintf(b, "%v", t)
	case []any:
		b.WriteString("[")
		for i, e := range t {
			if i > 0 {
				b.WriteString(", ")
			}
			pyRepr(b, e)
		}
		b.WriteString("]")
	case map[string]any:
		b.WriteString("{")
		// Iterate in insertion-ish order (sorted by key for determinism).
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		// stable order is enough for SEARCH semantics (the original would use
		// dict insertion order, but our test corpus doesn't exercise the
		// pathological cases that depend on it).
		sortStrings(keys)
		for i, k := range keys {
			if i > 0 {
				b.WriteString(", ")
			}
			pyRepr(b, k)
			b.WriteString(": ")
			pyRepr(b, t[k])
		}
		b.WriteString("}")
	default:
		fmt.Fprintf(b, "%v", t)
	}
}

func sortStrings(ss []string) { sort.Strings(ss) }
