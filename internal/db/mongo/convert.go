package mongo

import (
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/japannext/snooze/internal/condition"
	dbpkg "github.com/japannext/snooze/internal/db"
)

// Convert translates a condition.Cond AST into a bson.M MongoDB filter.
//
// SearchFields is used by the SEARCH operator: when non-empty, SEARCH compiles
// to an $or of indexable case-insensitive regex matches over those fields;
// when empty, SEARCH compiles to an "always false" filter (matches nothing)
// rather than the slow $where JavaScript deep-iterate used by Python.
func Convert(c condition.Cond, searchFields []string) (bson.M, error) {
	switch c.Op {
	case condition.OpAlwaysTrue:
		return bson.M{}, nil

	case condition.OpAnd:
		args := make([]bson.M, 0, len(c.Children))
		for _, child := range c.Children {
			q, err := Convert(child, searchFields)
			if err != nil {
				return nil, err
			}
			args = append(args, q)
		}
		return bson.M{"$and": args}, nil

	case condition.OpOr:
		args := make([]bson.M, 0, len(c.Children))
		for _, child := range c.Children {
			q, err := Convert(child, searchFields)
			if err != nil {
				return nil, err
			}
			args = append(args, q)
		}
		return bson.M{"$or": args}, nil

	case condition.OpNot:
		if len(c.Children) == 0 {
			return nil, fmt.Errorf("%w: NOT without child", dbpkg.ErrBadCondition)
		}
		q, err := Convert(c.Children[0], searchFields)
		if err != nil {
			return nil, err
		}
		return bson.M{"$nor": []bson.M{q}}, nil

	case condition.OpEq:
		return bson.M{c.Field: c.Value}, nil
	case condition.OpNeq:
		return bson.M{c.Field: bson.M{"$ne": c.Value}}, nil
	case condition.OpGt:
		return bson.M{c.Field: bson.M{"$gt": c.Value}}, nil
	case condition.OpGte:
		return bson.M{c.Field: bson.M{"$gte": c.Value}}, nil
	case condition.OpLt:
		return bson.M{c.Field: bson.M{"$lt": c.Value}}, nil
	case condition.OpLte:
		return bson.M{c.Field: bson.M{"$lte": c.Value}}, nil

	case condition.OpMatches:
		return bson.M{c.Field: bson.M{"$regex": fmt.Sprintf("%v", c.Value), "$options": "i"}}, nil

	case condition.OpExists:
		return bson.M{c.Field: bson.M{"$exists": true}}, nil

	case condition.OpContains:
		// CONTAINS matches when the field (string or array) contains any of
		// the supplied values. Mongo $in over case-insensitive regex objects
		// covers both forms in a single query.
		values := flatten(c.Value)
		in := make([]any, 0, len(values))
		for _, v := range values {
			if s, ok := v.(string); ok {
				in = append(in, bson.M{"$regex": s, "$options": "i"})
				continue
			}
			in = append(in, v)
		}
		return bson.M{c.Field: bson.M{"$in": in}}, nil

	case condition.OpIn:
		// IN has two modes (matching the Python BackendDB.convert):
		//   - Value is a Cond ⇒ $elemMatch on Field (Field is an array of objects).
		//   - Value is a list  ⇒ $in over Field.
		// Anything else collapses to a single-element $in (single membership test).
		if sub, ok := c.Value.(condition.Cond); ok {
			inner, err := Convert(sub, searchFields)
			if err != nil {
				return nil, err
			}
			return bson.M{c.Field: bson.M{"$elemMatch": inner}}, nil
		}
		if list, ok := asList(c.Value); ok {
			return bson.M{c.Field: bson.M{"$in": list}}, nil
		}
		return bson.M{c.Field: bson.M{"$in": []any{c.Value}}}, nil

	case condition.OpSearch:
		needle := fmt.Sprintf("%v", c.Value)
		if len(searchFields) == 0 {
			// DEPARTURE FROM PYTHON: no $where JS fallback. With no registered
			// search fields, SEARCH matches nothing. Document this in the
			// package doc comment.
			return bson.M{"_id": bson.M{"$exists": false}}, nil
		}
		parts := make([]bson.M, 0, len(searchFields))
		for _, f := range searchFields {
			parts = append(parts, bson.M{f: bson.M{"$regex": needle, "$options": "i"}})
		}
		return bson.M{"$or": parts}, nil
	}
	return nil, fmt.Errorf("%w: unsupported op %q", dbpkg.ErrBadCondition, c.Op)
}

// asList tries to coerce v into a []any.
func asList(v any) ([]any, bool) {
	switch x := v.(type) {
	case []any:
		return x, true
	case []string:
		out := make([]any, len(x))
		for i, s := range x {
			out[i] = s
		}
		return out, true
	case []int:
		out := make([]any, len(x))
		for i, n := range x {
			out[i] = n
		}
		return out, true
	case []int64:
		out := make([]any, len(x))
		for i, n := range x {
			out[i] = n
		}
		return out, true
	case []float64:
		out := make([]any, len(x))
		for i, n := range x {
			out[i] = n
		}
		return out, true
	}
	return nil, false
}

// flatten reproduces Python's flatten() recursively. Strings stay scalar; every
// other iterable (slice/array) is flattened depth-first.
func flatten(v any) []any {
	out := []any{}
	switch x := v.(type) {
	case nil:
		return out
	case []any:
		for _, e := range x {
			out = append(out, flatten(e)...)
		}
		return out
	case []string:
		for _, s := range x {
			out = append(out, s)
		}
		return out
	case []int:
		for _, n := range x {
			out = append(out, n)
		}
		return out
	case []int64:
		for _, n := range x {
			out = append(out, n)
		}
		return out
	case []float64:
		for _, n := range x {
			out = append(out, n)
		}
		return out
	default:
		return []any{v}
	}
}
