package mongo

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/snoozeweb/snooze/internal/condition"
	dbpkg "github.com/snoozeweb/snooze/internal/db"
)

// Convert translates a condition.Cond AST into a bson.M MongoDB filter.
//
// ctx and collection drive tenant injection (D1): for a tenant-scoped
// collection with a tenant in ctx the result is wrapped in an $and with a
// tenant_id predicate; platform scope and global collections skip injection;
// a naked context on a scoped collection fails closed with ErrNoTenant.
//
// SearchFields is used by the SEARCH operator: when non-empty, SEARCH compiles
// to an $or of indexable case-insensitive regex matches over those fields;
// when empty, SEARCH compiles to an "always false" filter (matches nothing)
// rather than the slow $where JavaScript deep-iterate used by Python.
func Convert(ctx context.Context, collection string, c condition.Cond, searchFields []string) (bson.M, error) {
	tenantID, inject, err := dbpkg.TenantScope(ctx, collection)
	if err != nil {
		return nil, fmt.Errorf("mongo: %w", err)
	}
	result, err := convertCond(c, searchFields)
	if err != nil {
		return nil, err
	}
	if inject {
		return bson.M{"$and": []bson.M{{"tenant_id": bson.M{"$eq": tenantID}}, result}}, nil
	}
	return result, nil
}

// convertCond is the internal recursive translator (was Convert before tenant
// injection). It performs no tenant injection.
func convertCond(c condition.Cond, searchFields []string) (bson.M, error) {
	switch c.Op {
	case condition.OpAlwaysTrue:
		return bson.M{}, nil

	case condition.OpAnd:
		args := make([]bson.M, 0, len(c.Children))
		for _, child := range c.Children {
			q, err := convertCond(child, searchFields)
			if err != nil {
				return nil, err
			}
			args = append(args, q)
		}
		return bson.M{"$and": args}, nil

	case condition.OpOr:
		args := make([]bson.M, 0, len(c.Children))
		for _, child := range c.Children {
			q, err := convertCond(child, searchFields)
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
		q, err := convertCond(c.Children[0], searchFields)
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
		// CONTAINS matches when the field (string or array of strings) contains
		// any of the supplied values via case-insensitive regex. Mongo refuses
		// `$in: [{$regex: …}]` ("cannot nest $ under $in") so we route string
		// values through a direct `$regex` clause (single value) or a `$or` of
		// per-value regex clauses (multiple values). Non-string values keep
		// the cheap `$in` form, which also works on array fields.
		values := flatten(c.Value)
		var strs []string
		var rest []any
		for _, v := range values {
			if s, ok := v.(string); ok {
				strs = append(strs, s)
				continue
			}
			rest = append(rest, v)
		}
		switch {
		case len(strs) == 0 && len(rest) == 0:
			return bson.M{c.Field: bson.M{"$in": []any{}}}, nil
		case len(strs) == 1 && len(rest) == 0:
			return bson.M{c.Field: bson.M{"$regex": strs[0], "$options": "i"}}, nil
		case len(strs) > 1 && len(rest) == 0:
			or := make([]bson.M, 0, len(strs))
			for _, s := range strs {
				or = append(or, bson.M{c.Field: bson.M{"$regex": s, "$options": "i"}})
			}
			return bson.M{"$or": or}, nil
		case len(strs) == 0:
			return bson.M{c.Field: bson.M{"$in": rest}}, nil
		default:
			// Mixed values: union the regex-$or with a non-string $in.
			or := make([]bson.M, 0, len(strs)+1)
			for _, s := range strs {
				or = append(or, bson.M{c.Field: bson.M{"$regex": s, "$options": "i"}})
			}
			or = append(or, bson.M{c.Field: bson.M{"$in": rest}})
			return bson.M{"$or": or}, nil
		}

	case condition.OpIn:
		// IN has two modes (matching the Python BackendDB.convert):
		//   - Value is a Cond ⇒ $elemMatch on Field (Field is an array of objects).
		//   - Value is a list  ⇒ $in over Field.
		// Anything else collapses to a single-element $in (single membership test).
		if sub, ok := c.Value.(condition.Cond); ok {
			inner, err := convertCond(sub, searchFields)
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
