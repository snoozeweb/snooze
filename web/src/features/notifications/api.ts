import { defineResource } from "@/lib/api/resource";
import type { Action, Notification } from "./types";

export const Notifications = defineResource<Notification>("notification");
export const Actions = defineResource<Action>("action");
