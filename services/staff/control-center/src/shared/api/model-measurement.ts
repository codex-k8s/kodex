import type { ModelMeasurement } from "./generated/openapi/types.gen";

// Значения приходят из owner event, но неполная/неизвестная проекция не становится измеренным нулём.
export function validModelMeasurement(value: ModelMeasurement): boolean {
  if (
    !value ||
    typeof value !== "object" ||
    Object.keys(value).some(
      (key) =>
        ![
          "ref",
          "source",
          "phase",
          "outcome",
          "completeness",
          "durationMs",
          "ttftMs",
          "usage",
        ].includes(key),
    ) ||
    !/^(request|step)_[A-Za-z0-9_-]{8,80}$/.test(value.ref) ||
    !["NORMAL", "COMPACTION", "AUXILIARY"].includes(value.phase) ||
    !["COMPLETED", "FAILED", "CANCELLED"].includes(value.outcome) ||
    !["COMPLETE", "PARTIAL", "UNKNOWN"].includes(value.completeness)
  )
    return false;
  for (const duration of [value.durationMs, value.ttftMs])
    if (
      duration !== undefined &&
      (!Number.isSafeInteger(duration) || duration < 0 || duration > 86400000)
    )
      return false;
  if (
    value.ttftMs !== undefined &&
    (value.durationMs === undefined || value.ttftMs > value.durationMs)
  )
    return false;
  if (value.source === "HTTP_REQUEST") {
    if (!value.ref.startsWith("request_") || value.usage !== undefined)
      return false;
    if (value.completeness === "COMPLETE")
      return (
        value.durationMs !== undefined &&
        value.ttftMs !== undefined &&
        value.outcome === "COMPLETED"
      );
    return value.completeness === "UNKNOWN"
      ? value.durationMs === undefined && value.ttftMs === undefined
      : value.durationMs !== undefined;
  }
  if (
    value.source !== "NATIVE_STEP" ||
    !value.ref.startsWith("step_") ||
    value.ttftMs !== undefined ||
    value.completeness === "COMPLETE"
  )
    return false;
  const usage = value.usage;
  if (
    !usage ||
    usage.completeness !== value.completeness ||
    !["PARTIAL", "UNKNOWN"].includes(usage.completeness)
  )
    return false;
  const counters = [
    usage.totalTokens,
    usage.inputTokens,
    usage.outputTokens,
    usage.cachedInputTokens,
    usage.cacheWriteInputTokens,
    usage.reasoningOutputTokens,
    usage.modelContextWindow,
  ];
  return (
    counters.every(
      (count) =>
        Number.isSafeInteger(count) && count >= 0 && count <= 1000000000000,
    ) &&
    usage.totalTokens === usage.inputTokens + usage.outputTokens &&
    usage.cachedInputTokens + usage.cacheWriteInputTokens <=
      usage.inputTokens &&
    usage.reasoningOutputTokens <= usage.outputTokens &&
    (value.completeness !== "UNKNOWN" || usage.totalTokens === 0)
  );
}
