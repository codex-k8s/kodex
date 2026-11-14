import type {
  IntegrationDefinition,
  IntegrationConfigurationField,
} from "@/shared/api/generated/openapi/types.gen";

// Выбор конкретных репозиториев выполняется по наблюдениям собственного ключа в «Моих подключениях».
export function githubScopeForm(
  definition: IntegrationDefinition | undefined,
): readonly IntegrationConfigurationField[] {
  const fields = definition?.configurationFields ?? [];
  if (!definition?.capabilities.some((c) => c.resourceKind === "GITHUB_SCOPE"))
    return fields;
  return fields
    .filter(
      (field) => field.key !== "repositories" && field.key !== "scope_kind",
    )
    .map((field) =>
      field.key === "service_identity"
        ? {
            ...field,
            label: "Служебная учётная запись GitHub (только для общего ключа)",
          }
        : field,
    );
}

// Создание начинает с явного all-accessible режима; PAT добавляется только через собственную привязку.
export function isPersonalGitHubScope(
  definition: IntegrationDefinition | undefined,
): boolean {
  return !!definition?.capabilities.some(
    (c) => c.resourceKind === "GITHUB_SCOPE",
  );
}
