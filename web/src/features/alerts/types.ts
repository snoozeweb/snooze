import type { components } from "@/lib/api/types.gen";

/** Single alert/record row. Trailing underscore avoids the JS built-in Record collision. */
export type Record_ = components["schemas"]["Record"];

export type AlertState = "" | "open" | "ack" | "esc" | "close" | "shelved";

// eslint-disable-next-line @typescript-eslint/no-redundant-type-constituents
export type AlertSeverity = "info" | "warning" | "error" | "critical" | string;
