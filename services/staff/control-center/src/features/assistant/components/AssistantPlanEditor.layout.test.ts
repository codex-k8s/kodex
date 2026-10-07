import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./AssistantPlanEditor.vue", import.meta.url),
  "utf8",
);

describe("AssistantPlanEditor layout", () => {
  it("homogeneous PROJECT batch переносит только сверенный общий контекст над списком, детали остаются mounted", () => {
    expect(source).toContain("commonProjectGrantBatchContext(");
    expect(source).toContain("projectGrantContexts.value = {}");
    expect(source).toMatch(
      /:aria-labelledby="\s*commonProjectGrantContext \? grantContextId : undefined\s*"/,
    );
    expect(source).toContain(
      ':shared-context="Boolean(commonProjectGrantContext)"',
    );
    expect(source).toContain(
      '@context="projectGrantContexts[operation.value.ref] = $event"',
    );
    expect(source.indexOf('v-if="commonProjectGrantContext"')).toBeLessThan(
      source.indexOf('class="assistant-plan-operations"'),
    );
    const form = readFileSync(
      new URL(
        "./AssistantProjectIntegrationGrantPlanForm.vue",
        import.meta.url,
      ),
      "utf8",
    );
    expect(form).toContain('v-if="!sharedContext || !batchContext"');
    expect(form).toContain('v-show="!compact || expanded"');
    expect(form).toContain("grid-template-columns: 16px minmax(0, 1fr)");
    expect(form).toContain('flush: "sync"');
    expect(form).not.toContain('v-if="expanded"');
  });
  it("mobile оставляет Validate/Apply видимыми, а второстепенные действия раскрывает штатным popover", () => {
    const footer = source.slice(
      source.indexOf('<footer class="assistant-plan-editor__footer">'),
    );
    expect(footer).toContain(
      'class="assistant-plan-editor__secondary-actions"',
    );
    expect(footer).toContain("<DismissiblePopover");
    expect(footer).toContain('v-model:open="footerActionsOpen"');
    expect(footer).toContain(":ariaLabel=\"$t('common.actions')\"");
    expect(footer).toContain('v-bind="attrs"');
    expect(footer).toContain('@click="toggle"');
    expect(footer).toMatch(/close\(\);\s*requestChanges\(\);/);
    expect(footer).toMatch(/close\(\);\s*emit\('reject'\);/);
    expect(footer).toMatch(/close\(\);\s*save\(\);/);
    const primary = footer.slice(
      footer.indexOf('class="assistant-plan-editor__primary-actions"'),
    );
    expect(primary).toContain('v-if="canValidate"');
    expect(primary).toContain('v-if="canApply"');
    expect(primary).toContain("@click=\"emit('validate')\"");
    expect(primary).toContain("@click=\"emit('apply')\"");
    expect(source).toContain("footerActionsOpen.value = false");
    expect(source).toMatch(/@media \(max-width: 600px\)/);
    expect(source).toMatch(
      /\.assistant-plan-editor__secondary-actions\s*{\s*display: none;/,
    );
    expect(source).toMatch(
      /\.assistant-plan-editor__primary-actions\s*{\s*display: contents;/,
    );
    expect(source).toContain(
      "grid-template-columns: minmax(0, 1fr) minmax(0, 1fr)",
    );
    expect(source).toContain("min-height: 44px");
  });
  it("PROJECT batch использует собственный exact-profile bundle, не ordinary/SYSTEM fallback", () => {
    expect(source).toContain(':read-bundle="projectGrantReadBundle"');
    expect(source).toContain(':compact="compactProjectGrantBatch"');
    expect(source).toContain("projectGrantReadBundle.value.close()");
    expect(source).toContain("createProjectIntegrationGrantReadBundle()");
    const form = readFileSync(
      new URL(
        "./AssistantProjectIntegrationGrantPlanForm.vue",
        import.meta.url,
      ),
      "utf8",
    );
    expect(form).toContain('v-show="!compact || expanded"');
    expect(form).not.toContain('v-if="expanded"');
    expect(form).not.toContain("createIntegrationGrantReadBundle");
  });
  it("сворачивает batch grant поля, не размонтирует проверку и делит scoped bundle одной ревизии", () => {
    expect(source).toContain(':read-bundle="grantReadBundle"');
    expect(source).toContain(':compact="compactGrantBatch"');
    expect(source).toContain("grantReadBundle.value.close()");
    expect(source).toContain(
      "grantReadBundle.value = createIntegrationGrantReadBundle()",
    );
    expect(source).toContain("assistant-plan-operations--grant-batch");
    expect(source).toContain("max-height: 480px");
    const form = readFileSync(
      new URL("./AssistantIntegrationGrantPlanForm.vue", import.meta.url),
      "utf8",
    );
    expect(form).toContain('v-show="!compact || expanded"');
    expect(form).toContain(':aria-expanded="expanded"');
    expect(form).not.toContain('v-if="expanded"');
  });
  it("INVALID snapshot conflict предлагает помощнику новый план, а не автоматический rebase", () => {
    expect(source).toContain('props.plan.state === "INVALID"');
    expect(source).toContain('problem === "snapshot-conflict"');
    expect(source).toContain("assistant.planEditor.grantRefreshPlan");
    expect(source).toContain("assistant.planEditor.grantRefreshHint");
    expect(source).toContain('emit("requestChanges")');
    expect(source).not.toContain("refreshStale");
  });
  it("после Apply открывает точный авторитетный черновик прямо из modal", () => {
    expect(source).toContain("<AssistantEnvironmentDraftCard");
    expect(source).toContain('v-if="draftContinuationPlan"');
    expect(source).toContain(':plan="draftContinuationPlan"');
    expect(source).toContain("@navigate=\"emit('close')\"");
    expect(source).toContain("receipt.planRef !== props.plan.ref");
    expect(source).toContain("receipt.planRevision !== props.plan.revision");
    expect(source.indexOf("<AssistantEnvironmentDraftCard")).toBeGreaterThan(
      source.indexOf('class="assistant-plan-receipt"'),
    );
    const card = readFileSync(
      new URL("./AssistantEnvironmentDraftCard.vue", import.meta.url),
      "utf8",
    );
    expect(card).toContain("readEnvironmentDraft(");
    expect(card).toContain("query: { draftRef: current.ref }");
    expect(card).not.toContain("createEnvironmentDraft");
  });
  it("показывает PROJECT preparation как ORG подключение с readonly owner и защищённым продолжением", () => {
    expect(source).toContain("projectAssistantConnectionPlanOwner");
    expect(source).toContain("projectConnectionReady(operation)");
    expect(source).toContain("assistant.planEditor.projectConnectionBoundary");
    expect(source).toContain('v-if="allowRawOperationEdit(operation)"');
    expect(source).toMatch(
      /operation\.value\.type !==\s*"PREPARE_PROJECT_ASSISTANT_INTEGRATION_CONNECTION"/,
    );
    expect(source).not.toContain("setField(operation, 'projectAssistantRef'");
    const workspace = readFileSync(
      new URL("./AssistantWorkspace.vue", import.meta.url),
      "utf8",
    );
    expect(workspace).toContain(
      "PREPARE_PROJECT_ASSISTANT_INTEGRATION_CONNECTION",
    );
  });
  it("показывает SYSTEM grant как закрытую owner-confirmed форму без прямой публикации", () => {
    expect(source).toContain("<AssistantSystemIntegrationGrantPlanForm");
    expect(
      /operation\.value\.type !==\s*"CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT"/.test(
        source,
      ),
    ).toBe(true);
    const form = readFileSync(
      new URL("./AssistantSystemIntegrationGrantPlanForm.vue", import.meta.url),
      "utf8",
    );
    expect(form).toContain("systemIntegrationGrantPlanOwner");
    expect(form).toContain("systemIntegrationGrantPlanCandidate");
    expect(form).toContain("readSystemGrantCandidates");
    expect(form).not.toContain("saveSystemGrant");
    expect(form).not.toContain('changed("connectionRef"');
    expect(form).not.toContain('changed("capabilityKey"');
    expect(form).not.toContain("getAgent");
    expect(form).not.toContain("getWorkflow");
  });
  it("локализует помощника только в закрытом helper context и объясняет уже сохранённый черновик", () => {
    expect(source).toContain("isProjectAssistantHelper(operation)");
    expect(source).toContain("assistant.planEditor.helperInstructions");
    expect(source).toContain("assistant.planEditor.agentInstructions");
    expect(source).toContain(
      "assistant.planEditor.helperInstructionDraftPrepared",
    );
    expect(source).toContain(
      "assistant.planEditor.helperInstructionDraftNextSteps",
    );
    expect(source).toContain(':helper-applied="');
    const card = readFileSync(
      new URL("./AssistantInstructionDraftCard.vue", import.meta.url),
      "utf8",
    );
    expect(card).toContain("assistantProjectHelperScope(");
    expect(card).toContain("assistant.instructionDraft.helperTitle");
    expect(card).toContain("assistant.instructionDraft.title");
    const revision = readFileSync(
      new URL("./AssistantEnvironmentRevisionForm.vue", import.meta.url),
      "utf8",
    );
    expect(revision).toContain(
      "assistant.planEditor.helperEnvironmentDraftPrepared",
    );
    expect(revision).toContain('v-if="!helperApplied"');
  });
  it("не предлагает редактировать уже применённый или readonly план", () => {
    expect(source).toMatch(
      /<p(?=[^>]*v-if="editable")(?=[^>]*class="assistant-plan-friendly__hint")[^>]*>\s*{{ \$t\("assistant\.planEditor\.friendlyHint"\) }}/,
    );
  });
  it("свернутая grant строка скрывает повторный title/hint/snapshot, но сохраняет выбор и раскрытие", () => {
    expect(source).toContain("assistant-plan-operation--compact-grant");
    expect(source).toContain("grid-template-columns: 24px minmax(0, 1fr) 28px");
    expect(source).toContain(
      'v-show="grantOperationDetailsVisible(operation)"',
    );
    expect(source).toContain(
      '@expanded="grantExpanded[operation.value.ref] = $event"',
    );
    expect(source).toContain('v-model="operation.value.selected"');
    expect(source).toContain('class="assistant-plan-friendly__snapshot"');
    expect(source).not.toContain('v-if="grantExpanded');
  });
  it("показывает модель помощника штатной формой без прямой публикации", () => {
    expect(source).toContain("<AssistantRuntimeConfigurationPlanForm");
    expect(source).toContain(
      "assistantRuntimeValidity.value[operation.value.ref] === true",
    );
    const form = readFileSync(
      new URL("./AssistantRuntimeConfigurationPlanForm.vue", import.meta.url),
      "utf8",
    );
    expect(form).toContain("ProviderModelSelector");
    expect(form).toContain("ProviderAccountSelector");
    expect(form).not.toContain("saveAgentRuntime");
    expect(form).not.toContain("publishAgentRuntimeConfiguration");
    expect(form).toContain(
      "assistantRuntimePlanOwner(input, platform.bootstrap?.organizationRef)",
    );
  });
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
    expect(source).toContain('class="assistant-plan-operation__title"');
    expect(source).toMatch(
      /operation\.value\.title\s*\|\|\s*operationTargetLabel\(operation\.value\.target\)/,
    );
  });

  it("показывает понятные поля проекта и сотрудника без редактирования authority", () => {
    expect(source).toContain('v-if="friendlyPlanOperationType(operation)"');
    expect(source).toContain('v-if="allowRawOperationEdit(operation)"');
    expect(source).toContain("fieldValue(operation, 'name')");
    expect(source).toContain("fieldValue(operation, 'purpose')");
    expect(source).toContain("fieldValue(operation, 'instructions')");
    expect(source).toContain("capabilityChecked(operation, key)");
    expect(source).toContain("assistant.planEditor.agentNextSteps");
    expect(source).toContain("<AgentFormFields");
    expect(source).toContain("allow-default-runtime");
    expect(source).toContain(
      "agentFormValidity.value[operation.value.ref] === true",
    );
    const agentFields = readFileSync(
      new URL("../../platform/AgentFormFields.vue", import.meta.url),
      "utf8",
    );
    expect(agentFields).toContain("allowDefaultRuntime?: boolean");
    expect(agentFields).toContain(':required="!allowDefaultRuntime"');
    expect(agentFields).toContain("agents.runtimeDefault");
  });

  it("задаёт стабильные имена полям большой формы для браузерной диагностики", () => {
    const agentFields = readFileSync(
      new URL("../../platform/AgentFormFields.vue", import.meta.url),
      "utf8",
    );
    const environmentFields = readFileSync(
      new URL(
        "../../runtime/RuntimeEnvironmentFieldListsEditor.vue",
        import.meta.url,
      ),
      "utf8",
    );
    const environmentPolicy = readFileSync(
      new URL(
        "../../runtime/RuntimeEnvironmentPolicyFields.vue",
        import.meta.url,
      ),
      "utf8",
    );
    expect(source).toContain('name="assistant-plan-summary"');
    expect(source).toContain("assistant-operation-selected-${index}");
    expect(source).toContain("assistant-environment-description-${index}");
    expect(agentFields).toContain(':name="nameId"');
    expect(agentFields).toContain(':name="purposeId"');
    expect(agentFields).toContain(':name="roleDescriptionId"');
    expect(agentFields).toContain(':name="instructionsId"');
    expect(agentFields).toContain(':name="runtimeId"');
    expect(environmentFields).toContain("runtime-public-value-name-${index}");
    expect(environmentFields).toContain("runtime-secret-binding-name-${index}");
    expect(environmentPolicy).toContain("runtime-resource-${field.key}");
    expect(environmentPolicy).not.toContain(
      'name="runtime-read-own-execution"',
    );
    expect(environmentPolicy).toContain('name="runtime-web-access-mode"');
  });

  it("не помещает бинарное содержимое файла в текстовый редактор плана", () => {
    expect(source).toContain("projectFileEncoding(operation) === 'UTF8'");
    expect(source).toContain('type="file"');
    expect(source).toContain("setProjectBinaryFile(operation, $event)");
    expect(source).toContain(
      'accept="image/png,image/jpeg,image/webp,application/pdf"',
    );
    expect(source).toContain("projectFileBinaryStatus");
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
    expect(source).toContain("assistant.planEditor.roleImageCreateNextSteps");
    expect(source).toContain("assistant.planEditor.roleImageUpdateNextSteps");
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

  it("ограничивает высоту Dockerfile только во встроенном плане", () => {
    expect(source).toContain('class="assistant-plan-dockerfile"');
    expect(source).toContain(
      ".assistant-plan-dockerfile :deep(.dockerfile-editor__viewport)",
    );
    expect(source).toContain("height: clamp(240px, 36dvh, 360px)");
    expect(source).toContain("height: clamp(240px, 36dvh, 300px)");
    expect(source).toContain(".assistant-plan-dockerfile :deep(.cm-scroller)");
    expect(source).toMatch(/min-height: 0;\s+overflow: auto;/);
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
    expect(source).toContain('v-if="allowRawOperationEdit(operation)"');
  });

  it("показывает тест интеграции и архивирование как понятные подтверждения", () => {
    expect(source).toContain(
      "operation.value.type === 'TEST_INTEGRATION_CONNECTION'",
    );
    expect(source).toContain("assistant.planEditor.integrationTestBoundary");
    expect(source).toContain("operation.value.type === 'ARCHIVE_AGENT'");
    expect(source).toContain("operation.value.type === 'ARCHIVE_WORKFLOW'");
    expect(source).toContain("assistant.planEditor.archiveBoundary");
    expect(source).toContain("operationTargetLabel(operation.value.target)");
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

  it("объясняет причины отклонения плана, включая недоступный runtime", () => {
    expect(source).toContain('v-if="plan.validationProblems.length"');
    expect(source).toContain("validationProblemLabel(validationProblem)");
    expect(source).toContain('"runtime-unavailable": "runtimeUnavailable"');
    expect(source).toContain("assistant.planEditor.validationProblems");
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
