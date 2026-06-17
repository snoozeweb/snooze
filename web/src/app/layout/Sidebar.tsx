import { Link, useLocation, useNavigate } from "@tanstack/react-router";
import { Icon } from "@/shared/icons/Icon";
import { Kbd } from "@/shared/ui/Kbd";
import { Logo } from "@/shared/ui/Logo";
import { Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger } from "@/shared/ui/Menu";
import { GROUP_LABELS, type NavGroup } from "./nav-items";
import { visibleNavItems } from "./nav-list";
import { useAuth } from "@/lib/auth/store";
import { hasAnyPermission } from "@/lib/auth/permissions";
import { useActiveAlertCount } from "@/features/alerts/api";
import styles from "./Sidebar.module.css";

const GROUPS: NavGroup[] = ["operate", "configure", "admin"];

// Permission set that gates the Alerts nav item — mirrors NAV_ITEMS definition.
const ALERTS_PERMS = ["ro_record", "rw_record"];

export function Sidebar() {
  const location = useLocation();
  const currentPath = location.pathname;
  const { claims, logout } = useAuth();
  const navigate = useNavigate();
  const username = claims?.sub ?? "anonymous";

  // Live alert-count badge: only fetch when the user can see the Alerts item.
  const canSeeAlerts = hasAnyPermission(claims, ALERTS_PERMS);
  const { data: alertCountData } = useActiveAlertCount(canSeeAlerts);
  const alertTotal = alertCountData?.meta.total ?? 0;

  // Permission-filtered nav, centralized in nav-list so the Sidebar, BottomNav
  // and MoreSheet all apply the same RequirePlatformPerm-aware rule.
  const permitted = visibleNavItems(claims);

  return (
    <aside className={styles.sidebar} aria-label="Primary navigation">
      <Link to="/web/alerts" className={styles.brand} aria-label="Snooze home">
        <Logo />
      </Link>
      <nav className={styles.nav}>
        {GROUPS.map((group) => {
          const items = permitted.filter((i) => i.group === group);
          if (items.length === 0) return null;
          return (
            <div className={styles.group} key={group}>
              <span className={styles.groupLabel}>{GROUP_LABELS[group]}</span>
              {items.map((item) => {
                const active = currentPath === item.to || currentPath.startsWith(item.to + "/");
                const isAlertsItem = item.to === "/web/alerts";
                const showBadge = isAlertsItem && alertTotal > 0;
                const badgeLabel = alertTotal > 999 ? "999+" : String(alertTotal);
                return (
                  <Link
                    key={item.to}
                    to={item.to}
                    className={`${styles.item} ${active ? styles.itemActive : ""}`}
                    {...(active ? { "aria-current": "page" as const } : {})}
                  >
                    <Icon name={item.icon} size={16} />
                    <span className={styles.label}>{item.label}</span>
                    {showBadge ? (
                      <span className={styles.badge} aria-label={`${alertTotal} active alerts`}>
                        {badgeLabel}
                      </span>
                    ) : null}
                    {item.shortcut ? <Kbd>{shortcutLabel(item.shortcut)}</Kbd> : null}
                  </Link>
                );
              })}
            </div>
          );
        })}
      </nav>
      <div className={styles.footer}>
        <Menu>
          <MenuTrigger>
            <button
              type="button"
              className={styles.footerUser}
              aria-label={`Account menu — signed in as ${username}`}
            >
              <span className={styles.footerAvatar} aria-hidden="true">
                {username.charAt(0).toUpperCase()}
              </span>
              <span className={styles.footerUsername}>{username}</span>
              <span className={styles.footerChevron} aria-hidden="true">
                <Icon name="chevron-up" size={14} />
              </span>
            </button>
          </MenuTrigger>
          <MenuContent side="top" align="start">
            <MenuItem leadingIcon="sliders" onSelect={() => void navigate({ to: "/web/profile" })}>
              Profile
            </MenuItem>
            <MenuSeparator />
            <MenuItem
              leadingIcon="lock"
              danger
              onSelect={() => {
                logout();
                void navigate({ to: "/web/login" });
              }}
            >
              Log out
            </MenuItem>
          </MenuContent>
        </Menu>
        {claims?.tenant_id ? (
          <span className={styles.footerTenant} aria-label="Organization">
            org:{claims.tenant_id}
          </span>
        ) : null}
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
