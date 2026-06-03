import { useState } from "react";
import { useLocation } from "@tanstack/react-router";
import { Icon } from "@/shared/icons/Icon";
import { Menu, MenuContent, MenuItem, MenuTrigger } from "@/shared/ui/Menu";
import { InjectAlertsDialog } from "@/features/alerts/InjectAlertsDialog";
import { SendAlertsDialog } from "@/features/notifications/SendAlertsDialog";
import { Actions, Notifications } from "@/features/notifications/api";
import styles from "./HowToMenu.module.css";

export function HowToMenu() {
  const [injectOpen, setInjectOpen] = useState(false);
  const [sendOpen, setSendOpen] = useState(false);

  const location = useLocation();
  const isAlertsPage = location.pathname.startsWith("/web/alerts");

  const actionList = Actions.useList({ limit: 1 }, { enabled: isAlertsPage });
  const notifList = Notifications.useList({ limit: 1 }, { enabled: isAlertsPage });

  const actionCount = actionList.data?.meta.total ?? null;
  const notifCount = notifList.data?.meta.total ?? null;

  return (
    <>
      <Menu>
        <MenuTrigger>
          <button type="button" className={styles.howToBtn} aria-label="How to">
            How to <Icon name="chevron-down" size={12} />
          </button>
        </MenuTrigger>
        <MenuContent align="end">
          <MenuItem leadingIcon="download" onSelect={() => setInjectOpen(true)}>
            Receive alerts
          </MenuItem>
          <MenuItem leadingIcon="upload" onSelect={() => setSendOpen(true)}>
            Send alerts
          </MenuItem>
        </MenuContent>
      </Menu>
      {isAlertsPage && actionCount === 0 ? (
        <button type="button" className={styles.dangerBadge} onClick={() => setSendOpen(true)}>
          <span className={styles.dot} aria-hidden="true" />
          No actions
        </button>
      ) : null}
      {isAlertsPage && notifCount === 0 ? (
        <button type="button" className={styles.dangerBadge} onClick={() => setSendOpen(true)}>
          <span className={styles.dot} aria-hidden="true" />
          No notifications
        </button>
      ) : null}
      <InjectAlertsDialog open={injectOpen} onOpenChange={setInjectOpen} />
      <SendAlertsDialog open={sendOpen} onOpenChange={setSendOpen} />
    </>
  );
}
