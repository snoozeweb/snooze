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
import { BrandIcon } from "@/shared/icons/BrandIcon";
import { brandFor } from "@/shared/icons/brand-names";
import { IntegrationGallery } from "./IntegrationGallery";
import { Actions, useTestAction } from "./api";
import type { Action } from "./types";
import styles from "./NotificationEditor.module.css";

type FormShape = {
  name: string;
  comment: string;
  // `selected` is the notifier plugin registry key (mail / webhook / teams / …).
  selected: string;
  // JSON fallback for actions whose plugin isn't in /metadata (legacy / 3rd-party).
  subcontent_json: string;
};

const EMPTY_FORM: FormShape = {
  name: "",
  comment: "",
  selected: "",
  subcontent_json: "{}",
};

export type ActionEditorProps = {
  uid: string | undefined;
  onClose: () => void;
};

function plugins_with_form(list: Metadata[] | undefined): Metadata[] {
  if (!list) return [];
  return list.filter((m) => m.action_form && Object.keys(m.action_form).length > 0);
}

export function ActionEditor({ uid, onClose }: ActionEditorProps) {
  const isCreate = uid === undefined || uid === "";
  const existing = Actions.useGet(isCreate ? undefined : uid);
  const create = Actions.useCreate();
  const update = Actions.useUpdate();
  const metadata = useAllMetadata();
  const testAction = useTestAction();

  // Wizard step: new actions start by picking an integration; edits jump
  // straight to the config form (the type is already chosen).
  const [step, setStep] = useState<"pick" | "configure">(isCreate ? "pick" : "configure");

  const { register, handleSubmit, reset, formState, watch, setValue } = useForm<FormShape>({
    defaultValues: EMPTY_FORM,
  });

  // Subcontent for the typed form is kept outside react-hook-form because it's
  // a free-form object the MetadataForm renderer owns end-to-end.
  const [subcontent, setSubcontent] = useState<Record<string, unknown>>({});

  useEffect(() => {
    if (isCreate) {
      reset(EMPTY_FORM);
      setSubcontent({});
      setStep("pick");
      return;
    }
    if (existing.data) {
      const env = existing.data.action ?? {};
      const selected = env.selected ?? "";
      const sub = env.subcontent ?? {};
      reset({
        name: existing.data.name ?? "",
        comment: existing.data.comment ?? "",
        selected,
        subcontent_json: JSON.stringify(sub, null, 2),
      });
      setSubcontent(sub);
      setStep("configure");
    }
  }, [existing.data, isCreate, reset]);

  const [submitting, setSubmitting] = useState(false);
  const [jsonError, setJsonError] = useState<string | null>(null);

  const formPlugins = useMemo(() => plugins_with_form(metadata.data), [metadata.data]);
  const selected = watch("selected");
  // Match against plugin_name (the registry key), not the YAML `name:` label.
  const selectedPlugin = useMemo(
    () => formPlugins.find((m) => m.plugin_name === selected),
    [formPlugins, selected],
  );
  const useJsonFallback = !!selected && !selectedPlugin && !metadata.isPending;
  // `selected` is the notifier registry key, so it doubles as the brand id.
  const headerBrand = brandFor(selected);

  function pickIntegration(pluginName: string) {
    setValue("selected", pluginName, { shouldDirty: true });
    setSubcontent({});
    setStep("configure");
  }

  async function onTest() {
    if (!selected) return;
    try {
      await testAction.mutateAsync({ selected, subcontent });
      toast.success("Test notification sent");
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : "Test failed");
    }
  }

  async function onSubmit(form: FormShape) {
    setSubmitting(true);
    setJsonError(null);
    try {
      let sub: Record<string, unknown>;
      if (selectedPlugin) {
        sub = subcontent;
      } else {
        try {
          sub = JSON.parse(form.subcontent_json) as Record<string, unknown>;
          if (typeof sub !== "object" || sub === null || Array.isArray(sub)) {
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
        action: {
          selected: form.selected,
          subcontent: sub,
        },
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
  const picking = step === "pick";
  const heading = picking
    ? "Choose an integration"
    : isCreate
      ? `New ${selectedPlugin?.name || selected} action`
      : "Edit action";

  return (
    <Drawer
      open
      onOpenChange={(o) => {
        if (!o) onClose();
      }}
    >
      <DrawerContent>
        <DrawerTitle>{heading}</DrawerTitle>
        <DrawerBody key={step}>
          {picking ? (
            metadata.isPending ? (
              <div style={{ display: "flex", justifyContent: "center", padding: "var(--space-5)" }}>
                <Spinner size={20} />
              </div>
            ) : (
              <IntegrationGallery plugins={formPlugins} onPick={pickIntegration} />
            )
          ) : !isCreate && existing.isPending ? (
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
                <div className={styles.row} style={{ justifyContent: "space-between" }}>
                  <div className={styles.row}>
                    {headerBrand ? <BrandIcon name={headerBrand} size={20} /> : null}
                    <h3 className={styles.sectionTitle}>
                      {selectedPlugin?.name || selected || "Action"}
                    </h3>
                  </div>
                  <div className={styles.row}>
                    {selectedPlugin?.doc_url ? (
                      <a
                        className={styles.docLink}
                        href={selectedPlugin.doc_url}
                        target="_blank"
                        rel="noreferrer"
                      >
                        Docs ↗
                      </a>
                    ) : null}
                    {isCreate ? (
                      <Button type="button" variant="ghost" onClick={() => setStep("pick")}>
                        Change
                      </Button>
                    ) : null}
                  </div>
                </div>
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
              </section>

              {selectedPlugin && selectedPlugin.action_form ? (
                <section className={styles.section}>
                  <h3 className={styles.sectionTitle}>Config</h3>
                  <MetadataForm
                    fields={selectedPlugin.action_form}
                    value={subcontent}
                    onChange={setSubcontent}
                    idPrefix={`action-${selectedPlugin.plugin_name}`}
                  />
                </section>
              ) : useJsonFallback ? (
                <section className={styles.section}>
                  <h3 className={styles.sectionTitle}>Config (JSON)</h3>
                  <Textarea
                    {...register("subcontent_json")}
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
        {picking ? (
          <DrawerFooter>
            <div style={{ flex: 1 }} />
            <Button variant="ghost" onClick={onClose}>
              Cancel
            </Button>
          </DrawerFooter>
        ) : (
          <DrawerFooter>
            {selectedPlugin ? (
              <Button
                type="button"
                variant="ghost"
                onClick={() => void onTest()}
                loading={testAction.isPending}
                disabled={testAction.isPending || !selected}
              >
                Send test
              </Button>
            ) : null}
            <div style={{ flex: 1 }} />
            <Button variant="ghost" onClick={isCreate ? () => setStep("pick") : onClose}>
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
        )}
      </DrawerContent>
    </Drawer>
  );
}
