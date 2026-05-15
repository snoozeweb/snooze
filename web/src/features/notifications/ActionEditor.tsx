import { useEffect, useMemo, useState } from "react";
import { useForm } from "react-hook-form";
import { Button } from "@/shared/ui/Button";
import { Drawer, DrawerBody, DrawerContent, DrawerFooter, DrawerTitle } from "@/shared/ui/Drawer";
import { Input } from "@/shared/ui/Input";
import { Spinner } from "@/shared/ui/Spinner";
import { Textarea } from "@/shared/ui/Textarea";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import { MetadataForm } from "@/shared/forms/MetadataForm";
import { useAllMetadata } from "@/shared/forms/useMetadata";
import type { Metadata } from "@/shared/forms/types";
import { Actions } from "./api";
import type { Action } from "./types";
import styles from "./NotificationEditor.module.css";

type FormShape = {
  name: string;
  comment: string;
  action_type: string;
  action_json: string;
};

const EMPTY_FORM: FormShape = {
  name: "",
  comment: "",
  action_type: "",
  action_json: "{}",
};

export type ActionEditorProps = {
  uid: string | undefined;
  onClose: () => void;
};

function plugins_with_form(list: Metadata[] | undefined): Metadata[] {
  if (!list) return [];
  return list.filter(
    (m) => m.action_form && Object.keys(m.action_form).length > 0,
  );
}

