import { useMemo, useState } from "react";
import { Icon } from "@/shared/icons/Icon";
import { BrandIcon } from "@/shared/icons/BrandIcon";
import { brandFor } from "@/shared/icons/brand-names";
import { Input } from "@/shared/ui/Input";
import { EmptyState } from "@/shared/ui/EmptyState";
import type { IconName } from "@/shared/icons/icon-names";
import type { Metadata } from "@/shared/forms/types";
import styles from "./IntegrationGallery.module.css";

// Fixed display order + labels for the category buckets.
const CATEGORY_ORDER: { key: string; label: string }[] = [
  { key: "generic", label: "Generic" },
  { key: "chat", label: "Chat" },
  { key: "oncall", label: "On-call / Incident" },
  { key: "ticketing", label: "Ticketing" },
  { key: "push", label: "Push" },
  { key: "sms", label: "SMS" },
];

// Branded notifiers (Slack, Teams, PagerDuty, the Snooze-bell snoozepeer, …)
// render their brand glyph via brandFor() — a sprite symbol from
// web/public/brands.svg or a masked PNG silhouette; everything else falls back
// to the bucket's monochrome glyph from the icon sprite (web/public/icons.svg).
const CATEGORY_ICON: Record<string, IconName> = {
  chat: "message-square",
  oncall: "bell",
  ticketing: "briefcase",
  push: "megaphone",
  sms: "message-square",
  generic: "plug",
};

function bucketOf(m: Metadata): string {
  const c = (m.category ?? "").toLowerCase();
  return CATEGORY_ICON[c] ? c : "generic";
}

export type IntegrationGalleryProps = {
  plugins: Metadata[];
  onPick: (pluginName: string) => void;
};

export function IntegrationGallery({ plugins, onPick }: IntegrationGalleryProps) {
  const [query, setQuery] = useState("");

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return plugins;
    return plugins.filter((m) => {
      const name = (m.name || m.plugin_name).toLowerCase();
      const display = (m.display_name ?? "").toLowerCase();
      return name.includes(q) || display.includes(q) || m.plugin_name.toLowerCase().includes(q);
    });
  }, [plugins, query]);

  const grouped = useMemo(() => {
    const map = new Map<string, Metadata[]>();
    for (const m of filtered) {
      const b = bucketOf(m);
      const arr = map.get(b) ?? [];
      arr.push(m);
      map.set(b, arr);
    }
    for (const arr of map.values()) {
      arr.sort((a, b) => (a.name || a.plugin_name).localeCompare(b.name || b.plugin_name));
    }
    return map;
  }, [filtered]);

  const hasAnyResult = filtered.length > 0;

  return (
    <div className={styles.gallery}>
      {/* eslint-disable jsx-a11y/no-autofocus -- opens inside a drawer dedicated
          to picking one of 19 cards; the search field is the first focusable
          element, matching Combobox.tsx's popover-search pattern. */}
      <Input
        type="search"
        leadingIcon="search"
        placeholder="Search integrations…"
        aria-label="Search integrations"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        // 19 cards with no filter is the exact P3 finding this fixes — the
        // search field is also the first focusable element in the drawer,
        // so it (not an arbitrary card) gets the dialog's initial focus.
        autoFocus
      />
      {/* eslint-enable jsx-a11y/no-autofocus */}
      {hasAnyResult ? (
        CATEGORY_ORDER.map(({ key, label }) => {
          const items = grouped.get(key);
          if (!items || items.length === 0) return null;
          return (
            <section key={key} className={styles.group}>
              <h3 className={styles.groupTitle}>{label}</h3>
              <div className={styles.grid}>
                {items.map((m) => {
                  const brand = brandFor(m.plugin_name);
                  return (
                    <button
                      key={m.plugin_name}
                      type="button"
                      className={styles.card}
                      onClick={() => onPick(m.plugin_name)}
                    >
                      {brand ? (
                        <BrandIcon name={brand} size={24} />
                      ) : (
                        <Icon name={CATEGORY_ICON[key] ?? "plug"} size={24} />
                      )}
                      <span className={styles.cardName}>{m.name || m.plugin_name}</span>
                      {m.display_name ? (
                        <span className={styles.cardDesc}>{m.display_name}</span>
                      ) : null}
                    </button>
                  );
                })}
              </div>
            </section>
          );
        })
      ) : (
        <EmptyState
          icon="search"
          title="No integrations match"
          description={`Nothing found for "${query}". Try a different name.`}
        />
      )}
    </div>
  );
}
