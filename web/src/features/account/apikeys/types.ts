export type ApiKey = {
  uid: string;
  owner: string;
  owner_method?: string;
  name: string;
  key_prefix?: string;
  permissions?: string[];
  groups?: string[];
  created_at?: number;
  expires_at?: number;
  revoked_at?: number;
  /** Unix epoch (seconds). Absent (0) when the key has never been used. */
  last_used_at?: number;
  /** Lower-bound count; updated at most once per hour. */
  use_count?: number;
};

export type ApiKeyCreate = {
  name: string;
  permissions: string[];
  /** RFC3339; omit to default to the server cap. */
  expires_at?: string;
};

export type ApiKeyCreated = ApiKey & { key: string };
