import { defineResource } from "@/lib/api/resource";
import type { Environment } from "./types";

export const Environments = defineResource<Environment>("environment");
