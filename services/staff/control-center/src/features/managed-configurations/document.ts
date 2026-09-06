import { dump, JSON_SCHEMA, load } from "js-yaml";

export function parseConfigurationDocument(
  source: string,
  format: "JSON" | "YAML",
): Record<string, unknown> {
  if (new TextEncoder().encode(source).length > 256 * 1024)
    throw new Error("Configuration content is too large");
  const value: unknown =
    format === "JSON"
      ? JSON.parse(source)
      : load(source, {
          schema: JSON_SCHEMA,
          json: false,
          onWarning: () => {
            throw new Error("Invalid YAML configuration");
          },
        });
  const seen = new Set<object>();
  let count = 0;
  function validate(item: unknown, depth: number): void {
    count += 1;
    if (depth > 32 || count > 20_000)
      throw new Error("Configuration document is too complex");
    if (typeof item === "number" && !Number.isFinite(item))
      throw new Error("Invalid configuration number");
    if (item && typeof item === "object") {
      if (seen.has(item))
        throw new Error("Configuration aliases are unsupported");
      seen.add(item);
      for (const child of Object.values(item)) validate(child, depth + 1);
    }
  }
  validate(value, 0);
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error("Configuration document must be an object");
  return value as Record<string, unknown>;
}

export function serializeConfigurationDocument(
  value: Record<string, unknown>,
  format: "JSON" | "YAML",
): string {
  return format === "JSON"
    ? JSON.stringify(value, null, 2)
    : dump(value, { schema: JSON_SCHEMA, noRefs: true, lineWidth: -1 });
}

export function normalizedConfigurationDocument(
  source: string,
  format: "JSON" | "YAML",
): string {
  function sort(value: unknown): unknown {
    if (Array.isArray(value)) return value.map(sort);
    if (value && typeof value === "object")
      return Object.fromEntries(
        Object.entries(value)
          .sort(([left], [right]) => (left < right ? -1 : left > right ? 1 : 0))
          .map(([key, item]) => [key, sort(item)]),
      );
    return value;
  }
  return JSON.stringify(
    sort(parseConfigurationDocument(source, format)),
    null,
    2,
  );
}
