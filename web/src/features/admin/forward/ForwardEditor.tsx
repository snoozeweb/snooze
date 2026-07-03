import { useWatch, type Control, type UseFormSetValue } from "react-hook-form";
import { ConditionPreview } from "@/shared/ui/ConditionPreview";
import { Switch } from "@/shared/ui/Switch";
import { Input } from "@/shared/ui/Input";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/shared/ui/Select";
import { MultiCombobox } from "@/shared/ui/MultiCombobox";
import { CollapsibleSection } from "@/shared/ui/CollapsibleSection";
import { DiffSection } from "@/shared/ui/DiffSection";
import { ConditionEditor } from "@/shared/condition/ConditionEditor";
import type { Condition } from "@/lib/condition/types";
import { EditorDrawer, useFieldInvalid, type EditorBodyProps } from "@/shared/forms/EditorDrawer";
import { Forward } from "./api";
import type { ForwardDestination, ForwardAuth } from "./types";
import { formatAuthType } from "./columns";
import styles from "./ForwardEditor.module.css";

// "none" is the UI-only sentinel for "no auth"; formToBody maps it to the
// empty string (omit auth) as the backend expects. Radix Select requires
// all item values to be non-empty strings.
type AuthTypeSentinel = "none" | "bearer" | "basic" | "apikey";

type FormShape = {
  name: string;
  enabled: boolean;
  endpoint: string;
  event_classes: string[];
  condition: Condition;
  auth_type: AuthTypeSentinel;
  auth_token: string;
  auth_username: string;
  auth_password: string;
  auth_api_key: string;
  auth_header: string;
  tls_insecure: boolean;
  timeout: string;
};

const EMPTY_FORM: FormShape = {
  name: "",
  enabled: true,
  endpoint: "",
  event_classes: ["*"],
  condition: { type: "ALWAYS_TRUE" },
  auth_type: "none",
  auth_token: "",
  auth_username: "",
  auth_password: "",
  auth_api_key: "",
  auth_header: "X-API-Key",
  tls_insecure: false,
  timeout: "",
};

export type ForwardEditorProps = {
  uid: string | undefined;
  onClose: () => void;
};

export function ForwardEditor({ uid, onClose }: ForwardEditorProps) {
  const isCreate = uid === undefined || uid === "";
  const get = Forward.useGet(isCreate ? undefined : uid);
  const create = Forward.useCreate();
  const update = Forward.useUpdate();

  return (
    <EditorDrawer<FormShape, ForwardDestination>
      uid={uid}
      onClose={onClose}
      get={get}
      create={create}
      update={update}
      emptyForm={EMPTY_FORM}
      recordToForm={(d) => ({
        name: d.name ?? "",
        enabled: d.enabled ?? true,
        endpoint: d.endpoint ?? "",
        event_classes: d.event_classes ?? ["*"],
        condition: d.condition ?? { type: "ALWAYS_TRUE" },
        auth_type: (d.auth?.type as AuthTypeSentinel | undefined) ?? "none",
        auth_token: d.auth?.token ?? "",
        auth_username: d.auth?.username ?? "",
        auth_password: d.auth?.password ?? "",
        auth_api_key: d.auth?.api_key ?? "",
        auth_header: d.auth?.header ?? "X-API-Key",
        tls_insecure: d.tls_insecure ?? false,
        timeout: d.timeout ?? "",
      })}
      formToBody={(form) => {
        const auth: ForwardAuth | undefined =
          form.auth_type === "none"
            ? undefined
            : {
                type: form.auth_type as "" | "bearer" | "basic" | "apikey",
                ...(form.auth_type === "bearer" && form.auth_token
                  ? { token: form.auth_token }
                  : {}),
                ...(form.auth_type === "basic" && form.auth_username
                  ? { username: form.auth_username }
                  : {}),
                ...(form.auth_type === "basic" && form.auth_password
                  ? { password: form.auth_password }
                  : {}),
                ...(form.auth_type === "apikey" && form.auth_api_key
                  ? { api_key: form.auth_api_key }
                  : {}),
                ...(form.auth_type === "apikey" && form.auth_header !== "X-API-Key"
                  ? { header: form.auth_header }
                  : {}),
              };
        return {
          name: form.name,
          enabled: form.enabled,
          ...(form.endpoint ? { endpoint: form.endpoint } : {}),
          ...(form.event_classes.length > 0 ? { event_classes: form.event_classes } : {}),
          condition: form.condition,
          ...(auth ? { auth } : {}),
          ...(form.tls_insecure ? { tls_insecure: true } : {}),
          ...(form.timeout ? { timeout: form.timeout } : {}),
        };
      }}
      title={(c) => (c ? "New forward destination" : "Edit forward destination")}
      titleToolbar={({ control, setValue }) => (
        <ForwardEnabledToggle control={control} setValue={setValue} />
      )}
      footerStart={({ control }) => (
        <ForwardDiff control={control} original={isCreate ? undefined : get.data} />
      )}
      successMessage={{
        create: "Forward destination created",
        update: "Forward destination saved",
      }}
      conflictMessage="name already taken"
      formId="forward-form"
      formClassName={styles.stack}
    >
      {(body) => <ForwardFields {...body} />}
    </EditorDrawer>
  );
}

