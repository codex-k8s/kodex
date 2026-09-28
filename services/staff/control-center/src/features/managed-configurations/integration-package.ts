import schema from "@/shared/api/generated/integration-package/schema.json";
import validate from "@/shared/api/generated/integration-package/validate";

export interface PackageFieldSchema {
  $ref?: string;
  type?: string;
  const?: string | boolean | number | null;
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

export interface PackageDiagnosticDetail {
  path: string[];
  keyword: string;
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
  if (validate(normalizeLegacyOpenAPIInputFields(value))) return [];
  // Только пути схемы и закрытые имена правил, без пользовательских значений.
  return [
    ...new Set(
      (validate.errors ?? []).map(
        (error) => `${error.schemaPath}: ${error.keyword}`,
      ),
    ),
  ];
}

export function packageDiagnosticDetails(
  value: unknown,
): PackageDiagnosticDetail[] {
  if (validate(normalizeLegacyOpenAPIInputFields(value))) return [];
  const details = new Map<string, PackageDiagnosticDetail>();
  for (const error of validate.errors ?? []) {
    const path = error.instancePath
      .split("/")
      .slice(1)
      .map((part) => part.replaceAll("~1", "/").replaceAll("~0", "~"));
    if (error.keyword === "required") {
      const params = error.params as Record<string, unknown>;
      const missingProperty = params.missingProperty;
      if (
        typeof missingProperty === "string" &&
        /^[A-Za-z][A-Za-z0-9_-]{0,80}$/.test(missingProperty)
      )
        path.push(missingProperty);
    }
    const detail = { path, keyword: error.keyword };
    details.set(`${path.join(".")}:${error.keyword}`, detail);
  }
  return [...details.values()];
}

function normalizeLegacyOpenAPIInputFields(value: unknown): unknown {
  if (!value || typeof value !== "object" || Array.isArray(value)) return value;
  const document = value as Record<string, unknown>;
  const spec = document.spec;
  if (!spec || typeof spec !== "object" || Array.isArray(spec)) return value;
  const packageSpec = spec as Record<string, unknown>;
  if (
    packageSpec.adapter !== "OPENAPI_MCP" ||
    !Array.isArray(packageSpec.capabilities)
  )
    return value;
  // Старые импортированные ревизии сериализовали пустой Go slice как null.
  // Для диагностики это пустой список; сохранённый документ не меняем.
  return {
    ...document,
    spec: {
      ...packageSpec,
      capabilities: packageSpec.capabilities.map((item: unknown) => {
        if (!item || typeof item !== "object" || Array.isArray(item))
          return item;
        const capability = item as Record<string, unknown>;
        return capability.inputFields === null
          ? { ...capability, inputFields: [] }
          : item;
      }),
    },
  };
}
