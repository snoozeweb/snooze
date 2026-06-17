import { Link, useNavigate } from "@tanstack/react-router";
import * as RD from "@radix-ui/react-dialog";
import { Icon } from "@/shared/icons/Icon";
import { useTheme } from "@/shared/hooks/useTheme";
import { useAuth } from "@/lib/auth/store";
import { visibleNavItems } from "./nav-list";
import styles from "./MoreSheet.module.css";

// Items NOT pinned to the bottom bar (everything except the four primaries).
const PRIMARY = ["/web/alerts", "/web/dashboard", "/web/snoozes", "/web/rules"];

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

  const close = () => onOpenChange(false);

  return (
    <RD.Root open={open} onOpenChange={onOpenChange}>
      <RD.Portal>
        <RD.Overlay className={styles.overlay} />
        <RD.Content className={styles.sheet}>
          <RD.Title className={styles.title}>Menu</RD.Title>
          <nav className={styles.nav}>
            {overflow.map((item) => (
              <Link key={item.to} to={item.to} className={styles.item} onClick={close}>
                <Icon name={item.icon} size={16} />
                <span>{item.label}</span>
              </Link>
            ))}
          </nav>
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
    </RD.Root>
  );
}
