import type { Metadata } from "@/shared/forms/types";
import styles from "./IntegrationModeChooser.module.css";

export type IntegrationModeChooserProps = {
  plugin: Metadata;
  onUseBuiltin: () => void;
  onBack: () => void;
};

export function IntegrationModeChooser({ plugin, onUseBuiltin, onBack }: IntegrationModeChooserProps) {
  const daemon = plugin.daemon;
  return (
    <div className={styles.chooser}>
      <p className={styles.intro}>{plugin.name} can deliver in two ways:</p>
      <div className={styles.options}>
        <button type="button" className={styles.option} onClick={onUseBuiltin}>
          <span className={styles.optionTitle}>Built-in</span>
          <span className={styles.optionDesc}>Quick — Snooze posts directly. No extra service to run.</span>
        </button>
        {daemon ? (
          <a className={styles.option} href={daemon.doc_url} target="_blank" rel="noreferrer noopener">
            <span className={styles.optionTitle}>{`Advanced · ${daemon.name} ↗`}</span>
            <span className={styles.optionDesc}>{daemon.blurb}</span>
          </a>
        ) : null}
      </div>
      <button type="button" className={styles.back} onClick={onBack}>← Back</button>
    </div>
  );
}
