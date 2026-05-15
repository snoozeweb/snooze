// Wire shape of an audit-log entry, emitted by internal/plugins/crud.go on
// every successful create/replace/patch/delete and stored in the "audit"
// collection. Field names mirror internal/pluginimpl/audit/plugin.go and the
// cleanup queries in internal/db/{sqlite,postgres,mongo}/cleanup.go which
// index on `object_id`.
export type AuditAction = "create" | "patch" | "replace" | "delete";

export type AuditEntry = {
  uid?: string;
  object_type: string;
  object_id: string;
  action: AuditAction;
  username?: string;
  method?: string;
  summary?: string;
  date_epoch?: number;
};
