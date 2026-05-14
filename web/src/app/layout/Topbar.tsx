import { Icon } from "@/shared/icons/Icon";
import { IconButton } from "@/shared/ui/IconButton";
import { Kbd } from "@/shared/ui/Kbd";
import { useTheme } from "@/shared/hooks/useTheme";
import styles from "./Topbar.module.css";

export type TopbarProps = {
  breadcrumb?: string;
  onOpenPalette: () => void;
};

export function Topbar({ breadcrumb, onOpenPalette }: TopbarProps) {
  const { theme, toggleTheme } = useTheme();
  return (
    <header className={styles.topbar}>
      <div style={{ display: "inline-flex", alignItems: "center" }}>
        <span className={styles.brand}>
          <Icon name="bell-off" size={20} />
          <span>Snooze</span>
        </span>
        {breadcrumb ? (
          <>
            <span className={styles.divider} />
            <span className={styles.breadcrumb}>{breadcrumb}</span>
          </>
        ) : null}
      </div>
      <div className={styles.right}>
        <button
          type="button"
          className={styles.paletteOpener}
          onClick={onOpenPalette}
          aria-label="Open command palette"
        >
          <Icon name="search" size={12} />
          <span>Search…</span>
          <Kbd>⌘K</Kbd>
        </button>
        <IconButton
          icon={theme === "dark" ? "sun" : "moon"}
          label={theme === "dark" ? "Switch to light theme" : "Switch to dark theme"}
          onClick={toggleTheme}
        />
        <IconButton icon="users" label="Account" />
      </div>
    </header>
  );
}
