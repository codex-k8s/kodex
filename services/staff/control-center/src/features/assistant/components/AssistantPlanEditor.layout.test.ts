import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./AssistantPlanEditor.vue", import.meta.url),
  "utf8",
);

describe("AssistantPlanEditor layout", () => {
  it("сначала показывает штатную форму и оставляет пояснения доступными по запросу", () => {
    expect(source).toContain("const showPlanDetails = ref(false)");
    expect(source).toContain("showPlanDetails.value = false");
    expect(source).toContain('v-if="hasFriendlyOperations"');
    expect(source).toContain(
      'v-show="!allOperationsFriendly || showPlanDetails"',
    );
    expect(source).toContain(
      'v-show="!friendlyPlanOperationType(operation) || showPlanDetails"',
    );
    expect(source).toContain("assistant.planEditor.showDetails");
    expect(source).toContain("assistant.planEditor.hideDetails");
    expect(source).toContain("<ProjectFormFields");
  });

  it("показывает понятные поля проекта и сотрудника без редактирования authority", () => {
    expect(source).toContain('v-if="friendlyPlanOperationType(operation)"');
    expect(source).toContain('v-if="!friendlyPlanOperationType(operation)"');
    expect(source).toContain("fieldValue(operation, 'name')");
    expect(source).toContain("fieldValue(operation, 'purpose')");
    expect(source).toContain("fieldValue(operation, 'instructions')");
    expect(source).toContain("capabilityChecked(operation, key)");
    expect(source).toContain("assistant.planEditor.agentNextSteps");
    expect(source).toContain("<AgentFormFields");
    expect(source).toContain(
      "agentFormValidity.value[operation.value.ref] === true",
    );
  });

  it("повторно использует ручную форму профиля для изменения сотрудника", () => {
    const profile = readFileSync(
      new URL("../../agents/detail/AgentProfileFields.vue", import.meta.url),
      "utf8",
    );
    const manual = readFileSync(
      new URL("../../agents/detail/AgentProfilePanel.vue", import.meta.url),
      "utf8",
    );
    expect(manual).toContain("<AgentProfileFields");
    expect(source).toContain("<AgentProfileFields");
    expect(source).toContain(
      "agentProfileValidity.value[operation.value.ref] === true",
    );
    expect(source).toContain("operation.value.type === 'UPDATE_AGENT'");
    expect(profile).toContain('maxlength="120"');
    expect(profile).toContain('maxlength="1000"');
  });

  it("показывает Markdown-редактор черновика и оставляет публикацию отдельной", () => {
    expect(source).toContain(
      "operation.value.type === 'CREATE_INSTRUCTION_DRAFT'",
    );
    expect(source).toContain("<TemplateSourceField");
    expect(source).toContain("assistant.planEditor.instructionDraftNextSteps");
    const card = readFileSync(
      new URL("./AssistantInstructionDraftCard.vue", import.meta.url),
      "utf8",
    );
    expect(card).toContain("draftInstructions?.content");
    expect(card).toContain("assistantForm: '1'");
  });

  it("даёт выбрать продвинутый образ для черновика среды без ручного ref", () => {
    expect(source).toContain("CREATE_RUNTIME_ENVIRONMENT_DRAFT");
    expect(source).toContain("runtime.searchPromotedRoleImagePage");
    expect(source).toContain("<AsyncEntityPicker");
    expect(source).toContain("setImageArtifact(operation, $event)");
    expect(source).toContain("assistant.planEditor.environmentDraftNextSteps");
    expect(source).toContain("<AssistantEnvironmentFieldsForm");
    expect(source).toContain(
      "environmentFieldsValidity.value[operation.value.ref] === true",
    );
    expect(source).toContain("!environmentFieldsTouched.value");
  });

  it("при изменении среды выбирает образ штатным поиском, а не сырым ref", () => {
    const revision = readFileSync(
      new URL("./AssistantEnvironmentRevisionForm.vue", import.meta.url),
      "utf8",
    );
    expect(source).toContain("PREPARE_RUNTIME_ENVIRONMENT_REVISION");
    expect(source).toMatch(
      /<AssistantEnvironmentRevisionForm[\s\S]*?<AsyncEntityPicker/,
    );
    expect(source).toContain("runtime.choosePromotedImage");
    expect(revision).not.toContain(
      "@input=\"changeText('imageArtifactRef', $event)\"",
    );
  });

  it("показывает общий редактор инструментов и очищает старый выбор при смене образа", () => {
    const manual = readFileSync(
      new URL(
        "../../../pages/RuntimeEnvironmentEditorPage.vue",
        import.meta.url,
      ),
      "utf8",
    );
    const editor = readFileSync(
      new URL(
        "../../runtime/RuntimeEnvironmentToolsEditor.vue",
        import.meta.url,
      ),
      "utf8",
    );
    expect(source).toContain("<AssistantEnvironmentToolsForm");
    expect(source).toContain(
      'updateOperationParameter(operation, "tools", [])',
    );
    expect(source).toContain(
      "environmentToolsValidity.value[operation.value.ref] === true",
    );
    expect(manual).toContain("<RuntimeEnvironmentToolsEditor");
    expect(editor).toContain("const target = event.target");
  });

  it("открывает защищённую форму секрета и после применения плана", () => {
    expect(source).toContain("parseAssistantSecretSuggestions");
    expect(source).toContain("emit('prepareSecret', suggestion)");
    expect(source).toContain("plan.state === 'REJECTED'");
    expect(source).not.toContain('v-model="secretValue"');
  });

  it("показывает сотрудника и каталог окружений для рецепта образа", () => {
    expect(source).toContain("CREATE_ROLE_IMAGE_RECIPE");
    expect(source).toContain("roleImageAgentNames[");
    expect(source).toMatch(/fieldValue\(operation,\s*"agentRef"\)/);
    expect(source).toContain("loadRoleEnvironmentCatalog");
    expect(source).toContain("assistant.planEditor.roleImageAgentFixed");
    expect(source).toContain("assistant.planEditor.roleImageNextSteps");
    expect(source).toContain("<RoleImageDockerfileEditor");
    expect(source).toContain(
      "validateDockerfile(fieldValue(operation, 'dockerfile'))",
    );
    expect(source).toContain("roleImageReady(operation)");
    expect(source).toContain(
      "v-if=\"editable || fieldValue(operation, 'dockerfile')\"",
    );
    expect(source).toContain("assistant.planEditor.roleImageHistoricalSource");
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
