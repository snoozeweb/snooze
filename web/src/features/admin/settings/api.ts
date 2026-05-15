import { useMemo } from "react";
import { defineResource } from "@/lib/api/resource";
import { usePluginMetadata } from "@/shared/forms/useMetadata";
import type { FormField } from "@/shared/forms/types";
import type { Setting } from "./types";

export const Settings = defineResource<Setting>("settings");

/**
 * useSettingsCatalogue returns the typed catalogue of runtime settings the
 * server advertises in its settings plugin metadata.yaml. The shape is the
 * same `FormField` record the ActionEditor consumes from `action_form`.
 *
 * Returns `undefined` while the metadata fetch is in-flight (and on error)
 * so callers can render a loading / fallback state. The catalogue is shared
 * (TanStack Query cache keyed by plugin name); multiple components on the
 * same page share a single fetch.
 */
export function useSettingsCatalogue(): {
  catalogue: Record<string, FormField> | undefined;
  isLoading: boolean;
  isError: boolean;
} {
  const query = usePluginMetadata("settings");
  return {
    catalogue: query.data?.setting_form,
    isLoading: query.isPending,
    isError: query.isError,
  };
}

/**
 * useSettingsList fetches every Setting record in one shot and indexes them
 * by `name` for O(1) lookup when wiring up a SettingCard per catalogue key.
 *
 * The settings collection is small (a handful of keys per deployment); a
 * single page-sized fetch is fine and lets the page render every card off a
 * single shared request rather than a per-card useGet.
 */
export function useSettingsList(): {
  byName: Record<string, Setting>;
  records: Setting[];
  isLoading: boolean;
  isError: boolean;
} {
  // A practical ceiling for an internal config collection — anything larger
  // is a misuse of the runtime settings plugin.
  const query = Settings.useList({ limit: 500, offset: 0 });
  const records = useMemo(() => query.data?.data ?? [], [query.data]);
  const byName = useMemo(() => {
    const out: Record<string, Setting> = {};
    for (const s of records) {
      out[s.name] = s;
    }
    return out;
  }, [records]);
  return {
    byName,
    records,
    isLoading: query.isPending,
    isError: query.isError,
  };
}
