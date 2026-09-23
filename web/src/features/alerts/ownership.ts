// Reading the untyped ownership keys off a record, and the "since …" wording
// the Owner cell and its tooltip share. No React here (see Owner.tsx).
import { formatRelativeTime } from "@/lib/format/time";
import type { Record_ } from "./types";

/** The record's ownership, normalised: empty strings for "nobody". */
export function recordOwnership(r: Record_): {
  owner: string;
  ownerMethod: string;
  since: number;
  previous: string;
  previousMethod: string;
} {
  const owner = typeof r.owner === "string" ? r.owner : "";
  return {
    owner,
    ownerMethod: typeof r.owner_method === "string" ? r.owner_method : "",
    since: owner !== "" && typeof r.owner_since === "number" ? r.owner_since : 0,
    // A ghost only while unowned: taking an alert resets previous_owner
    // server-side, but a row written mid-transition must not show two faces.
    previous: owner === "" && typeof r.previous_owner === "string" ? r.previous_owner : "",
    previousMethod: typeof r.previous_owner_method === "string" ? r.previous_owner_method : "",
  };
}

/** "since 5m ago" / "since just now" — the relative-time helper's wording. */
export function sincePhrase(epoch: number): string {
  if (!epoch) return "";
  const rel = formatRelativeTime(epoch);
  return rel === "just now" ? "since just now" : `since ${rel} ago`;
}