function ForwardEnabledToggle({
  control,
  setValue,
}: {
  control: Control<FormShape>;
  setValue: UseFormSetValue<FormShape>;
}) {
  const enabled = useWatch({ control, name: "enabled" });
  return (
    <>
      <Switch
        checked={enabled}
        onCheckedChange={(v) => setValue("enabled", v, { shouldDirty: true })}
        aria-label="Enabled"
      />
      <span>{enabled ? "Enabled" : "Disabled"}</span>
    </>
  );
}

function ForwardDiff({
  control,
  original,
}: {
  control: Control<FormShape>;
  original: ForwardDestination | undefined;
}) {
  const name = useWatch({ control, name: "name" });
  const enabled = useWatch({ control, name: "enabled" });
  const endpoint = useWatch({ control, name: "endpoint" });
  const auth_type = useWatch({ control, name: "auth_type" });
  const condition = useWatch({ control, name: "condition" });
  const event_classes = useWatch({ control, name: "event_classes" });
  const projected: ForwardDestination = {
    name,
    enabled,
    ...(endpoint ? { endpoint } : {}),
    ...(event_classes.length > 0 ? { event_classes } : {}),
    condition,
    ...(auth_type && auth_type !== "none"
      ? { auth: { type: auth_type as "" | "bearer" | "basic" | "apikey" } }
      : {}),
  };
  return <DiffSection original={original} current={projected} />;
}

const EVENT_CLASS_OPTIONS = [
  { value: "*", label: "All (*)" },
  { value: "alerts", label: "Alerts" },
];

function ForwardFields({ control, register, setValue }: EditorBodyProps<FormShape>) {
  const nameInvalid = useFieldInvalid(control, "name");
  const condition = useWatch({ control, name: "condition" });
  const eventClasses = useWatch({ control, name: "event_classes" });
  const authType = useWatch({ control, name: "auth_type" });
  const tlsInsecure = useWatch({ control, name: "tls_insecure" });

  const advancedSummary = [
    formatAuthType(authType === "none" ? undefined : authType),
    tlsInsecure ? "TLS verify off" : null,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <>
      <section className={styles.section}>
        <h3 className={styles.sectionTitle}>Identity</h3>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="forward-name">
            Name
          </label>
          <Input
            id="forward-name"
            {...register("name")}
            invalid={nameInvalid}
            placeholder="e.g. peer-prod"
          />
        </div>
      </section>
      <section className={styles.section}>
        <h3 className={styles.sectionTitle}>Destination</h3>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="forward-endpoint">
            Endpoint
          </label>
          <Input
            id="forward-endpoint"
            {...register("endpoint")}
            placeholder="https://peer/api/v1/alerts"
            type="url"
          />
        </div>
        <div className={styles.field}>
          <span className={styles.label}>Event classes</span>
          <MultiCombobox
            aria-label="Event classes"
            placeholder="Select event classes"
            options={EVENT_CLASS_OPTIONS}
            value={eventClasses}
            onChange={(next) => setValue("event_classes", next, { shouldDirty: true })}
            allowCustom={false}
          />
        </div>
      </section>
      <section className={styles.section}>
        <h3 className={styles.sectionTitle}>Condition</h3>
        <ConditionEditor
          value={condition}
          onChange={(c) => setValue("condition", c, { shouldDirty: true })}
          plugin="record"
        />
        <div style={{ marginTop: "var(--space-2)" }}>
          <ConditionPreview condition={condition} />
        </div>
      </section>
      <CollapsibleSection title="Advanced" summary={advancedSummary} defaultOpen={false}>
        <div className={styles.stack}>
          <p className={styles.hint}>
            Federation relays are fire-and-forget and run after the full pipeline. Each Snooze node
            in an HA cluster must set a distinct <code>syncer.hostname</code>; nodes that find their
            own hostname in the inbound <code>X-Snooze-Loop</code> header accept the alert without
            re-relaying.
          </p>
          <section className={styles.section}>
            <h3 className={styles.sectionTitle}>Auth</h3>
            <div className={styles.field}>
              <span className={styles.label} id="forward-auth-type-label">
                Auth type
              </span>
              <ForwardAuthTypeSelect control={control} setValue={setValue} />
            </div>
            <ForwardAuthFields control={control} register={register} />
          </section>
          <div className={styles.field}>
            <div style={{ display: "flex", alignItems: "center", gap: "var(--space-2)" }}>
              <ForwardTlsSwitch control={control} setValue={setValue} />
              <span className={styles.label}>Skip TLS verification</span>
            </div>
          </div>
          <div className={styles.field}>
            <label className={styles.label} htmlFor="forward-timeout">
              Timeout
            </label>
            <Input id="forward-timeout" {...register("timeout")} placeholder="e.g. 30s" />
            <p className={styles.hint}>
              Go duration string (e.g. 30s, 1m). Leave empty for the default.
            </p>
          </div>
        </div>
      </CollapsibleSection>
    </>
  );
}

