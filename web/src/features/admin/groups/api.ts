import { defineResource } from "@/lib/api/resource";
import type { Group } from "./types";

export const Groups = defineResource<Group>("group");
