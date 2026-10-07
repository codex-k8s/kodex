import type { IntegrationConfigurationField } from "@/shared/api/generated/openapi/types.gen";

const fieldTypeKeys = {
  TEXT: "integrationsRedesign.fieldTypes.TEXT",
  URL: "integrationsRedesign.fieldTypes.URL",
  STRING_LIST: "integrationsRedesign.fieldTypes.STRING_LIST",
  INTEGER: "integrationsRedesign.fieldTypes.INTEGER",
  BOOLEAN: "integrationsRedesign.fieldTypes.BOOLEAN",
} as const satisfies Record<IntegrationConfigurationField["valueType"], string>;

export function integrationFieldTypeKey(
  valueType: IntegrationConfigurationField["valueType"],
): string {
  return fieldTypeKeys[valueType];
}
