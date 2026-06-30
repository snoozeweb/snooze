export type HeartbeatStatus = "ok" | "slow" | "overdue";

export type Heartbeat = {
  uid?: string;
  name: string;
  interval: number; // seconds
  grace?: number; // seconds
  max_latency?: number; // ms; 0 or absent disables latency tracking
  last_latency?: number; // ms; read-only, set by ping
  last_seen?: string; // RFC3339; set by ping
  token?: string; // read-only, server-generated
  severity?: string;
  environment?: string;
  host?: string;
  message?: string;
  enabled?: boolean;
  status?: HeartbeatStatus; // read-only, projected at read time
};
