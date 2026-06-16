import { EditorDrawer } from "@/shared/forms/EditorDrawer";
import { Input } from "@/shared/ui/Input";
import { DatePicker } from "@/shared/ui/DatePicker";
import { ApiKeys } from "./api";
import type { ApiKey } from "./types";

// The admin editor only ever runs in edit mode: keys are minted via the
// self-service routes (subset-of-caller enforced there), never through the
// generic CRUD route. formToBody therefore emits ONLY name + expires_at, so
// the protected fields the backend GuardWrite rejects are never sent.
type FormShape = { name: string; expires: string | undefined };

export function ApiKeyAdminEditor({
  uid,
  onClose,
}: {
  uid: string | undefined;
  onClose: () => void;
}) {
  const get = ApiKeys.useGet(uid);
  // useCreate is required by the EditorDrawer contract but never invoked here —
  // the page never opens the editor in create mode (no "New" button).
  const create = ApiKeys.useCreate();
  const update = ApiKeys.useUpdate();
  return (
    <EditorDrawer<FormShape, ApiKey>
      uid={uid}
      onClose={onClose}
      get={get}
      create={create}
      update={update}
      emptyForm={{ name: "", expires: undefined }}
      recordToForm={(k) => ({
        name: k.name ?? "",
        expires: k.expires_at
          ? new Date(k.expires_at * 1000).toISOString().slice(0, 10)
          : undefined,
      })}
      formToBody={(form) =>
        ({
          name: form.name,
          ...(form.expires
            ? { expires_at: Math.floor(new Date(`${form.expires}T00:00:00Z`).getTime() / 1000) }
            : {}),
        }) as Partial<ApiKey>
      }
      title="Edit API key"
      successMessage={{ create: "Saved", update: "API key saved" }}
      formId="apikey-form"
    >
      {({ register, setValue, watch }) => {
        const expires = watch("expires");
        return (
          <>
            <div style={{ display: "flex", flexDirection: "column", gap: "var(--space-1)" }}>
              <label htmlFor="apikey-name">Name</label>
              <Input id="apikey-name" {...register("name")} />
            </div>
            <div style={{ display: "flex", flexDirection: "column", gap: "var(--space-1)" }}>
              <span>Expires</span>
              <DatePicker
                aria-label="Expiry date"
                value={expires}
                onChange={(v) => setValue("expires", v, { shouldDirty: true })}
              />
            </div>
          </>
        );
      }}
    </EditorDrawer>
  );
}
