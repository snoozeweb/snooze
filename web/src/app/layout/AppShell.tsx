import { useCallback, useState } from "react";
import { Outlet, useMatches, useNavigate } from "@tanstack/react-router";
import { Sidebar } from "./Sidebar";
import { Topbar } from "./Topbar";
import { BottomNav } from "./BottomNav";
import { CommandPalette } from "./CommandPalette";
import { useShortcut } from "@/shared/hooks/useShortcut";
import { useIsMobileShell } from "@/shared/hooks/useIsMobileShell";
import { pickBreadcrumb } from "./breadcrumb";
import styles from "./AppShell.module.css";

export function AppShell() {
  const [paletteOpen, setPaletteOpen] = useState(false);
  const navigate = useNavigate();
  const isMobile = useIsMobileShell();

  const open = useCallback(() => setPaletteOpen(true), []);

  useShortcut("mod+k", open);
  // Direct-nav shortcuts wired statically so the hook-call count stays stable.
  // Cast `to` as `string` because dynamic feature routes are not individually
  // typed as string literals in the router tree.
  useShortcut("mod+1", () => void navigate({ to: "/web/alerts" as string }));
  useShortcut("mod+2", () => void navigate({ to: "/web/dashboard" as string }));
  useShortcut("mod+3", () => void navigate({ to: "/web/snoozes" as string }));
  useShortcut("mod+4", () => void navigate({ to: "/web/rules" as string }));
  useShortcut("mod+5", () => void navigate({ to: "/web/notifications" as string }));

  const matches = useMatches();
  const breadcrumb = pickBreadcrumb(matches);

  return (
    <div className={styles.shell}>
      <a href="#main-content" className={styles.skipLink}>
        Skip to content
      </a>
      <div className={styles.body}>
        {!isMobile ? <Sidebar /> : null}
        <div className={styles.content}>
          <Topbar {...(breadcrumb ? { breadcrumb } : {})} onOpenPalette={open} mobile={isMobile} />
          <main id="main-content" tabIndex={-1} className={styles.main}>
            <Outlet />
          </main>
          {isMobile ? <BottomNav /> : null}
        </div>
      </div>
      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
    </div>
  );
}
