import { defineResource } from "@/lib/api/resource";
import type { Role } from "./types";

export const Roles = defineResource<Role>("role");
