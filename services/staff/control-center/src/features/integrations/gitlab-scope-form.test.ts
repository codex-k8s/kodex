import { it, expect } from "vitest";
import { gitLabScopeForm, changeGitLabScopeField } from "./gitlab-scope-form";
import type { IntegrationDefinition } from "@/shared/api/generated/openapi/types.gen";
const definition = {
  capabilities: [{ resourceKind: "GITLAB_SCOPE" }],
  configurationFields: [
    "base_url",
    "scope_kind",
    "project_path",
    "group_path",
    "include_subgroups",
  ].map((key) => ({
    key,
    required: key === "base_url" || key === "scope_kind",
  })),
} as IntegrationDefinition;
it("GROUP требует группу и оставляет подгруппы явным выбором", () => {
  const fields = gitLabScopeForm(definition, { scope_kind: "GROUP" });
  expect(fields.map((f) => f.key)).toEqual([
    "base_url",
    "scope_kind",
    "group_path",
    "include_subgroups",
  ]);
  expect(fields.find((f) => f.key === "group_path")?.required).toBe(true);
});
it("смена INSTANCE очищает только прежние селекторы", () => {
  const values = {
    scope_kind: "PROJECT",
    project_path: "team/project",
    base_url: "https://example.invalid",
    extra: "keep",
  };
  changeGitLabScopeField(definition, values, "scope_kind", "INSTANCE");
  expect(values).toEqual({
    scope_kind: "INSTANCE",
    base_url: "https://example.invalid",
    extra: "keep",
  });
  expect(gitLabScopeForm(definition, values).map((f) => f.key)).toEqual([
    "base_url",
    "scope_kind",
  ]);
});
it("старое определение не получает новую семантику", () => {
  const legacy = {
    ...definition,
    capabilities: [{ resourceKind: "GITLAB_PROJECT" }],
  } as IntegrationDefinition;
  expect(gitLabScopeForm(legacy, { scope_kind: "GROUP" })).toBe(
    legacy.configurationFields,
  );
});
