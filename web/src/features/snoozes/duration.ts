// parseDuration moved to lib/format/duration.ts so shared UI primitives (e.g.
// DurationInput) can use the same grammar without depending on a feature module.
// Re-exported here to keep existing feature imports working.
export { parseDuration } from "@/lib/format/duration";
