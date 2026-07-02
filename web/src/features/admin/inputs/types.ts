import type { InjectionFamily } from "@/features/alerts/injectionGuide";

/** One row from GET /api/v1/inputs. Mirrors the OpenAPI `InputActivity`. */
export type InputActivity = { source: string; last_epoch: number; count: number };

/** Response envelope from GET /api/v1/inputs. */
export type InputsResponse = { data: InputActivity[] };

/** A row rendered in the Inputs table. */
export type InputRow = {
  id: string;
  name: string;
  family: InjectionFamily | "other";
  docSlug?: string;
  lastEpoch?: number;
  count?: number;
  /** REST's source is caller-defined → render "—" not "never". */
  restNoActivity?: boolean;
  /** Catalogue rows get a Setup deep-link; "other" rows don't. */
  catalogue: boolean;
};
