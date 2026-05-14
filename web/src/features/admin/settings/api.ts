import { defineResource } from "@/lib/api/resource";
import type { Setting } from "./types";

export const Settings = defineResource<Setting>("settings");
