import { useEffect, useState } from "react";
import { Input } from "@/shared/ui/Input";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/shared/ui/Select";
import type { AlertSeverity, AlertState } from "./types";
import styles from "./Filters.module.css";

export type AlertFilters = {
  state?: AlertState;
  severity?: AlertSeverity;
  environment?: string;
  search?: string;
};

export type AlertsFiltersProps = {
  value: AlertFilters;
  onChange: (next: AlertFilters) => void;
};

const STATE_OPTIONS = [
  { value: "__all__", label: "All states" },
  { value: "open", label: "Open" },
  { value: "ack", label: "Acknowledged" },
  { value: "close", label: "Closed" },
  { value: "shelved", label: "Shelved" },
] as const;

const SEVERITY_OPTIONS = [
  { value: "__all__", label: "All severities" },
  { value: "critical", label: "Critical" },
  { value: "error", label: "Error" },
  { value: "warning", label: "Warning" },
  { value: "info", label: "Info" },
] as const;

export function AlertsFilters({ value, onChange }: AlertsFiltersProps) {
  const [envLocal, setEnvLocal] = useState(value.environment ?? "");
  const [searchLocal, setSearchLocal] = useState(value.search ?? "");

  // Keep local input in sync if the parent resets the filter (e.g. via URL).
  useEffect(() => {
    setEnvLocal(value.environment ?? "");
  }, [value.environment]);
  useEffect(() => {
    setSearchLocal(value.search ?? "");
  }, [value.search]);

  // Debounce free-text inputs.
  useEffect(() => {
    const t = setTimeout(() => {
      if (envLocal !== (value.environment ?? "")) {
        const next: AlertFilters = { ...value };
        if (envLocal) next.environment = envLocal;
        else delete next.environment;
        onChange(next);
      }
    }, 300);
    return () => clearTimeout(t);
  }, [envLocal, value, onChange]);

  useEffect(() => {
    const t = setTimeout(() => {
      if (searchLocal !== (value.search ?? "")) {
        const next: AlertFilters = { ...value };
        if (searchLocal) next.search = searchLocal;
        else delete next.search;
        onChange(next);
      }
    }, 300);
    return () => clearTimeout(t);
  }, [searchLocal, value, onChange]);

  function handleState(v: string) {
    const next: AlertFilters = { ...value };
    if (v === "__all__") delete next.state;
    // STATE_OPTIONS values are exactly AlertState literals; runtime-validated by the options list.
    else next.state = v as AlertState; // narrowing from string to union
    onChange(next);
  }

  function handleSeverity(v: string) {
    const next: AlertFilters = { ...value };
    if (v === "__all__") delete next.severity;
    else next.severity = v;
    onChange(next);
  }

  return (
    <div className={styles.bar}>
      <div className={styles.selectRow}>
        <Select
          {...(value.state !== undefined ? { value: value.state } : {})}
          onValueChange={handleState}
        >
          <SelectTrigger placeholder="State" />
          <SelectContent>
            {STATE_OPTIONS.map((o) => (
              <SelectItem key={o.value} value={o.value}>
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          {...(value.severity !== undefined ? { value: value.severity } : {})}
          onValueChange={handleSeverity}
        >
          <SelectTrigger placeholder="Severity" />
          <SelectContent>
            {SEVERITY_OPTIONS.map((o) => (
              <SelectItem key={o.value} value={o.value}>
                {o.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Input
          placeholder="Environment"
          value={envLocal}
          onChange={(e) => setEnvLocal(e.target.value)}
        />
      </div>
      <div className={styles.searchWrap}>
        <Input
          placeholder="Search host, message, …"
          leadingIcon="search"
          value={searchLocal}
          onChange={(e) => setSearchLocal(e.target.value)}
        />
      </div>
    </div>
  );
}
