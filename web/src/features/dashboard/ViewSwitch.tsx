// The dashboard's two views, switched from the title row.
//
// Not a tab strip: tabs sit above the content they swap and read as sections
// of one page, which is exactly how the analyses list ended up invisible as
// the seventh tab of a card in the bottom-right corner. This is a page-level
// switch — same segmented-control recipe as the time-range presets two
// centimetres to its right, so the two read as one control family, with amber
// marking only the segment that is on.
import type { IconName } from "@/shared/icons/icon-names";
import { Icon } from "@/shared/icons/Icon";
import styles from "./ViewSwitch.module.css";

/** Which of the dashboard's two views is on screen. Round-trips as `?view=`. */
export type DashboardView = "overview" | "analyses";

const VIEWS: ReadonlyArray<{ id: DashboardView; label: string; icon: IconName }> = [
  { id: "overview", label: "Overview", icon: "gauge" },
  { id: "analyses", label: "Analyses", icon: "file-text" },
];

export type ViewSwitchProps = {
  value: DashboardView;
  onChange: (next: DashboardView) => void;
};

export function ViewSwitch({ value, onChange }: ViewSwitchProps) {
  // `group` + `aria-pressed`, not `radiogroup` + `aria-checked`: the latter
  // conveys the mutual exclusivity better, but it also brings roving-tabindex
  // arrow-key semantics a radiogroup is expected to have, and the page's other
  // segmented control (TimeRangePicker) is a plain row of aria-pressed buttons.
  // One inconsistent control is worse than one imprecise role; if that picker
  // ever becomes a radiogroup, this follows it.
  return (
    <div className={styles.bar} role="group" aria-label="Dashboard view">
      {VIEWS.map((v) => (
        <button
          key={v.id}
          type="button"
          className={styles.segment}
          data-active={value === v.id}
          aria-pressed={value === v.id}
          onClick={() => onChange(v.id)}
        >
          <Icon name={v.icon} size={14} />
          {v.label}
        </button>
      ))}
    </div>
  );
}
