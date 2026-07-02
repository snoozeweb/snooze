import { useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import * as RD from "@radix-ui/react-dialog";
import { Icon } from "@/shared/icons/Icon";
import { useTheme } from "@/shared/hooks/useTheme";
import { useAuth } from "@/lib/auth/store";
import { InjectAlertsDialog } from "@/features/alerts/InjectAlertsDialog";
import { SendAlertsDialog } from "@/features/notifications/SendAlertsDialog";
import { visibleNavItems } from "./nav-list";
import { GROUP_LABELS, type NavGroup } from "./nav-items";
import { useConfigHealth } from "./useConfigHealth";
import styles from "./MoreSheet.module.css";

// Items NOT pinned to the bottom bar (everything except the four primaries).
const PRIMARY = ["/web/alerts", "/web/dashboard", "/web/snoozes", "/web/rules"];
const GROUPS: NavGroup[] = ["operate", "configure", "admin"];

export function MoreSheet({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { claims, logout } = useAuth();
  const { theme, toggleTheme } = useTheme();
  const navigate = useNavigate();
  const username = claims?.sub ?? "anonymous";
  const overflow = visibleNavItems(claims).filter((i) => !PRIMARY.includes(i.to));

  const [injectOpen, setInjectOpen] = useState(false);
  const [sendOpen, setSendOpen] = useState(false);
  // How-to and its config-health warnings were desktop-only; surface them here
  // so a mobile operator can open the setup dialogs and still sees the
  // unconfigured-pipeline signal. Only fetch while the sheet is open.
  const { actionCount, notifCount } = useConfigHealth(open);

  const close = () => onOpenChange(false);

  return (
    <RD.Root open={open} onOpenChange={onOpenChange}>
      <RD.Portal>
        <RD.Overlay className={styles.overlay} />
        <RD.Content className={styles.sheet}>
          <RD.Title className={styles.title}>Menu</RD.Title>
          <nav className={styles.nav}>
            {/* Same Operate/Configure/Admin grouping the sidebar uses, so the
                mental model survives on mobile instead of one flat list. */}
            {GROUPS.map((group) => {
              const items = overflow.filter((i) => i.group === group);
              if (items.length === 0) return null;
              return (
                <div className={styles.group} key={group}>
                  <span className={styles.groupLabel}>{GROUP_LABELS[group]}</span>
                  {items.map((item) => (
                    <Link key={item.to} to={item.to} className={styles.item} onClick={close}>
                      <Icon name={item.icon} size={16} />
                      <span>{item.label}</span>
                    </Link>
                  ))}
                </div>
              );
            })}
          </nav>

          <div className={styles.group}>
            <span className={styles.groupLabel}>How to</span>
            <button type="button" className={styles.item} onClick={() => setInjectOpen(true)}>
              <Icon name="download" size={16} />
              <span>Receive alerts</span>
            </button>
            <button type="button" className={styles.item} onClick={() => setSendOpen(true)}>
              <Icon name="upload" size={16} />
              <span>Send alerts</span>
            </button>
            {actionCount === 0 ? (
              <button
                type="button"
                className={`${styles.item} ${styles.danger}`}
                onClick={() => setSendOpen(true)}
              >
                <Icon name="alert-triangle" size={16} />
                <span>No actions configured</span>
              </button>
            ) : null}
            {notifCount === 0 ? (
              <button
                type="button"
                className={`${styles.item} ${styles.danger}`}
                onClick={() => setSendOpen(true)}
              >
                <Icon name="alert-triangle" size={16} />
                <span>No notifications configured</span>
              </button>
            ) : null}
          </div>

          <div className={styles.footer}>
            <button
              type="button"
              className={styles.item}
              onClick={() => {
                void navigate({ to: "/web/profile" });
                close();
              }}
            >
              <Icon name="sliders" size={16} />
              <span>Profile · {username}</span>
            </button>
            <button type="button" className={styles.item} onClick={toggleTheme}>
              <Icon name={theme === "dark" ? "sun" : "moon"} size={16} />
              <span>{theme === "dark" ? "Light theme" : "Dark theme"}</span>
            </button>
            {claims?.tenant_id ? <span className={styles.org}>org:{claims.tenant_id}</span> : null}
            <button
              type="button"
              className={`${styles.item} ${styles.danger}`}
              onClick={() => {
                logout();
                void navigate({ to: "/web/login" });
                close();
              }}
            >
              <Icon name="lock" size={16} />
              <span>Log out</span>
            </button>
          </div>
        </RD.Content>
      </RD.Portal>
      <InjectAlertsDialog open={injectOpen} onOpenChange={setInjectOpen} />
      <SendAlertsDialog open={sendOpen} onOpenChange={setSendOpen} />
    </RD.Root>
  );
}
