import schema from "../../../../../../contracts/integrations/v1/integration-package.schema.json";
import validate from "@/shared/api/generated/integration-package/validate";

export interface PackageFieldSchema {
  $ref?: string;
  type?: string;
  const?: string;
  enum?: string[];
  properties?: Record<string, PackageFieldSchema>;
  required?: string[];
  items?: PackageFieldSchema;
  minItems?: number;
  maxItems?: number;
  minLength?: number;
  maxLength?: number;
  minimum?: number;
  maximum?: number;
  pattern?: string;
}
export const packageSchema: PackageFieldSchema = schema;
export function resolvePackageField(
  field: PackageFieldSchema,
): PackageFieldSchema {
  if (!field.$ref) return field;
  const key = field.$ref.replace(/^#\/\$defs\//, "");
  const definitions: Record<string, PackageFieldSchema> = schema.$defs;
  const resolved = definitions[key];
  if (!resolved)
    throw new Error("Unsupported integration package schema reference");
  return resolved;
}
export function emptyPackageField(field: PackageFieldSchema): unknown {
  const resolved = resolvePackageField(field);
  if (resolved.const !== undefined) return resolved.const;
  if (resolved.type === "object")
    return Object.fromEntries(
      Object.entries(resolved.properties ?? {})
        .filter(([key]) => resolved.required?.includes(key))
        .map(([key, child]) => [key, emptyPackageField(child)]),
    );
  if (resolved.type === "array") return [];
  if (resolved.type === "boolean") return false;
  return "";
}
export function packageDiagnostics(value: unknown): string[] {
  if (validate(value)) return [];
  // Только пути схемы и закрытые имена правил, без пользовательских значений.
  return [
    ...new Set(
      (validate.errors ?? []).map(
        (error) => `${error.schemaPath}: ${error.keyword}`,
      ),
    ),
  ];
}
