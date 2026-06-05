// web/src/lib/api/tenant-contract.test-types.ts
// Compile-time shape assertions. This file must compile without error after
// codegen; the import below will fail if types.gen.ts is missing the symbols.
import type { components } from "./types.gen";

// If codegen has not been run yet, these type aliases will be `unknown` or
// missing and the assignment below will produce a compile error.
type _TenantSchema = components["schemas"]["Tenant"];
type _LoginRequestSchema = components["schemas"]["LoginRequest"];

// Ensure LoginRequest has the optional org field.
declare const _lr: _LoginRequestSchema;
const _orgField: string | undefined = _lr.org;
void _orgField;

// Ensure Tenant has the expected shape.
declare const _t: _TenantSchema;
const _id: string = _t.id;
const _dn: string = _t.display_name;
const _st: string = _t.status;
void _id;
void _dn;
void _st;
