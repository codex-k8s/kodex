import type { IntegrationConfigurationField } from "@/shared/api/generated/openapi/types.gen";
import {
  parseConfigurationDocument,
  serializeConfigurationDocument,
} from "@/features/managed-configurations/document";
import { prepareConnectionConfiguration } from "./connection-setup";

export function parseConnectionYaml(
  source: string,
  fields: readonly IntegrationConfigurationField[],
): Record<string, string> {
  const document = parseConfigurationDocument(source, "YAML");
  const known = new Map(fields.map((field) => [field.key, field]));
  const values: Record<string, string> = {};
  for (const [key, value] of Object.entries(document)) {
    const field = known.get(key);
    if (
      !field ||
      key === "__proto__" ||
      key === "constructor" ||
      key === "prototype"
    )
      throw new Error("Unknown or protected connection configuration field");
    switch (field.valueType) {
      case "BOOLEAN":
        if (typeof value !== "boolean")
          throw new Error("Invalid configuration boolean");
        values[key] = String(value);
        break;
      case "INTEGER":
        if (!Number.isSafeInteger(value))
          throw new Error("Invalid configuration integer");
        values[key] = String(value);
        break;
      case "STRING_LIST":
        if (
          !Array.isArray(value) ||
          value.some(
            (item: unknown) => typeof item !== "string" || item.includes(","),
          )
        )
          throw new Error("Invalid configuration string list");
        values[key] = value.join(", ");
        break;
      default:
        if (typeof value !== "string")
          throw new Error("Invalid configuration string");
        values[key] = value;
    }
  }
  if (
    Object.keys(prepareConnectionConfiguration(fields, values).problems).length
  )
    throw new Error("Connection configuration does not match schema");
  return values;
}

export function connectionYaml(
  fields: readonly IntegrationConfigurationField[],
  values: Readonly<Record<string, string>>,
): string {
  const prepared = prepareConnectionConfiguration(fields, values);
  if (Object.keys(prepared.problems).length)
    throw new Error("Connection configuration does not match schema");
  return serializeConfigurationDocument(prepared.value, "YAML");
}
