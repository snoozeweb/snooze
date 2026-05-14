import { defineResource } from "@/lib/api/resource";
import type { User } from "./types";

export const Users = defineResource<User>("user");
