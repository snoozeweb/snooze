export type User = {
  uid?: string;
  name: string;
  type?: "local" | "ldap";
  roles?: string[];
  comment?: string;
  /** Only sent on create. */
  password?: string;
};
