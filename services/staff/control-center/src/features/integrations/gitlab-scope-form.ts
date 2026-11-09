import type {
  IntegrationDefinition,
  IntegrationConfigurationField,
} from "@/shared/api/generated/openapi/types.gen";

// Профиль определяется объявленной областью контракта; конкретных hosts, групп и репозиториев здесь нет.
export function gitLabScopeForm(
  definition: IntegrationDefinition | undefined,
  values: Readonly<Record<string, string>>,
): readonly IntegrationConfigurationField[] {
  const fields = definition?.configurationFields ?? [];
  if (!definition?.capabilities.some((c) => c.resourceKind === "GITLAB_SCOPE"))
    return fields;
  const kind = values.scope_kind;
  if (kind !== "PROJECT" && kind !== "GROUP" && kind !== "INSTANCE")
    return fields;
  return fields
    .filter((field) => field.key !== "project_path" || kind === "PROJECT")
    .filter(
      (field) =>
        !(field.key === "group_path" || field.key === "include_subgroups") ||
        kind === "GROUP",
    )
    .map((field) =>
      field.key === "project_path" || field.key === "group_path"
        ? { ...field, required: true }
        : field,
    );
}

// Меняем только поля выбора ресурса при явном переключении режима, не credentials или произвольные metadata.
export function changeGitLabScopeField(
  definition: IntegrationDefinition | undefined,
  values: Record<string, string>,
  key: string,
  value: string,
): void {
  values[key] = value;
  if (
    key !== "scope_kind" ||
    !definition?.capabilities.some((c) => c.resourceKind === "GITLAB_SCOPE")
  )
    return;
  if (value === "GROUP") {
    delete values.project_path;
    return;
  }
  if (value === "PROJECT") {
    delete values.group_path;
    delete values.include_subgroups;
    return;
  }
  if (value === "INSTANCE") {
    delete values.project_path;
    delete values.group_path;
    delete values.include_subgroups;
  }
}
