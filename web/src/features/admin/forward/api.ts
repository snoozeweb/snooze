import { defineResource } from "@/lib/api/resource";
import type { ForwardDestination } from "./types";

export const Forward = defineResource<ForwardDestination>("forward");
