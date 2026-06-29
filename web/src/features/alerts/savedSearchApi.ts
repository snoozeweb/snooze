import { defineResource } from "@/lib/api/resource";

/**
 * A saved search is a human label (`name`) paired with a raw Snooze
 * condition-DSL string (`query`). `owner` and `tenant_id` are stamped
 * server-side from the JWT, so they are read-only from the client's view
 * (present on reads, omitted on writes).
 */
export type SavedSearch = {
  uid?: string;
  name: string;
  query: string;
  owner?: string;
};

export const SavedSearches = defineResource<SavedSearch>("savedsearch");
