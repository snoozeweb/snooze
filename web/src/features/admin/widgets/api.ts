import { defineResource } from "@/lib/api/resource";
import type { Widget } from "./types";

export const Widgets = defineResource<Widget>("widget");
