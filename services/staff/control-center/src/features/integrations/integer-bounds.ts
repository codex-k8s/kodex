import type { IntegrationConfigurationField } from "@/shared/api/generated/openapi/types.gen";

const minimumInt64 = -9223372036854775808n;
const maximumInt64 = 9223372036854775807n;
const maximumSafeInteger = BigInt(Number.MAX_SAFE_INTEGER);

export type IntegerBounds =
  | { valid: true; minimum?: bigint; maximum?: bigint }
  | { valid: false };

function bound(value: unknown): bigint | undefined {
  if (value === undefined) return undefined;
  if (typeof value === "number") {
    if (!Number.isSafeInteger(value)) throw new Error("Invalid integer bound");
    return BigInt(value);
  }
  if (
    typeof value !== "string" ||
    value.length > 20 ||
    !/^(?:0|[1-9]\d*|-[1-9]\d*)$/.test(value)
  )
    throw new Error("Invalid integer bound");
  const result = BigInt(value);
  if (result < minimumInt64 || result > maximumInt64)
    throw new Error("Integer bound exceeds int64");
  return result;
}

export function readIntegerBounds(
  field: Pick<IntegrationConfigurationField, "minimum" | "maximum">,
): IntegerBounds {
  try {
    const minimum = bound(field.minimum);
    const maximum = bound(field.maximum);
    if (minimum !== undefined && maximum !== undefined && minimum > maximum)
      return { valid: false };
    return { valid: true, minimum, maximum };
  } catch {
    return { valid: false };
  }
}

export function integrationInteger(
  value: string,
  bounds: IntegerBounds,
): number | undefined {
  if (!bounds.valid || !/^-?\d+$/.test(value)) return undefined;
  const integer = BigInt(value);
  if (
    integer < -maximumSafeInteger ||
    integer > maximumSafeInteger ||
    (bounds.minimum !== undefined && integer < bounds.minimum) ||
    (bounds.maximum !== undefined && integer > bounds.maximum)
  )
    return undefined;
  // Payload сохраняет safe JSON number; большие decimal strings относятся только к metadata.
  return Number(integer);
}
