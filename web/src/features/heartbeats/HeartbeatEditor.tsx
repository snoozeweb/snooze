import { useWatch, type Control, type UseFormSetValue } from "react-hook-form";
import { Icon } from "@/shared/icons/Icon";
import { Switch } from "@/shared/ui/Switch";
import { Input } from "@/shared/ui/Input";
import { Badge } from "@/shared/ui/Badge";
import { CopyField } from "@/shared/ui/CopyField";
import { DiffSection } from "@/shared/ui/DiffSection";
import { EditorDrawer, useFieldInvalid, type EditorBodyProps } from "@/shared/forms/EditorDrawer";
import { formatRelativeTime } from "@/lib/format/time";
import { Heartbeats } from "./api";
import { STATUS_BADGE } from "./statusBadge";
import { parsedLastSeenEpoch } from "./format";
import type { Heartbeat } from "./types";
import styles from "./HeartbeatEditor.module.css";

type FormShape = {
  name: string;
  interval: number;
  grace: number;
  max_latency: number; // 0 means disabled
  severity: string;
  environment: string;
  host: string;
  message: string;
  enabled: boolean;
};

const EMPTY_FORM: FormShape = {
  name: "",
  interval: 3600,
  grace: 0,
  max_latency: 0,
  severity: "",
  environment: "",
  host: "",
  message: "",
  enabled: true,
};

export type HeartbeatEditorProps = {
  uid: string | undefined;
  onClose: () => void;
};

export function HeartbeatEditor({ uid, onClose }: HeartbeatEditorProps) {
  const isCreate = uid === undefined || uid === "";
  const get = Heartbeats.useGet(isCreate ? undefined : uid);
  const create = Heartbeats.useCreate();
  const update = Heartbeats.useUpdate();

  return (
    <EditorDrawer<FormShape, Heartbeat>
      uid={uid}
      onClose={onClose}
      get={get}
      create={create}
      update={update}
      emptyForm={EMPTY_FORM}
      recordToForm={(r) => ({
        name: r.name ?? "",
        interval: r.interval ?? 3600,
        grace: r.grace ?? 0,
        max_latency: r.max_latency ?? 0,
        severity: r.severity ?? "",
        environment: r.environment ?? "",
        host: r.host ?? "",
        message: r.message ?? "",
        enabled: r.enabled ?? true,
      })}
      formToBody={(form) => {
        const body: Partial<Heartbeat> = {
          name: form.name,
          interval: form.interval,
          enabled: form.enabled,
        };
        if (form.grace !== 0) body.grace = form.grace;
        if (form.max_latency !== 0) body.max_latency = form.max_latency;
        if (form.severity.trim()) body.severity = form.severity;
        if (form.environment.trim()) body.environment = form.environment;
        if (form.host.trim()) body.host = form.host;
        if (form.message.trim()) body.message = form.message;
        return body;
      }}
      title={(c) => (c ? "New heartbeat" : "Edit heartbeat")}
      titleToolbar={({ control, setValue }) => (
        <HeartbeatEnabledToggle control={control} setValue={setValue} />
      )}
      footerStart={({ control }) => (
        <HeartbeatDiff control={control} original={isCreate ? undefined : get.data} />
      )}
      successMessage={{ create: "Heartbeat created", update: "Heartbeat saved" }}
      formId="heartbeat-form"
      formClassName={styles.stack}
    >
      {(body) => <HeartbeatFields {...body} editData={isCreate ? undefined : get.data} />}
    </EditorDrawer>
  );
}

