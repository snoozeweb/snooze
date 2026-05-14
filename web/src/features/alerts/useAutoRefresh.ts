import { useCallback, useState } from "react";

const KEY = "alerts.autoRefresh";

function readEnabled(): boolean {
  try {
    const v = localStorage.getItem(KEY);
    return v !== "false";
  } catch {
    return true;
  }
}

export type UseAutoRefreshResult = {
  enabled: boolean;
  intervalMs: number | undefined;
  setEnabled: (v: boolean) => void;
};

export function useAutoRefresh(defaultIntervalMs: number): UseAutoRefreshResult {
  const [enabled, setEnabledState] = useState<boolean>(() => readEnabled());

  const setEnabled = useCallback((v: boolean) => {
    try {
      localStorage.setItem(KEY, v ? "true" : "false");
    } catch {
      // Quota or private-mode; in-memory state still updates.
    }
    setEnabledState(v);
  }, []);

  return {
    enabled,
    intervalMs: enabled ? defaultIntervalMs : undefined,
    setEnabled,
  };
}
