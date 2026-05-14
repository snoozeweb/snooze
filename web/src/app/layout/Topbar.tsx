import { useNavigate } from "@tanstack/react-router";
import { Icon } from "@/shared/icons/Icon";
import { IconButton } from "@/shared/ui/IconButton";
import { Kbd } from "@/shared/ui/Kbd";
import { Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger } from "@/shared/ui/Menu";
import { useTheme } from "@/shared/hooks/useTheme";
import { useAuth } from "@/lib/auth/store";
import styles from "./Topbar.module.css";

export type TopbarProps = {
  breadcrumb?: string;
  onOpenPalette: () => void;
};

function UserMenu() {
  const { claims, logout } = useAuth();
  const navigate = useNavigate();
  const username = claims?.sub ?? "anonymous";

  return (
    <Menu>
      <MenuTrigger>
        <IconButton icon="users" label={`Signed in as ${username}`} />
      </MenuTrigger>
      <MenuContent>
        <MenuItem leadingIcon="sliders" onSelect={() => void navigate({ to: "/web/profile" })}>
          Profile · {username}
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
  );
}

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
        <UserMenu />
      </div>
    </header>
  );
}