function ForwardAuthTypeSelect({
  control,
  setValue,
}: {
  control: Control<FormShape>;
  setValue: UseFormSetValue<FormShape>;
}) {
  const authType = useWatch({ control, name: "auth_type" });
  return (
    <Select
      value={authType || "none"}
      onValueChange={(v) => setValue("auth_type", v as AuthTypeSentinel, { shouldDirty: true })}
    >
      <SelectTrigger aria-label="Auth type" />
      <SelectContent>
        <SelectItem value="none">None</SelectItem>
        <SelectItem value="bearer">Bearer token</SelectItem>
        <SelectItem value="basic">Basic (username/password)</SelectItem>
        <SelectItem value="apikey">API key</SelectItem>
      </SelectContent>
    </Select>
  );
}

function ForwardAuthFields({
  control,
  register,
}: {
  control: Control<FormShape>;
  register: EditorBodyProps<FormShape>["register"];
}) {
  const authType = useWatch({ control, name: "auth_type" });

  if (authType === "bearer") {
    return (
      <div className={styles.field}>
        <label className={styles.label} htmlFor="forward-auth-token">
          Token
        </label>
        <Input
          id="forward-auth-token"
          {...register("auth_token")}
          type="password"
          autoComplete="new-password"
          placeholder="Bearer token"
        />
      </div>
    );
  }

  if (authType === "basic") {
    return (
      <div className={styles.grid2}>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="forward-auth-username">
            Username
          </label>
          <Input id="forward-auth-username" {...register("auth_username")} placeholder="username" />
        </div>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="forward-auth-password">
            Password
          </label>
          <Input
            id="forward-auth-password"
            {...register("auth_password")}
            type="password"
            autoComplete="new-password"
            placeholder="password"
          />
        </div>
      </div>
    );
  }

  if (authType === "apikey") {
    return (
      <div className={styles.grid2}>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="forward-auth-api-key">
            API Key
          </label>
          <Input
            id="forward-auth-api-key"
            {...register("auth_api_key")}
            type="password"
            autoComplete="new-password"
            placeholder="API key value"
          />
        </div>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="forward-auth-header">
            Header
          </label>
          <Input id="forward-auth-header" {...register("auth_header")} placeholder="X-API-Key" />
        </div>
      </div>
    );
  }

  return null;
}

function ForwardTlsSwitch({
  control,
  setValue,
}: {
  control: Control<FormShape>;
  setValue: UseFormSetValue<FormShape>;
}) {
  const tls = useWatch({ control, name: "tls_insecure" });
  return (
    <Switch
      checked={tls}
      onCheckedChange={(v) => setValue("tls_insecure", v, { shouldDirty: true })}
      aria-label="Skip TLS verification"
    />
  );
}
