import { defineResource } from "@/lib/api/resource";
import type { Snooze } from "./types";

export const Snoozes = defineResource<Snooze>("snooze");
