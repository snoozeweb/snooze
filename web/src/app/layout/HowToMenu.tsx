import { useState } from "react";
import { useLocation } from "@tanstack/react-router";
import { Icon } from "@/shared/icons/Icon";
import { Menu, MenuContent, MenuItem, MenuTrigger } from "@/shared/ui/Menu";
import { InjectAlertsDialog } from "@/features/alerts/InjectAlertsDialog";
import { SilencingGuideDialog } from "@/features/alerts/SilencingGuideDialog";
import { SendAlertsDialog } from "@/features/notifications/SendAlertsDialog";
import { useConfigHealth } from "./useConfigHealth";
import styles from "./HowToMenu.module.css";

export function HowToMenu() {
  const [injectOpen, setInjectOpen] = useState(false);
  const [sendOpen, setSendOpen] = useState(false);
  const [silenceOpen, setSilenceOpen] = useState(false);

  const location = useLocation();
  const isAlertsPage = location.pathname.startsWith("/web/alerts");

  const { actionCount, notifCount } = useConfigHealth(isAlertsPage);

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
          {/* The first two entries are about getting alerts in and out. This
              one is about the question every operator hits on day one and the
              UI can't answer in a label: which of Close / Snooze / Shelve /
              Shelve permanently do I actually want? */}
          <MenuItem leadingIcon="moon" onSelect={() => setSilenceOpen(true)}>
            Close, snooze or shelve?
          </MenuItem>
        </MenuContent>
      </Menu>
      {isAlertsPage && actionCount === 0 ? (
        <button type="button" className={styles.setupBadge} onClick={() => setSendOpen(true)}>
          <span className={styles.setupIcon} aria-hidden="true">
            <Icon name="alert-triangle" size={12} />
          </span>
          No actions
        </button>
      ) : null}
      {isAlertsPage && notifCount === 0 ? (
        <button type="button" className={styles.setupBadge} onClick={() => setSendOpen(true)}>
          <span className={styles.setupIcon} aria-hidden="true">
            <Icon name="alert-triangle" size={12} />
          </span>
          No notifications
        </button>
      ) : null}
      <InjectAlertsDialog open={injectOpen} onOpenChange={setInjectOpen} />
      <SendAlertsDialog open={sendOpen} onOpenChange={setSendOpen} />
      <SilencingGuideDialog open={silenceOpen} onOpenChange={setSilenceOpen} />
    </>
  );
}