function HeartbeatEnabledToggle({
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

function HeartbeatDiff({
  control,
  original,
}: {
  control: Control<FormShape>;
  original: Heartbeat | undefined;
}) {
  const name = useWatch({ control, name: "name" });
  const interval = useWatch({ control, name: "interval" });
  const grace = useWatch({ control, name: "grace" });
  const max_latency = useWatch({ control, name: "max_latency" });
  const severity = useWatch({ control, name: "severity" });
  const environment = useWatch({ control, name: "environment" });
  const host = useWatch({ control, name: "host" });
  const message = useWatch({ control, name: "message" });
  const enabled = useWatch({ control, name: "enabled" });
  const projected: Partial<Heartbeat> = {
    name,
    interval,
    enabled,
    ...(grace !== 0 ? { grace } : {}),
    ...(max_latency !== 0 ? { max_latency } : {}),
    ...(severity.trim() ? { severity } : {}),
    ...(environment.trim() ? { environment } : {}),
    ...(host.trim() ? { host } : {}),
    ...(message.trim() ? { message } : {}),
  };
  return <DiffSection original={original} current={projected} />;
}

function HeartbeatFields({
  control,
  register,
  isCreate,
  editData,
}: EditorBodyProps<FormShape> & { editData: Heartbeat | undefined }) {
  const nameInvalid = useFieldInvalid(control, "name");

  const pingUrl =
    !isCreate && editData
      ? `${window.location.origin}/api/v1/webhook/heartbeat?name=${editData.name}&token=${editData.token ?? ""}`
      : "";

  const lastSeenEpoch = parsedLastSeenEpoch(editData?.last_seen);
  const lastSeenDisplay = lastSeenEpoch !== undefined ? formatRelativeTime(lastSeenEpoch) : "never";
  const lastLatencyDisplay =
    editData?.last_latency !== undefined ? `${editData.last_latency} ms` : "—";

  return (
    <>
      <section className={styles.section}>
        <h3 className={styles.sectionTitle}>Identity</h3>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="heartbeat-name">
            Name
          </label>
          <Input
            id="heartbeat-name"
            {...register("name")}
            invalid={nameInvalid}
            errorMessage={nameInvalid ? "Name is required." : undefined}
            placeholder="e.g. nightly-backup"
          />
        </div>
      </section>

      <section className={styles.section}>
        <h3 className={styles.sectionTitle}>Schedule</h3>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="heartbeat-interval">
            Interval (seconds)
          </label>
          <Input
            id="heartbeat-interval"
            type="number"
            {...register("interval", { valueAsNumber: true })}
            placeholder="3600"
          />
        </div>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="heartbeat-grace">
            Grace (seconds)
          </label>
          <Input
            id="heartbeat-grace"
            type="number"
            {...register("grace", { valueAsNumber: true })}
            placeholder="0"
          />
        </div>
      </section>

      <section className={styles.section}>
        <h3 className={styles.sectionTitle}>Latency detection</h3>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="heartbeat-max-latency">
            Max latency (ms, 0 = disabled)
          </label>
          <Input
            id="heartbeat-max-latency"
            type="number"
            {...register("max_latency", { valueAsNumber: true })}
            placeholder="0"
          />
        </div>
      </section>

      <section className={styles.section}>
        <h3 className={styles.sectionTitle}>Alert shape</h3>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="heartbeat-severity">
            Severity
          </label>
          <Input id="heartbeat-severity" {...register("severity")} placeholder="critical" />
        </div>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="heartbeat-environment">
            Environment
          </label>
          <Input id="heartbeat-environment" {...register("environment")} placeholder="" />
        </div>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="heartbeat-host">
            Host
          </label>
          <Input id="heartbeat-host" {...register("host")} placeholder="" />
        </div>
        <div className={styles.field}>
          <label className={styles.label} htmlFor="heartbeat-message">
            Message
          </label>
          <Input id="heartbeat-message" {...register("message")} placeholder="" />
        </div>
      </section>

      {!isCreate && editData ? (
        <section className={styles.section}>
          <h3 className={styles.sectionTitle}>Ping setup</h3>
          <div className={styles.field}>
            <span className={styles.label}>Token</span>
            <CopyField value={editData.token ?? ""} label="Token" />
          </div>
          <div className={styles.field}>
            <span className={styles.label}>Ping URL</span>
            <CopyField value={pingUrl} label="Ping URL" />
          </div>
          <div className={styles.field}>
            <span className={styles.label}>Status</span>
            <span aria-label={`status: ${editData.status ?? "ok"}`}>
              <Badge variant={STATUS_BADGE[editData.status ?? "ok"]}>
                {editData.status ?? "ok"}
              </Badge>
            </span>
          </div>
          <div className={styles.field}>
            <span className={styles.label}>Last seen</span>
            <span className={styles.readOnlyValue}>{lastSeenDisplay}</span>
          </div>
          <div className={styles.field}>
            <span className={styles.label}>Last latency</span>
            <span className={styles.readOnlyValue}>{lastLatencyDisplay}</span>
          </div>
        </section>
      ) : null}

      {isCreate ? (
        <aside className={styles.note} role="note">
          <span className={styles.noteIcon} aria-hidden="true">
            <Icon name="info" size={16} />
          </span>
          <div>
            <p className={styles.noteTitle}>Ping URL &amp; API key generated on save</p>
            <p className={styles.noteBody}>
              When you create this heartbeat, Snooze generates a secret token (its API key) and a
              unique ping URL. Your job calls that URL on schedule to keep the switch alive — both
              appear here, ready to copy, the moment you save.
            </p>
          </div>
        </aside>
      ) : null}
    </>
  );
}
