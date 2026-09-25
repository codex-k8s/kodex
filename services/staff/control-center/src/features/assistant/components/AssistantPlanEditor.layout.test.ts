import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./AssistantPlanEditor.vue", import.meta.url),
  "utf8",
);

describe("AssistantPlanEditor layout", () => {
  it("показывает понятные поля проекта и сотрудника без редактирования authority", () => {
    expect(source).toContain('v-if="friendlyPlanOperationType(operation)"');
    expect(source).toContain('v-if="!friendlyPlanOperationType(operation)"');
    expect(source).toContain("fieldValue(operation, 'name')");
    expect(source).toContain("fieldValue(operation, 'purpose')");
    expect(source).toContain("fieldValue(operation, 'instructions')");
    expect(source).toContain("capabilityChecked(operation, key)");
    expect(source).toContain("assistant.planEditor.agentNextSteps");
  });

  it("даёт выбрать продвинутый образ для черновика среды без ручного ref", () => {
    expect(source).toContain("CREATE_RUNTIME_ENVIRONMENT_DRAFT");
    expect(source).toContain("runtime.searchPromotedRoleImagePage");
    expect(source).toContain("<AsyncEntityPicker");
    expect(source).toContain("setImageArtifact(operation, $event)");
    expect(source).toContain("assistant.planEditor.environmentDraftNextSteps");
  });

  it("показывает сотрудника и каталог окружений для рецепта образа", () => {
    expect(source).toContain("CREATE_ROLE_IMAGE_RECIPE");
    expect(source).toContain("roleImageAgentNames[");
    expect(source).toMatch(/fieldValue\(operation,\s*"agentRef"\)/);
    expect(source).toContain("loadRoleEnvironmentCatalog");
    expect(source).toContain("assistant.planEditor.roleImageAgentFixed");
    expect(source).toContain("assistant.planEditor.roleImageNextSteps");
  });

  it("проверяет схему подключения и не показывает ввод секрета в плане", () => {
    expect(source).toContain("CREATE_INTEGRATION_CONNECTION");
    expect(source).toContain("loadExactIntegrationDefinition");
    expect(source).toContain("prepareConnectionConfiguration");
    expect(source).toContain("connectionCredentialNextSteps");
    expect(source).not.toContain('v-model="credentialValue"');
  });

  it("показывает публикацию интеграции без свободного редактирования ревизии", () => {
    expect(source).toContain(
      "operation.value.type === 'PUBLISH_INTEGRATION_DEFINITION'",
    );
    expect(source).toContain(
      "assistant.planEditor.integrationPublicationBoundary",
    );
    expect(source).toContain('operationParameter(operation, "revisionRef")');
    expect(source).toContain('v-if="!friendlyPlanOperationType(operation)"');
  });

  it("не применяет проверенную старую ревизию при несохранённых изменениях", () => {
    expect(source).toContain("draftMatchesSavedPlan.value");
    expect(source).toContain("exactRevisionValidated.value");
  });

  it("повторно проверяет дружелюбные формы после обновления плана сервером", () => {
    expect(source).toContain("draftGeneration.value += 1");
    expect(source).toContain(
      ':key="`${draftGeneration}:${operation.value.ref}`"',
    );
    expect(source).toContain("runFormValidity.value = {}");
  });

  it("не закрывает план пока применяется операция", () => {
    expect(source).toMatch(
      /assistant\.planEditor\.back'[\s\S]*?:disabled="busy"/,
    );
  });

  it("показывает тип, действие и authority результата без скрытых изменений", () => {
    expect(source).toContain("{{ operation.value.type }}");
    expect(source).toContain("{{ operation.value.action }}");
    expect(source).toContain(
      'operation.value.permitted ? "common.yes" : "common.no"',
    );
    expect(source).toContain("operation.value.target.kind");
    expect(source).toContain("operation.value.target.name");
    expect(source).toContain("operation.value.target.ref");
    expect(source).toContain("operation.value.target.version");
    expect(source).toContain("operation.value.expectedVersion");
  });

  it("показывает выбранный объект отдельным текстом, а не только value поля", () => {
    expect(source).toContain('class="assistant-plan-target__summary"');
    expect(source).toContain("operationTargetLabel(operation.value.target)");
    expect(source).toContain("operationActionLabel(operation.value.action)");
  });

  it("даёт открыть parameters, before и after в расширенном редакторе", () => {
    expect(source).toContain("kind: 'PARAMETERS'");
    expect(source).toContain("kind: 'BEFORE'");
    expect(source).toContain("kind: 'AFTER'");
    expect(source).toContain("<AssistantCodeEditorModal");
  });

  it("не разрешает редактировать уже применённый или отклонённый план", () => {
    expect(source).toContain('["APPLIED", "REJECTED"].includes');
    expect(source).toContain(':disabled="!editable"');
  });

  it("возвращает в диалог для доработки и предупреждает о несохранённой форме", () => {
    expect(source).toContain("!draftMatchesSavedPlan.value");
    expect(source).toContain("assistant.planEditor.unsavedRevisionConfirm");
    expect(source).toContain('emit("requestChanges")');
    expect(source).toContain('v-if="canRequestChanges && editable"');
  });

  it("укладывает target и переход состояния в одну колонку на mobile", () => {
    const mobile = source.slice(source.indexOf("@media (max-width: 640px)"));

    expect(mobile).toMatch(
      /\.assistant-plan-target__summary\s*\{[\s\S]*?grid-template-columns:\s*1fr/,
    );
    expect(mobile).toMatch(
      /\.assistant-plan-transition\s*\{[\s\S]*?grid-template-columns:\s*1fr/,
    );
  });
});
