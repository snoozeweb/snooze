export type ApiKey = {
  uid: string;
  owner: string;
  owner_method?: string;
  name: string;
  key_prefix?: string;
  permissions?: string[];
  groups?: string[];
  created_at?: number;
  /** Epoch seconds of the last successful auth with this key; absent until the
   *  key is first used. The server throttles the write to at most once/hour, so
   *  it lags real usage by up to an hour (use_count is likewise a lower bound). */
  last_used_at?: number;
  use_count?: number;
  expires_at?: number;
  revoked_at?: number;
};

export type ApiKeyCreate = {
  name: string;
  permissions: string[];
  /** RFC3339; omit to default to the server cap. */
  expires_at?: string;
};

export type ApiKeyCreated = ApiKey & { key: string };
