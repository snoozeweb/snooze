// Who may write an alert's agentic analysis.
//
// Reading needs nothing beyond the record read permission the Alerts page
// already required, so there is no read gate here: whoever sees the alert sees
// its analysis.
//
// Writing is the unusual one. `agentic` is a protected field
// (internal/protected): PUT/DELETE /api/v1/record/{uid}/agentic demand the
// LITERAL `rw_protected` permission, and the server deliberately does not let
// the `rw_all` admin wildcard stand in for it — if it did, every role that
// already holds `rw_all` would silently gain the right to rewrite
// agent-authored analysis. See the endpoint description in api/openapi.yaml.
import { useAuth } from "@/lib/auth/store";

/**
 * The one permission that grants write access, spelled the way the server
 * spells it (internal/auth PermWriteProtected).
 */
export const ANALYSIS_WRITE_PERM = "rw_protected";

/**
 * useCanWriteAnalysis gates Edit / Clear / "Write analysis".
 *
 * It deliberately does NOT go through `hasAnyPermission`, which honours the
 * `rw_all` wildcard: the server's check on this route is literal membership,
 * so a wildcard-only admin who saw an enabled Save button would fill the form
 * and then eat a 403. A `rw_all` holder gets the read-only view, exactly like
 * the server treats them.
 */
export function useCanWriteAnalysis(): boolean {
  const { claims } = useAuth();
  const perms = claims?.permissions;
  return Array.isArray(perms) && perms.includes(ANALYSIS_WRITE_PERM);
}
