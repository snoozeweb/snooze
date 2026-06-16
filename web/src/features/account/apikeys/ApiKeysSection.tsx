// ApiKeysSection — the self-service "API keys" card on the Profile page.
// Lists the caller's own keys (newest first), toggles the inline create form,
// and revokes a key behind an in-DOM confirm (never window.confirm).
import { useState } from "react";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import { useMyApiKeys, useDeleteMyApiKey } from "./api";
import { CreateApiKeyForm } from "./CreateApiKeyForm";

function fmtDate(unix?: number): string {
  if (!unix) return "—";
  return new Date(unix * 1000).toLocaleDateString();
}

export function ApiKeysSection() {
  const list = useMyApiKeys();
  const remove = useDeleteMyApiKey();
  const [creating, setCreating] = useState(false);
  const [confirmId, setConfirmId] = useState<string | null>(null);

  async function revoke(id: string) {
    try {
      await remove.mutateAsync(id);
      toast.success("Key revoked");
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : "Failed to revoke");
    } finally {
      setConfirmId(null);
    }
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "var(--space-3)" }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <h2 style={{ margin: 0 }}>API keys</h2>
        {!creating ? (
          <Button size="sm" variant="primary" leadingIcon="plus" onClick={() => setCreating(true)}>
            New API key
          </Button>
        ) : null}
      </div>

      {creating ? <CreateApiKeyForm onDone={() => setCreating(false)} /> : null}

      {list.data && list.data.length > 0 ? (
        <ul
          style={{
            listStyle: "none",
            margin: 0,
            padding: 0,
            display: "flex",
            flexDirection: "column",
            gap: "var(--space-2)",
          }}
        >
          {list.data.map((k) => (
            <li
              key={k.uid}
              style={{
                display: "flex",
                alignItems: "center",
                gap: "var(--space-3)",
                padding: "var(--space-2)",
                border: "1px solid var(--color-border)",
                borderRadius: "var(--radius-md)",
              }}
            >
              <div style={{ flex: 1 }}>
                <div style={{ fontWeight: 600 }}>{k.name}</div>
                <div style={{ color: "var(--color-text-muted)", fontSize: "var(--font-size-sm)" }}>
                  {k.key_prefix}… · expires {fmtDate(k.expires_at)}
                </div>
                <div
                  style={{
                    display: "flex",
                    gap: "var(--space-1)",
                    flexWrap: "wrap",
                    marginTop: "var(--space-1)",
                  }}
                >
                  {(k.permissions ?? []).map((p) => (
                    <Badge key={p} variant="muted">
                      {p}
                    </Badge>
                  ))}
                </div>
              </div>
              {confirmId === k.uid ? (
                <>
                  <span style={{ color: "var(--color-text-muted)" }}>Revoke?</span>
                  <Button size="sm" variant="danger" onClick={() => void revoke(k.uid)}>
                    Yes
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => setConfirmId(null)}>
                    No
                  </Button>
                </>
              ) : (
                <Button size="sm" variant="ghost" onClick={() => setConfirmId(k.uid)}>
                  Revoke
                </Button>
              )}
            </li>
          ))}
        </ul>
      ) : (
        <p style={{ color: "var(--color-text-muted)" }}>No API keys yet.</p>
      )}
    </div>
  );
}
