package condition

// Flatten recursively flattens nested []any. Strings are leaves (Python's
// `flatten` excludes str from iteration). Maps are leaves too; only slices
// recurse. Mirrors snooze.utils.functions.flatten.
func Flatten(v any) []any {
	var out []any
	flattenInto(&out, v)
	return out
}

func flattenInto(out *[]any, v any) {
	switch t := v.(type) {
	case []any:
		for _, e := range t {
			flattenInto(out, e)
		}
	default:
		*out = append(*out, v)
	}
}
