import { Link, useLocation } from "@tanstack/react-router";
import { Icon } from "@/shared/icons/Icon";
import { Kbd } from "@/shared/ui/Kbd";
import { GROUP_LABELS, NAV_ITEMS, type NavGroup } from "./nav-items";
import { useAuth } from "@/lib/auth/store";
import { hasAnyPermission } from "@/lib/auth/permissions";
import styles from "./Sidebar.module.css";

const GROUPS: NavGroup[] = ["operate", "configure", "admin"];

export function Sidebar() {
  const location = useLocation();
  const currentPath = location.pathname;
  const { claims } = useAuth();

  return (
    <aside className={styles.sidebar} aria-label="Primary navigation">
      <nav className={styles.nav}>
        {GROUPS.map((group) => {
          const items = NAV_ITEMS.filter((i) => {
            if (i.group !== group) return false;
            if (!i.permissions || i.permissions.length === 0) return true;
            return hasAnyPermission(claims, i.permissions);
          });
          if (items.length === 0) return null;
          return (
            <div className={styles.group} key={group}>
              <span className={styles.groupLabel}>{GROUP_LABELS[group]}</span>
              {items.map((item) => {
                const active = currentPath === item.to || currentPath.startsWith(item.to + "/");
                return (
                  <Link
                    key={item.to}
                    to={item.to}
                    className={`${styles.item} ${active ? styles.itemActive : ""}`}
                    {...(active ? { "aria-current": "page" as const } : {})}
                  >
                    <Icon name={item.icon} size={16} />
                    <span className={styles.label}>{item.label}</span>
                    {item.shortcut ? <Kbd>{shortcutLabel(item.shortcut)}</Kbd> : null}
                  </Link>
                );
              })}
            </div>
          );
        })}
      </nav>
      <div className={styles.footer}>
        <span className={styles.footerAvatar} aria-hidden="true">
          O
        </span>
        <span>operator</span>
      </div>
    </aside>
  );
}

function shortcutLabel(combo: string): string {
  return combo.replace(
    /mod/i,
    /mac/i.test(typeof navigator !== "undefined" ? navigator.platform : "") ? "⌘" : "Ctrl",
  );
}
