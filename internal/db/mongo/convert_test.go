package mongo

import (
	"reflect"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/snoozeweb/snooze/internal/condition"
)

// TestConvert covers the operator-translation table that Convert is the only
// owner of. Each case is a small AST fragment plus the bson.M filter we
// expect Convert to produce. The cases mirror the truth table at the top of
// src/snooze/db/mongo/database.py:convert.
func TestConvert(t *testing.T) {
	tests := []struct {
		name    string
		in      condition.Cond
		fields  []string
		want    bson.M
		wantErr bool
	}{
		{
			name: "always_true",
			in:   condition.Cond{},
			want: bson.M{},
		},
		{
			name: "eq",
			in:   condition.Equals("a", 1),
			want: bson.M{"a": 1},
		},
		{
			name: "neq",
			in:   condition.Cond{Op: condition.OpNeq, Field: "a", Value: 1},
			want: bson.M{"a": bson.M{"$ne": 1}},
		},
		{
			name: "gt",
			in:   condition.Cond{Op: condition.OpGt, Field: "a", Value: 1},
			want: bson.M{"a": bson.M{"$gt": 1}},
		},
		{
			name: "matches",
			in:   condition.Cond{Op: condition.OpMatches, Field: "c", Value: "ta*"},
			want: bson.M{"c": bson.M{"$regex": "ta*", "$options": "i"}},
		},
		{
			name: "exists",
			in:   condition.Exists("c"),
			want: bson.M{"c": bson.M{"$exists": true}},
		},
		{
			name: "and",
			in: condition.And(
				condition.Equals("a", 1),
				condition.Cond{Op: condition.OpNeq, Field: "b", Value: 40},
			),
			want: bson.M{"$and": []bson.M{
				{"a": 1},
				{"b": bson.M{"$ne": 40}},
			}},
		},
		{
			name: "or",
			in: condition.Or(
				condition.Equals("a", 1),
				condition.Equals("a", 30),
			),
			want: bson.M{"$or": []bson.M{
				{"a": 1},
				{"a": 30},
			}},
		},
		{
			name: "not",
			in:   condition.Not(condition.Equals("a", 1)),
			want: bson.M{"$nor": []bson.M{{"a": 1}}},
		},
		{
			// Single string value: direct $regex (NOT wrapped in $in — Mongo
			// rejects `$in: [{$regex: …}]` with "cannot nest $ under $in"
			// when the surrounding clause is part of an $and).
			name: "contains_string",
			in:   condition.Cond{Op: condition.OpContains, Field: "a", Value: "1"},
			want: bson.M{"a": bson.M{"$regex": "1", "$options": "i"}},
		},
		{
			// Multiple string values: $or of per-value $regex clauses (still
			// no $regex inside $in).
			name: "contains_list",
			in:   condition.Cond{Op: condition.OpContains, Field: "a", Value: []any{"2", "4"}},
			want: bson.M{"$or": []bson.M{
				{"a": bson.M{"$regex": "2", "$options": "i"}},
				{"a": bson.M{"$regex": "4", "$options": "i"}},
			}},
		},
		{
			// Single non-string value: $in (the cheap path, no regex involved).
			name: "contains_int",
			in:   condition.Cond{Op: condition.OpContains, Field: "a", Value: 9},
			want: bson.M{"a": bson.M{"$in": []any{9}}},
		},
		{
			// Mixed string + non-string: $or of regex clauses + a $in for
			// the residual non-string values.
			name: "contains_mixed",
			in:   condition.Cond{Op: condition.OpContains, Field: "a", Value: []any{"x", 9}},
			want: bson.M{"$or": []bson.M{
				{"a": bson.M{"$regex": "x", "$options": "i"}},
				{"a": bson.M{"$in": []any{9}}},
			}},
		},
		{
			name: "in_list",
			in:   condition.Cond{Op: condition.OpIn, Field: "a", Value: []any{"2", "4"}},
			want: bson.M{"a": bson.M{"$in": []any{"2", "4"}}},
		},
		{
			name: "in_query",
			in: condition.Cond{
				Op:    condition.OpIn,
				Field: "a",
				Value: condition.Equals("y", "1"),
			},
			want: bson.M{"a": bson.M{"$elemMatch": bson.M{"y": "1"}}},
		},
		{
			name:   "search_no_fields",
			in:     condition.Cond{Op: condition.OpSearch, Value: "needle"},
			want:   bson.M{"_id": bson.M{"$exists": false}},
			fields: nil,
		},
		{
			name:   "search_with_fields",
			in:     condition.Cond{Op: condition.OpSearch, Value: "needle"},
			fields: []string{"a", "b"},
			want: bson.M{"$or": []bson.M{
				{"a": bson.M{"$regex": "needle", "$options": "i"}},
				{"b": bson.M{"$regex": "needle", "$options": "i"}},
			}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Convert(tc.in, tc.fields)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Convert err=%v wantErr=%v", err, tc.wantErr)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Convert mismatch\n got: %#v\nwant: %#v", got, tc.want)
			}
		})
	}
}

// TestFlatten covers the nested-list flattening used by CONTAINS.
func TestFlatten(t *testing.T) {
	got := flatten([]any{"a", []any{"b", []any{"c", "d"}}, "e"})
	want := []any{"a", "b", "c", "d", "e"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("flatten: got=%v want=%v", got, want)
	}
}
