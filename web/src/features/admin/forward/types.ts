import type { Condition } from "@/lib/condition/types";

export type ForwardAuth = {
  type?: "" | "bearer" | "basic" | "apikey";
  token?: string;
  username?: string;
  password?: string;
  api_key?: string;
  header?: string;
};

export type ForwardDestination = {
  uid?: string;
  name: string;
  enabled?: boolean;
  endpoint?: string;
  condition?: Condition;
  event_classes?: string[];
  auth?: ForwardAuth;
  tls_insecure?: boolean;
  timeout?: string; // Go duration string, e.g. "30s"
};
