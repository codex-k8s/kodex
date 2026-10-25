import type { TokenUsage } from "@/shared/api/generated/openapi/types.gen";

// Legacy без поля остаётся UNKNOWN; повреждённое качество не превращается в допустимый subtotal.
export function usageCompleteness(
  usage: TokenUsage | undefined,
): "UNKNOWN" | "PARTIAL" | "COMPLETE" {
  const value: unknown = usage?.completeness;
  if (value === undefined) return "UNKNOWN";
  if (value === "UNKNOWN" || value === "PARTIAL" || value === "COMPLETE")
    return value;
  throw new Error("Token usage completeness is invalid");
}
