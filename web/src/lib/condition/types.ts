export type LeafStringOp = "EQUALS" | "NOT_EQUALS" | "CONTAINS" | "MATCHES" | "SEARCH";
/**
 * Equality is the only operator the backend evaluates against a *typed* value:
 * `condition.Cond{Op:"=", Value:true}` on the Go side compares a real JSON
 * boolean, and every driver binds by type (SQLite binds a Go bool to 1, Mongo
 * matches a BSON bool), so `field = "true"` — the string — matches nothing on a
 * boolean column. The wire shape is the same EQUALS/NOT_EQUALS node, only the
 * `value` differs, so booleans get their own member of the union rather than a
 * separate operator.
 */
export type LeafBoolOp = "EQUALS" | "NOT_EQUALS";
export type LeafNumberOp = "LT" | "GT" | "LE" | "GE";
export type LeafArrayOp = "IN";
export type LeafExistsOp = "EXISTS";
export type LeafConstOp = "ALWAYS_TRUE";
export type GroupOp = "AND" | "OR";
export type NotOp = "NOT";

export type Condition =
  | { type: LeafConstOp }
  | { type: LeafStringOp; field: string; value: string }
  | { type: LeafBoolOp; field: string; value: boolean }
  | { type: LeafArrayOp; field: string; value: string[] }
  | { type: LeafNumberOp; field: string; value: number }
  | { type: LeafExistsOp; field: string }
  | { type: NotOp; arg: Condition }
  | { type: GroupOp; args: Condition[] };

export type ConditionType = Condition["type"];

export type ConditionPath = ReadonlyArray<number>;
