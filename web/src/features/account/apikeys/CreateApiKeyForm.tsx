// CreateApiKeyForm — hand-rolled mint form (the ChangePasswordForm precedent),
// using local useState. The permission picker offers only the caller's own
// permissions (or the full catalogue when the caller holds rw_all); the
// backend is the real subset-of-caller gate. On success the raw key is shown
// exactly once via CopyField with a "won't be shown again" warning.
import { useMemo, useState } from "react";
import { useAuth } from "@/lib/auth/store";
import { usePermissionsCatalogue } from "@/features/admin/roles/api";
import { Button } from "@/shared/ui/Button";
import { Input } from "@/shared/ui/Input";
import { MultiCombobox } from "@/shared/ui/MultiCombobox";
import { DatePicker } from "@/shared/ui/DatePicker";
import { CopyField } from "@/shared/ui/CopyField";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import { useCreateMyApiKey } from "./api";

export function CreateApiKeyForm({ onDone }: { onDone: () => void }) {
  const { claims } = useAuth();
  const ownPerms = useMemo(() => {
    const p = claims?.permissions;
    return Array.isArray(p) ? p : [];
  }, [claims]);
  const isWildcard = ownPerms.includes("rw_all");
  const catalogue = usePermissionsCatalogue();
  const options = useMemo(() => {
    const src = isWildcard ? (catalogue.data ?? []) : ownPerms;
    return src
      .filter((p) => p !== "ro_tenant" && p !== "rw_tenant")
      .map((p) => ({ value: p, label: p }));
  }, [isWildcard, catalogue.data, ownPerms]);

  const [name, setName] = useState("");
  const [perms, setPerms] = useState<string[]>([]);
  const [expires, setExpires] = useState<string | undefined>(undefined);
  const [created, setCreated] = useState<string | null>(null);
  const create = useCreateMyApiKey();

  async function submit() {
    try {
      const res = await create.mutateAsync({
        name: name.trim(),
        permissions: perms,
        ...(expires ? { expires_at: `${expires}T00:00:00Z` } : {}),
      });
      setCreated(res.key);
      toast.success("API key created");
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : "Failed to create key");
    }
  }

  if (created) {
    return (
      <div style={{ display: "flex", flexDirection: "column", gap: "var(--space-3)" }}>
        <p style={{ color: "var(--color-text-muted)" }}>
          Copy this key now — it will not be shown again.
        </p>
        <CopyField value={created} label="New API key" />
        <div>
          <Button variant="primary" onClick={onDone}>
            Done
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: "var(--space-3)" }}>
      <div style={{ display: "flex", flexDirection: "column", gap: "var(--space-1)" }}>
        <label htmlFor="apikey-name">Name</label>
        <Input
          id="apikey-name"
          value={name}
          onChange={(e) => setName(e.currentTarget.value)}
          placeholder="e.g. ci-bot"
        />
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: "var(--space-1)" }}>
        <span>Permissions (subset of your own)</span>
        <MultiCombobox
          aria-label="Permissions"
          options={options}
          value={perms}
          onChange={setPerms}
          allowCustom
          placeholder="Select permissions"
        />
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: "var(--space-1)" }}>
        <span>Expires</span>
        <DatePicker aria-label="Expiry date" value={expires} onChange={setExpires} />
      </div>
      <div style={{ display: "flex", gap: "var(--space-2)" }}>
        <Button
          variant="primary"
          disabled={!name.trim() || create.isPending}
          onClick={() => void submit()}
        >
          Create key
        </Button>
        <Button variant="ghost" onClick={onDone}>
          Cancel
        </Button>
      </div>
    </div>
  );
}
