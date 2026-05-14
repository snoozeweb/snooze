package condition

import "strconv"

// Dig walks a record by path. Numeric components index into []any; string
// components index into map[string]any. Returns (nil,false) when any step
// misses. Matches Python's snooze.utils.functions.dig.
func Dig(rec any, path ...string) (any, bool) {
	cur := rec
	for _, p := range path {
		if cur == nil {
			return nil, false
		}
		// Numeric: prefer list index, fall back to string key.
		if n, err := strconv.Atoi(p); err == nil {
			switch v := cur.(type) {
			case []any:
				if n < 0 || n >= len(v) {
					return nil, false
				}
				cur = v[n]
				continue
			case map[string]any:
				if next, ok := v[p]; ok {
					cur = next
					continue
				}
				return nil, false
			default:
				return nil, false
			}
		}
		switch v := cur.(type) {
		case map[string]any:
			next, ok := v[p]
			if !ok {
				return nil, false
			}
			cur = next
		default:
			return nil, false
		}
	}
	return cur, true
}

// dotDig is a convenience that splits a dotted path on '.'.
func dotDig(rec any, field string) (any, bool) {
	if field == "" {
		return rec, true
	}
	return Dig(rec, splitDots(field)...)
}

func splitDots(s string) []string {
	if s == "" {
		return nil
	}
	parts := make([]string, 0, 4)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}
