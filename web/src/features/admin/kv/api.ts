import { defineResource } from "@/lib/api/resource";
import type { KV } from "./types";

export const KVs = defineResource<KV>("kv");
