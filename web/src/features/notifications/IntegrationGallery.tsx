import { useMemo } from "react";
import { Icon } from "@/shared/icons/Icon";
import { BrandIcon } from "@/shared/icons/BrandIcon";
import { brandFor } from "@/shared/icons/brand-names";
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

// Branded notifiers (Slack, Teams, PagerDuty, …) render their brand glyph from
// web/public/brands.svg via brandFor(); everything else falls back to the
// bucket's monochrome glyph from the icon sprite (web/public/icons.svg).
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
  const grouped = useMemo(() => {
    const map = new Map<string, Metadata[]>();
    for (const m of plugins) {
      const b = bucketOf(m);
      const arr = map.get(b) ?? [];
      arr.push(m);
      map.set(b, arr);
    }
    for (const arr of map.values()) {
      arr.sort((a, b) => (a.name || a.plugin_name).localeCompare(b.name || b.plugin_name));
    }
    return map;
  }, [plugins]);

  return (
    <div className={styles.gallery}>
      {CATEGORY_ORDER.map(({ key, label }) => {
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
      })}
    </div>
  );
}
