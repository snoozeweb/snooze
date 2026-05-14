import type { ReactNode } from "react";
import { useAuth } from "@/lib/auth/store";
import { hasAllPermissions, hasAnyPermission } from "@/lib/auth/permissions";

export type RequirePermProps = {
  any?: readonly string[];
  all?: readonly string[];
  fallback?: ReactNode;
  children: ReactNode;
};

export function RequirePerm({
  any: anyPerms,
  all: allPerms,
  fallback = null,
  children,
}: RequirePermProps) {
  const { claims } = useAuth();
  const passAny = anyPerms ? hasAnyPermission(claims, anyPerms) : true;
  const passAll = allPerms ? hasAllPermissions(claims, allPerms) : true;
  const allowed = passAny && passAll;
  return allowed ? <>{children}</> : <>{fallback}</>;
}
