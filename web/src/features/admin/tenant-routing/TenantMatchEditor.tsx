import {
  useWatch,
  type Control,
  type UseFormRegister,
  type UseFormSetValue,
} from "react-hook-form";
import { Input } from "@/shared/ui/Input";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/shared/ui/Select";
import { EditorDrawer, EditorAbort, type EditorBodyProps } from "@/shared/forms/EditorDrawer";
import { Tenants } from "../tenants/api";
import { TenantMatchRules } from "./api";
import type { TenantMatchRule, TenantMatchRuleBody, TenantMatchRulePatch } from "./types";
import styles from "./TenantMatchEditor.module.css";

type MatchType = "group" | "domain" | "login";

type FormShape = {
  match_type: MatchType;
  match: string;
  tenant_id: string;
  priority: number;
};

const EMPTY_FORM: FormShape = {
  match_type: "group",
  match: "",
  tenant_id: "",
  priority: 0,
};

export type TenantMatchEditorProps = {
  /** undefined = create mode; a uid string = edit mode. */
  uid: string | undefined;
  onClose: () => void;
};

export function TenantMatchEditor({ uid, onClose }: TenantMatchEditorProps) {
  const get = TenantMatchRules.useGet(uid);
  const create = TenantMatchRules.useCreate();
  const update = TenantMatchRules.useUpdate();

  return (
    <EditorDrawer<FormShape, TenantMatchRule, TenantMatchRuleBody, TenantMatchRulePatch>
      uid={uid}
      onClose={onClose}
      get={get}
      create={create}
      update={update}
      emptyForm={EMPTY_FORM}
      recordToForm={(r) => ({
        match_type: r.match_type,
        match: r.match,
        tenant_id: r.tenant_id,
        priority: r.priority ?? 0,
      })}
      formToBody={(form) => {
        if (!form.match.trim() || !form.tenant_id) throw new EditorAbort();
        const body: TenantMatchRuleBody = {
          match_type: form.match_type,
          match: form.match.trim(),
          tenant_id: form.tenant_id,
          ...(form.priority !== 0 ? { priority: form.priority } : {}),
        };
        return body;
      }}
      title={(c) => (c ? "New routing rule" : "Edit routing rule")}
      conflictMessage="A rule for this (match type, match) pair already exists."
      successMessage={{ create: "Rule created", update: "Rule saved" }}
      formId="tenant-match-form"
      formClassName={styles.stack}
    >
      {(body) => <TenantMatchFields {...body} />}
    </EditorDrawer>
  );
}

function matchInputLabel(matchType: MatchType): string {
  if (matchType === "group") return "Group name (case-insensitive)";
  if (matchType === "domain") return "Email domain (e.g. example.com)";
  return "Username (case-insensitive)";
}

function TenantMatchFields({ control, register, setValue, isCreate }: EditorBodyProps<FormShape>) {
  return (
    <>
      <div className={styles.field}>
        <span className={styles.label} id="match-type-label">
          Match type
        </span>
        <MatchTypeSelect control={control} setValue={setValue} />
      </div>
      <MatchValueField control={control} register={register} />
      <div className={styles.field}>
        <span className={styles.label} id="tenant-picker-label">
          Target tenant
        </span>
        <TenantSelect control={control} setValue={setValue} isCreate={isCreate} />
      </div>
      <div className={styles.field}>
        <label className={styles.label} htmlFor="rule-priority">
          Priority
        </label>
        <Input
          id="rule-priority"
          type="number"
          min={0}
          max={999}
          {...register("priority", { valueAsNumber: true })}
          aria-label="Priority"
        />
      </div>
    </>
  );
}

function MatchTypeSelect({
  control,
  setValue,
}: {
  control: Control<FormShape>;
  setValue: UseFormSetValue<FormShape>;
}) {
  const matchType = useWatch({ control, name: "match_type" });
  return (
    <Select
      value={matchType}
      onValueChange={(v) => setValue("match_type", v as MatchType, { shouldDirty: true })}
    >
      <SelectTrigger aria-labelledby="match-type-label" />
      <SelectContent>
        <SelectItem value="group">Group</SelectItem>
        <SelectItem value="domain">Domain</SelectItem>
        <SelectItem value="login">Login</SelectItem>
      </SelectContent>
    </Select>
  );
}

function MatchValueField({
  control,
  register,
}: {
  control: Control<FormShape>;
  register: UseFormRegister<FormShape>;
}) {
  const matchType = useWatch({ control, name: "match_type" });
  const label = matchInputLabel(matchType);
  return (
    <div className={styles.field}>
      <label className={styles.label} htmlFor="rule-match">
        {label}
      </label>
      <Input id="rule-match" {...register("match")} aria-label={label} placeholder="" />
    </div>
  );
}

function TenantSelect({
  control,
  setValue,
}: {
  control: Control<FormShape>;
  setValue: UseFormSetValue<FormShape>;
  isCreate: boolean;
}) {
  // TODO: If tenant count exceeds 200 in practice the picker will silently
  // truncate — type a slug directly if yours is not listed.
  const tenantsQuery = Tenants.useList({ limit: 200 });
  const tenantList = tenantsQuery.data?.data ?? [];
  const tenantId = useWatch({ control, name: "tenant_id" });
  return (
    <Select value={tenantId} onValueChange={(v) => setValue("tenant_id", v, { shouldDirty: true })}>
      <SelectTrigger aria-labelledby="tenant-picker-label" />
      <SelectContent>
        {tenantList.map((t) => (
          <SelectItem key={t.id} value={t.id}>
            {t.display_name ? `${t.display_name} (${t.id})` : t.id}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
