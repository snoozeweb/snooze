import { useMemo, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@/shared/ui/Button";
import { Spinner } from "@/shared/ui/Spinner";
import { TabList, TabPanel, TabTrigger, Tabs } from "@/shared/ui/Tabs";
import { Textarea } from "@/shared/ui/Textarea";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import type { FormField } from "@/shared/forms/types";
import { Settings, useSettingsCatalogue, useSettingsList } from "./api";
import { SettingCard } from "./SettingCard";
import type { Setting } from "./types";
import styles from "./SettingsPage.module.css";

// Canonical tab order. Anything in the catalogue with a `group:` key not
// listed here is appended at the end, title-cased.
const TAB_ORDER = ["general", "notifications", "ldap", "housekeeping"] as const;

const TAB_LABELS: Record<string, string> = {
  general: "General",
  notifications: "Notifications",
  ldap: "LDAP",
  housekeeping: "Housekeeping",
};

/**
 * Normalize the group key so the catalogue's singular `notification` and
 * the canonical plural `notifications` both bucket into the same tab.
 * Older metadata.yaml entries use the singular; new keys should use the
 * plural per the brief.
 */
function normaliseGroup(group: string | undefined): string {
  if (!group) return "general";
  if (group === "notification") return "notifications";
  return group;
}

function tabLabel(group: string): string {
  return TAB_LABELS[group] ?? group.charAt(0).toUpperCase() + group.slice(1);
}

type Group = {
  key: string;
  label: string;
  entries: Array<[string, FormField]>;
};

/**
 * Buckets catalogue entries by their `group:` value, then orders the buckets
 * by TAB_ORDER (anything unknown appended). Within each bucket we preserve
 * catalogue insertion order so the YAML controls visible card ordering.
 */
function buildGroups(catalogue: Record<string, FormField>): Group[] {
  const buckets = new Map<string, Array<[string, FormField]>>();
  for (const [key, field] of Object.entries(catalogue)) {
    const g = normaliseGroup(field.group);
    if (!buckets.has(g)) buckets.set(g, []);
    buckets.get(g)!.push([key, field]);
  }
  const seen = new Set<string>();
  const ordered: Group[] = [];
  for (const g of TAB_ORDER) {
    if (buckets.has(g)) {
      ordered.push({ key: g, label: tabLabel(g), entries: buckets.get(g)! });
      seen.add(g);
    }
  }
  for (const [g, entries] of buckets.entries()) {
    if (!seen.has(g)) ordered.push({ key: g, label: tabLabel(g), entries });
  }
  return ordered;
}

export function SettingsPage() {
  const { catalogue, isLoading: catalogueLoading } = useSettingsCatalogue();
  const { byName, records, isLoading: recordsLoading } = useSettingsList();
  const queryClient = useQueryClient();

  // Custom-key bucket: any Setting record whose name isn't in the catalogue.
  const customRecords = useMemo(() => {
    if (!catalogue) return [];
    return records.filter((r) => !(r.name in catalogue));
  }, [records, catalogue]);

  const groups = useMemo(() => (catalogue ? buildGroups(catalogue) : []), [catalogue]);

  // Default to the first available tab so the page never opens to a panel
  // with no content. Falls back to "general" while the catalogue loads.
  const firstTab = groups[0]?.key ?? "general";
  const [tab, setTab] = useState<string>(firstTab);

  function refreshList() {
    void queryClient.invalidateQueries({ queryKey: ["settings"] });
  }

  if (catalogueLoading || recordsLoading) {
    return (
      <div className={styles.page}>
        <div style={{ display: "flex", justifyContent: "center", padding: "var(--space-5)" }}>
          <Spinner size={20} />
        </div>
      </div>
    );
  }

  const showCustomTab = customRecords.length > 0;

  return (
    <div className={styles.page}>
      <Tabs value={tab} onValueChange={setTab}>
        <TabList>
          {groups.map((g) => (
            <TabTrigger key={g.key} value={g.key}>
              {g.label}
            </TabTrigger>
          ))}
          {showCustomTab ? <TabTrigger value="__custom__">Custom</TabTrigger> : null}
        </TabList>
        {groups.map((g) => {
          const ldapEnabledRecord = byName["ldap.enabled"];
          const ldapEnabledDefault = catalogue?.["ldap.enabled"]?.default_value;
          const ldapEnabled = Boolean(ldapEnabledRecord?.value ?? ldapEnabledDefault ?? false);
          return (
            <TabPanel key={g.key} value={g.key}>
              <div className={styles.cards}>
                {g.entries.length === 0 ? (
                  <div className={styles.empty}>No settings in this group.</div>
                ) : (
                  g.entries
                    .filter(([name]) => {
                      if (g.key !== "ldap") return true;
                      if (name === "ldap.enabled") return true;
                      return ldapEnabled;
                    })
                    .map(([name, field]) => {
                      const record = byName[name];
                      return (
                        <SettingCard
                          key={name}
                          field={field}
                          name={name}
                          initialValue={record?.value}
                          recordUid={record?.uid}
                          onChange={refreshList}
                        />
                      );
                    })
                )}
                {g.key === "ldap" && !ldapEnabled ? (
                  <div className={styles.empty}>
                    Enable LDAP above to configure connection, user, and group settings.
                  </div>
                ) : null}
              </div>
            </TabPanel>
          );
        })}
        {showCustomTab ? (
          <TabPanel value="__custom__">
            <div className={styles.cards}>
              {customRecords.map((r) => (
                <CustomSettingCard key={r.uid ?? r.name} record={r} onChange={refreshList} />
              ))}
            </div>
          </TabPanel>
        ) : null}
      </Tabs>
    </div>
  );
}

function CustomSettingCard({ record, onChange }: { record: Setting; onChange: () => void }) {
  const update = Settings.useUpdate();
  const remove = Settings.useRemove();
  const initialJson = useMemo(() => {
    try {
      return JSON.stringify(record.value ?? null, null, 2);
    } catch {
      return "null";
    }
  }, [record.value]);
  const [text, setText] = useState(initialJson);
  const [err, setErr] = useState<string | null>(null);

  const dirty = text !== initialJson;

  async function save() {
    setErr(null);
    let parsed: unknown;
    try {
      parsed = JSON.parse(text);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Invalid JSON");
      return;
    }
    try {
      if (record.uid === undefined) return;
      await update.mutateAsync({ uid: record.uid, body: { name: record.name, value: parsed } });
      toast.success(`Saved ${record.name}`);
      onChange();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : "Save failed");
    }
  }

  async function del() {
    try {
      if (record.uid === undefined) return;
      await remove.mutateAsync(record.uid);
      toast.success(`Deleted ${record.name}`);
      onChange();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : "Delete failed");
    }
  }

  return (
    <section className={styles.customCard}>
      <div className={styles.customHeader}>
        <span className={styles.customName}>{record.name}</span>
        <span style={{ color: "var(--text-muted)", fontSize: "var(--text-xs)" }}>
          legacy / unknown key
        </span>
      </div>
      <Textarea
        rows={6}
        value={text}
        onChange={(e) => {
          setText(e.target.value);
          if (err) setErr(null);
        }}
        invalid={!!err}
        className={styles.customJson}
      />
      {err ? <span className={styles.customError}>{err}</span> : null}
      <div className={styles.customActions}>
        <Button
          size="sm"
          variant="danger"
          leadingIcon="trash"
          onClick={() => void del()}
          loading={remove.isPending}
        >
          Delete
        </Button>
        <Button
          size="sm"
          variant="primary"
          onClick={() => void save()}
          loading={update.isPending}
          disabled={!dirty}
        >
          Save
        </Button>
      </div>
    </section>
  );
}
