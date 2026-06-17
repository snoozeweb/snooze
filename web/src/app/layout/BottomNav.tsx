import { useState } from "react";
import { Link, useLocation } from "@tanstack/react-router";
import { Icon } from "@/shared/icons/Icon";
import { useAuth } from "@/lib/auth/store";
import { hasAnyPermission } from "@/lib/auth/permissions";
import { useActiveAlertCount } from "@/features/alerts/api";
import { visibleNavItems } from "./nav-list";
import { MoreSheet } from "./MoreSheet";
import styles from "./BottomNav.module.css";

// The four primary destinations pinned to the bar (in order). Everything else
// is reachable via the More sheet.
const PRIMARY = ["/web/alerts", "/web/dashboard", "/web/snoozes", "/web/rules"];
const ALERTS_PERMS = ["ro_record", "rw_record"];

export function BottomNav() {
  const { claims } = useAuth();
  const location = useLocation();
  const currentPath = location.pathname;
  const [moreOpen, setMoreOpen] = useState(false);

  const items = visibleNavItems(claims).filter((i) => PRIMARY.includes(i.to));
  // Preserve PRIMARY order regardless of NAV_ITEMS order.
  items.sort((a, b) => PRIMARY.indexOf(a.to) - PRIMARY.indexOf(b.to));

  const canSeeAlerts = hasAnyPermission(claims, ALERTS_PERMS);
  const { data: alertCountData } = useActiveAlertCount(canSeeAlerts);
  const alertTotal = alertCountData?.meta.total ?? 0;

  return (
    <>
      <nav className={styles.bar} aria-label="Primary">
        {items.map((item) => {
          const active = currentPath === item.to || currentPath.startsWith(item.to + "/");
          const showBadge = item.to === "/web/alerts" && alertTotal > 0;
          return (
            <Link
              key={item.to}
              to={item.to}
              className={`${styles.tab} ${active ? styles.tabActive : ""}`}
              {...(active ? { "aria-current": "page" as const } : {})}
            >
              <span className={styles.tabIcon}>
                <Icon name={item.icon} size={20} />
                {showBadge ? (
                  <span className={styles.badge} aria-label={`${alertTotal} active alerts`}>
                    {alertTotal > 99 ? "99+" : alertTotal}
                  </span>
                ) : null}
              </span>
              <span className={styles.tabLabel}>{item.label}</span>
            </Link>
          );
        })}
        <button
          type="button"
          className={`${styles.tab} ${moreOpen ? styles.tabActive : ""}`}
          aria-haspopup="dialog"
          aria-expanded={moreOpen}
          onClick={() => setMoreOpen(true)}
        >
          <span className={styles.tabIcon}>
            <Icon name="more-horizontal" size={20} />
          </span>
          <span className={styles.tabLabel}>More</span>
        </button>
      </nav>
      <MoreSheet open={moreOpen} onOpenChange={setMoreOpen} />
    </>
  );
}