export function ActionEditor({ uid, onClose }: ActionEditorProps) {
  const isCreate = uid === undefined || uid === "";
  const existing = Actions.useGet(isCreate ? undefined : uid);
  const create = Actions.useCreate();
  const update = Actions.useUpdate();
  const metadata = useAllMetadata();

  const { register, handleSubmit, reset, formState, watch, setValue } = useForm<FormShape>({
    defaultValues: EMPTY_FORM,
  });

  // Subcontent for the typed form is kept outside react-hook-form because it's
  // a free-form object the MetadataForm renderer owns end-to-end.
  const [action, setAction] = useState<Record<string, unknown>>({});

  useEffect(() => {
    if (isCreate) {
      reset(EMPTY_FORM);
      setAction({});
      return;
    }
    if (existing.data) {
      reset({
        name: existing.data.name ?? "",
        comment: existing.data.comment ?? "",
        action_type: existing.data.action_type ?? "",
        action_json: JSON.stringify(existing.data.action ?? {}, null, 2),
      });
      setAction(existing.data.action ?? {});
    }
  }, [existing.data, isCreate, reset]);

  const [submitting, setSubmitting] = useState(false);
  const [jsonError, setJsonError] = useState<string | null>(null);

  const formPlugins = useMemo(() => plugins_with_form(metadata.data), [metadata.data]);
  const action_type = watch("action_type");
  // Match against plugin_name (the registry key) rather than name. Most
  // action plugins' YAML `name:` is a human label ("Send email", "Run a
  // script") that wouldn't match the Action's `action_type` ("mail",
  // "script"). plugin_name is stamped by the metadata handler.
  const selectedPlugin = useMemo(
    () => formPlugins.find((m) => m.plugin_name === action_type),
    [formPlugins, action_type],
  );
  // If we have an action_type but no matching plugin in metadata, fall back
  // to the JSON textarea so legacy/unknown configs remain editable.
  const useJsonFallback =
    !!action_type && !selectedPlugin && !metadata.isPending;

  async function onSubmit(form: FormShape) {
    setSubmitting(true);
    setJsonError(null);
    try {
      let payload: Record<string, unknown>;
      if (selectedPlugin) {
        payload = action;
      } else {
        try {
          payload = JSON.parse(form.action_json) as Record<string, unknown>;
          if (typeof payload !== "object" || payload === null || Array.isArray(payload)) {
            throw new Error("expected a JSON object");
          }
        } catch (e) {
          setJsonError(e instanceof Error ? e.message : "Invalid JSON");
          setSubmitting(false);
          return;
        }
      }
      const body: Action = {
        name: form.name,
        ...(form.comment ? { comment: form.comment } : {}),
        action_type: form.action_type,
        action: payload,
      };
      if (isCreate) {
        await create.mutateAsync(body);
        toast.success("Action created");
      } else {
        await update.mutateAsync({ uid, body });
        toast.success("Action saved");
      }
      onClose();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : "Save failed");
    } finally {
      setSubmitting(false);
    }
  }

  const nameInvalid = formState.isSubmitted && !watch("name").trim();

  return (
    <Drawer
      open
      onOpenChange={(o) => {
        if (!o) onClose();
      }}
    >
      <DrawerContent>
        <DrawerTitle>{isCreate ? "New action" : "Edit action"}</DrawerTitle>
        <DrawerBody>
          {!isCreate && existing.isPending ? (
            <div style={{ display: "flex", justifyContent: "center", padding: "var(--space-5)" }}>
              <Spinner size={20} />
            </div>
          ) : (
            <form
              id="action-form"
              className={styles.stack}
              onSubmit={(e) => void handleSubmit(onSubmit)(e)}
            >
              <section className={styles.section}>
                <h3 className={styles.sectionTitle}>Identity</h3>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="action-name">
                    Name
                  </label>
                  <Input
                    id="action-name"
                    {...register("name")}
                    invalid={nameInvalid}
                    placeholder="e.g. slack-prod"
                  />
                </div>
                <div className={styles.field}>
                  <label className={styles.label} htmlFor="action-type">
                    Type
                  </label>
                  <select
                    id="action-type"
                    value={action_type}
                    onChange={(e) => {
                      const next = e.target.value;
                      setValue("action_type", next, { shouldDirty: true });
                      // Switching plugin resets the typed subcontent so we
                      // don't carry stale fields between plugin schemas.
                      setAction({});
                    }}
                    style={{
                      height: 28,
                      fontSize: "var(--text-sm)",
                      background: "var(--bg-surface)",
                      color: "var(--text-strong)",
                      border: "1px solid var(--border)",
                      borderRadius: "var(--radius-md)",
                      padding: "0 var(--space-2)",
                      width: "100%",
                    }}
                  >
                    <option value="" disabled hidden>
                      Select an action type…
                    </option>
                    {formPlugins.map((m) => (
                      <option key={m.plugin_name} value={m.plugin_name}>
                        {m.display_name ?? m.name}
                      </option>
                    ))}
                    {/* Preserve unknown action_type so we don't silently drop it. */}
                    {action_type && !selectedPlugin ? (
                      <option key={action_type} value={action_type}>
                        {action_type}
                      </option>
                    ) : null}
                  </select>
                </div>
              </section>
              {selectedPlugin && selectedPlugin.action_form ? (
                <section className={styles.section}>
                  <h3 className={styles.sectionTitle}>Config</h3>
                  <MetadataForm
                    fields={selectedPlugin.action_form}
                    value={action}
                    onChange={setAction}
                    idPrefix={`action-${selectedPlugin.plugin_name}`}
                  />
                </section>
              ) : useJsonFallback ? (
                <section className={styles.section}>
                  <h3 className={styles.sectionTitle}>Config (JSON)</h3>
                  <Textarea
                    {...register("action_json")}
                    rows={10}
                    invalid={!!jsonError}
                    style={{ fontFamily: "var(--font-mono)" }}
                  />
                  {jsonError ? (
                    <span style={{ color: "var(--severity-critical)", fontSize: "var(--text-xs)" }}>
                      {jsonError}
                    </span>
                  ) : null}
                </section>
              ) : null}
              <div className={styles.field}>
                <label className={styles.label} htmlFor="action-comment">
                  Comment
                </label>
                <Textarea id="action-comment" {...register("comment")} rows={2} />
              </div>
            </form>
          )}
        </DrawerBody>
        <DrawerFooter>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            type="submit"
            form="action-form"
            variant="primary"
            loading={submitting}
            disabled={submitting}
          >
            {isCreate ? "Create" : "Save"}
          </Button>
        </DrawerFooter>
      </DrawerContent>
    </Drawer>
  );
}
