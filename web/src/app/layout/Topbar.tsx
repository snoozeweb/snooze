import { useNavigate } from "@tanstack/react-router";
import { Icon } from "@/shared/icons/Icon";
import { IconButton } from "@/shared/ui/IconButton";
import { Kbd } from "@/shared/ui/Kbd";
import { Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger } from "@/shared/ui/Menu";
import { useTheme } from "@/shared/hooks/useTheme";
import { useAuth } from "@/lib/auth/store";
import { HowToMenu } from "./HowToMenu";
import type { Breadcrumb } from "./breadcrumb";
import styles from "./Topbar.module.css";

export type TopbarProps = {
  breadcrumb?: Breadcrumb;
  onOpenPalette: () => void;
};

function UserMenu() {
  const { claims, logout } = useAuth();
  const navigate = useNavigate();
  const username = claims?.sub ?? "anonymous";
  const tenant = claims?.tenant_id || "default";

  return (
    <Menu>
      <MenuTrigger>
        <IconButton icon="users" label={`Signed in as ${username}`} />
      </MenuTrigger>
      <MenuContent>
        <MenuItem leadingIcon="sliders" onSelect={() => void navigate({ to: "/web/profile" })}>
          Profile · {username}
        </MenuItem>
        <MenuItem leadingIcon="briefcase" disabled>
          Org: {tenant}
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
      <div className={styles.left}>
        {breadcrumb ? (
          <span className={styles.breadcrumb}>
            {breadcrumb.group ? (
              <>
                <span className={styles.breadcrumbGroup}>{breadcrumb.group}</span>
                <span className={styles.breadcrumbSep} aria-hidden="true">
                  {" "}
                  /{" "}
                </span>
              </>
            ) : null}
            <span className={styles.breadcrumbLabel}>{breadcrumb.label}</span>
          </span>
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
        <HowToMenu />
        <span className={styles.sep} aria-hidden="true" />
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
