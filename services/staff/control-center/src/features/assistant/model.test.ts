import { describe, expect, it } from "vitest";
import { reactive } from "vue";

import {
  assistantAwaitingReply,
  assistantCreatedScheduleTarget,
  assistantCreatedWorkflowTarget,
  assistantEffectiveRuntimeState,
  assistantEnvironmentDraftTarget,
  assistantIntegrationConnectionTarget,
  assistantLaunchedRunTarget,
  assistantRequiresProviderAccount,
  assistantRoleImageBuildTarget,
  editableOperations,
  friendlyPlanOperationType,
  operationActionLabel,
  operationInputs,
  operationTargetLabel,
  updateOperationParameter,
} from "@/features/assistant/model";
import type {
  AssistantConversation,
  AssistantPlan,
  AssistantPlanReceipt,
  AssistantPlanOperation,
  SystemAssistant,
} from "@/shared/api/generated/openapi/types.gen";

describe("assistant reply indicator", () => {
  const conversation = (
    role: "USER" | "ASSISTANT",
    state: "QUEUED" | "RUNNING" | "COMPLETED" | "FAILED",
  ) => ({ turns: [{ role, state }] }) as AssistantConversation;

  it("ожидает ответ после принятого сообщения пользователя", () => {
    expect(assistantAwaitingReply(conversation("USER", "COMPLETED"))).toBe(
      true,
    );
    expect(assistantAwaitingReply(conversation("USER", "QUEUED"))).toBe(true);
  });

  it("скрывает индикатор после ответа или ошибки", () => {
    expect(assistantAwaitingReply(conversation("ASSISTANT", "COMPLETED"))).toBe(
      false,
    );
    expect(assistantAwaitingReply(conversation("ASSISTANT", "FAILED"))).toBe(
      false,
    );
    expect(assistantAwaitingReply()).toBe(false);
  });
});

describe("assistant role image build target", () => {
  const imageOperation: AssistantPlanOperation = {
    ...operation(),
    ref: "op_image",
    type: "CREATE_ROLE_IMAGE_RECIPE",
    target: { kind: "ROLE_IMAGE_RECIPE", name: "Образ разработчика" },
  };
  const receipt: AssistantPlanReceipt = {
    ref: "rct_exact",
    planRef: "pln_exact",
    planRevision: 2,
    outcome: "APPLIED",
    operationReceipts: [
      {
        operationRef: "op_image",
        resourceRef: "rimg_exact",
        outcome: "APPLIED",
        auditRef: "aud_exact",
      },
    ],
    conflicts: [],
    auditRefs: ["aud_exact"],
    createdResourceRefs: ["rimg_exact"],
    createdAt: "2026-09-23T18:00:00Z",
  };
  const plan: AssistantPlan = {
    ref: "pln_exact",
    version: 3,
    revision: 2,
    state: "APPLIED",
    conversationRef: "conv_exact",
    projectRef: "prj_market",
    operations: [imageOperation],
    auditSummary: "Создать образ",
    applied: true,
    contentDigest: "a".repeat(64),
    validationProblems: [],
    nextActions: [],
    receipt,
  };

  it("берёт только точный ref из сохранённой квитанции", () => {
    expect(assistantRoleImageBuildTarget(plan, "op_image")).toEqual({
      projectRef: "prj_market",
      recipeRef: "rimg_exact",
    });
    expect(assistantRoleImageBuildTarget(plan, "op_other")).toBeUndefined();
  });

  it("не связывает сборку с другой ревизией, планом или дублированным эффектом", () => {
    expect(
      assistantRoleImageBuildTarget({ ...plan, revision: 3 }, "op_image"),
    ).toBeUndefined();
    expect(
      assistantRoleImageBuildTarget(
        { ...plan, receipt: { ...receipt, planRef: "pln_other" } },
        "op_image",
      ),
    ).toBeUndefined();
    expect(
      assistantRoleImageBuildTarget(
        {
          ...plan,
          receipt: {
            ...receipt,
            operationReceipts: [
              ...receipt.operationReceipts,
              ...receipt.operationReceipts,
            ],
          },
        },
        "op_image",
      ),
    ).toBeUndefined();
    expect(
      assistantRoleImageBuildTarget(
        { ...plan, operations: [{ ...imageOperation, selected: false }] },
        "op_image",
      ),
    ).toBeUndefined();
  });

  it("связывает черновик среды только с его операцией и квитанцией", () => {
    const environmentOperation: AssistantPlanOperation = {
      ...operation(),
      ref: "op_environment",
      type: "CREATE_RUNTIME_ENVIRONMENT_DRAFT",
      target: { kind: "RUNTIME_ENVIRONMENT_DRAFT", name: "Среда разработчика" },
    };
    const environmentPlan: AssistantPlan = {
      ...plan,
      operations: [environmentOperation],
      receipt: {
        ...receipt,
        operationReceipts: [
          {
            operationRef: "op_environment",
            resourceRef: "envdraft_exact",
            outcome: "APPLIED",
            auditRef: "aud_environment",
          },
        ],
      },
    };
    expect(
      assistantEnvironmentDraftTarget(environmentPlan, "op_environment"),
    ).toEqual({ projectRef: "prj_market", draftRef: "envdraft_exact" });
    expect(
      assistantRoleImageBuildTarget(environmentPlan, "op_environment"),
    ).toBeUndefined();
    expect(
      assistantEnvironmentDraftTarget(
        { ...environmentPlan, state: "DRAFT" },
        "op_environment",
      ),
    ).toBeUndefined();
  });

  it("связывает подключение с квитанцией без проектного контекста", () => {
    const connectionOperation: AssistantPlanOperation = {
      ...operation(),
      ref: "op_connection",
      type: "CREATE_INTEGRATION_CONNECTION",
      target: { kind: "INTEGRATION_CONNECTION", name: "GitHub" },
    };
    const connectionPlan: AssistantPlan = {
      ...plan,
      projectRef: undefined,
      operations: [connectionOperation],
      receipt: {
        ...receipt,
        operationReceipts: [
          {
            operationRef: "op_connection",
            resourceRef: "conn_exact",
            outcome: "APPLIED",
            auditRef: "aud_connection",
          },
        ],
      },
    };
    expect(
      assistantIntegrationConnectionTarget(connectionPlan, "op_connection"),
    ).toEqual({ connectionRef: "conn_exact" });
    expect(
      assistantIntegrationConnectionTarget(
        {
          ...connectionPlan,
          receipt: { ...receipt, planRevision: 1 },
        },
        "op_connection",
      ),
    ).toBeUndefined();
  });

  it("показывает созданную автоматизацию только по точной квитанции", () => {
    const scheduleOperation: AssistantPlanOperation = {
      ...operation(),
      ref: "op_schedule",
      type: "CREATE_SCHEDULE",
      target: { kind: "SCHEDULE", name: "Еженедельная сводка" },
    };
    const schedulePlan: AssistantPlan = {
      ...plan,
      operations: [scheduleOperation],
      receipt: {
        ...receipt,
        operationReceipts: [
          {
            operationRef: "op_schedule",
            resourceRef: "sch_exact",
            outcome: "APPLIED",
            auditRef: "aud_schedule",
          },
        ],
      },
    };
    expect(assistantCreatedScheduleTarget(schedulePlan, "op_schedule")).toEqual(
      {
        projectRef: "prj_market",
        scheduleRef: "sch_exact",
      },
    );
    expect(
      assistantCreatedScheduleTarget(
        { ...schedulePlan, revision: schedulePlan.revision + 1 },
        "op_schedule",
      ),
    ).toBeUndefined();
    expect(
      assistantCreatedScheduleTarget(
        { ...schedulePlan, projectRef: undefined },
        "op_schedule",
      ),
    ).toBeUndefined();
    expect(
      assistantCreatedScheduleTarget(
        {
          ...schedulePlan,
          operations: [
            {
              ...scheduleOperation,
              target: { kind: "WORKFLOW", name: "Еженедельная сводка" },
            },
          ],
        },
        "op_schedule",
      ),
    ).toBeUndefined();
  });

  it("показывает созданный процесс только по точной квитанции", () => {
    const workflowOperation: AssistantPlanOperation = {
      ...operation(),
      ref: "op_workflow",
      type: "CREATE_WORKFLOW",
      target: { kind: "WORKFLOW", name: "Еженедельная сводка" },
    };
    const workflowPlan: AssistantPlan = {
      ...plan,
      operations: [workflowOperation],
      receipt: {
        ...receipt,
        operationReceipts: [
          {
            operationRef: "op_workflow",
            resourceRef: "wfl_exact",
            outcome: "APPLIED",
            auditRef: "aud_workflow",
          },
        ],
      },
    };
    expect(assistantCreatedWorkflowTarget(workflowPlan, "op_workflow")).toEqual(
      {
        projectRef: "prj_market",
        workflowRef: "wfl_exact",
      },
    );
    expect(
      assistantCreatedWorkflowTarget(
        { ...workflowPlan, state: "VALID" },
        "op_workflow",
      ),
    ).toBeUndefined();
    expect(
      assistantCreatedWorkflowTarget(
        {
          ...workflowPlan,
          receipt: { ...receipt, planRef: "pln_other" },
        },
        "op_workflow",
      ),
    ).toBeUndefined();
  });

  it("показывает запуск только по точной квитанции применённого плана", () => {
    const runOperation: AssistantPlanOperation = {
      ...operation(),
      ref: "op_run",
      type: "LAUNCH_RUN",
      action: "EXECUTE",
      target: { kind: "WORKFLOW", name: "Еженедельный отчёт" },
    };
    const runPlan: AssistantPlan = {
      ...plan,
      operations: [runOperation],
      receipt: {
        ...receipt,
        operationReceipts: [
          {
            operationRef: "op_run",
            resourceRef: "run_exact",
            outcome: "APPLIED",
            auditRef: "aud_run",
          },
        ],
      },
    };
    expect(assistantLaunchedRunTarget(runPlan, "op_run")).toEqual({
      projectRef: "prj_market",
      runRef: "run_exact",
    });
    expect(
      assistantLaunchedRunTarget(
        {
          ...runPlan,
          operations: [
            { ...runOperation, target: { kind: "EXECUTION", name: "Отчёт" } },
          ],
        },
        "op_run",
      ),
    ).toEqual({ projectRef: "prj_market", runRef: "run_exact" });
    expect(
      assistantLaunchedRunTarget(
        { ...runPlan, projectRef: undefined },
        "op_run",
      ),
    ).toBeUndefined();
    expect(
      assistantLaunchedRunTarget(
        { ...runPlan, operations: [{ ...runOperation, selected: false }] },
        "op_run",
      ),
    ).toBeUndefined();
    expect(
      assistantLaunchedRunTarget(
        {
          ...runPlan,
          receipt: {
            ...receipt,
            operationReceipts: [
              {
                operationRef: "op_run",
                resourceRef: "../unsafe",
                outcome: "APPLIED",
                auditRef: "aud_run",
              },
            ],
          },
        },
        "op_run",
      ),
    ).toBeUndefined();
  });
});

function operation(): AssistantPlanOperation {
  return {
    ref: "op_project_create",
    type: "CREATE_PROJECT",
    action: "CREATE",
    title: "Создать проект",
    summary: "Создать рабочую область",
    target: { kind: "PROJECT", name: "Продажи" },
    parameters: { name: "Продажи", language: "ru" },
    before: {},
    after: { lifecycle: "ACTIVE" },
    selected: true,
    permitted: true,
    validationProblems: [],
  };
}

describe("assistant plan editor model", () => {
  it("сохраняет согласованные параметры и итог при изменении формы проекта", () => {
    const editable = editableOperations([
      {
        ...operation(),
        parameters: {
          name: "Продажи",
          purpose: "Работа с заказами",
          language: "ru",
        },
        after: {
          name: "Продажи",
          purpose: "Работа с заказами",
          language: "ru",
        },
      },
    ]);
    const first = editable[0];
    expect(first).toBeDefined();
    if (!first) return;
    expect(friendlyPlanOperationType(first)).toBe("CREATE_PROJECT");

    updateOperationParameter(first, "name", "Маркетплейс");
    updateOperationParameter(first, "language", "en");

    const changed = operationInputs(editable)[0];
    expect(changed?.parameters).toEqual(changed?.after);
    expect(changed?.parameters.name).toBe("Маркетплейс");
    expect(changed?.parameters.language).toBe("en");
    expect(changed?.target.name).toBe("Маркетплейс");
  });

  it("не подменяет исходную identity сотрудника при редактировании", () => {
    const editable = editableOperations([
      {
        ...operation(),
        type: "UPDATE_AGENT",
        action: "UPDATE",
        target: {
          kind: "AGENT",
          ref: "agt_existing",
          name: "Старое имя",
          version: 3,
        },
        expectedVersion: 3,
        parameters: { agentRef: "agt_existing", name: "Старое имя" },
        before: { agentRef: "agt_existing", name: "Старое имя" },
        after: { agentRef: "agt_existing", name: "Старое имя" },
      },
    ]);
    const first = editable[0];
    expect(first).toBeDefined();
    if (!first) return;
    expect(friendlyPlanOperationType(first)).toBe("UPDATE_AGENT");

    updateOperationParameter(first, "name", "Новое имя");

    const changed = operationInputs(editable)[0];
    expect(changed?.target.name).toBe("Старое имя");
    expect(changed?.parameters.name).toBe("Новое имя");
    expect(changed?.after.name).toBe("Новое имя");
    expect(changed?.before.name).toBe("Старое имя");
    expect(changed?.expectedVersion).toBe(3);
  });

  it("показывает изменение права сотрудника в форме без изменения identity", () => {
    const editable = editableOperations([
      {
        ...operation(),
        type: "CHANGE_CAPABILITY",
        action: "UPDATE",
        target: {
          kind: "AGENT",
          ref: "agt_existing",
          name: "Разработчик",
          version: 3,
        },
        expectedVersion: 3,
        parameters: {
          agentRef: "agt_existing",
          capabilityKey: "files.read",
          enabled: true,
          expectedVersion: 3,
        },
        before: { capabilityKey: "files.read", enabled: false },
        after: { capabilityKey: "files.read", enabled: true },
      },
    ]);
    const first = editable[0];
    expect(first).toBeDefined();
    if (!first) return;
    expect(friendlyPlanOperationType(first)).toBe("CHANGE_CAPABILITY");
    updateOperationParameter(first, "capabilityKey", "files.write");
    updateOperationParameter(first, "enabled", false);
    const changed = operationInputs(editable)[0];
    expect(changed?.target.ref).toBe("agt_existing");
    expect(changed?.expectedVersion).toBe(3);
    expect(changed?.parameters).toMatchObject({
      agentRef: "agt_existing",
      capabilityKey: "files.write",
      enabled: false,
      expectedVersion: 3,
    });
    expect(changed?.after.capabilityKey).toBe("files.write");
  });

  it("показывает черновик окружения как отдельную форму без секретных значений", () => {
    const editable = editableOperations([
      {
        ...operation(),
        type: "CREATE_RUNTIME_ENVIRONMENT_DRAFT",
        target: {
          kind: "RUNTIME_ENVIRONMENT_DRAFT",
          name: "Среда разработчика",
        },
        parameters: { projectRef: "prj_market", name: "Среда разработчика" },
        after: { projectRef: "prj_market", name: "Среда разработчика" },
      },
    ]);
    const first = editable[0];
    expect(first).toBeDefined();
    if (!first) return;
    expect(friendlyPlanOperationType(first)).toBe(
      "CREATE_RUNTIME_ENVIRONMENT_DRAFT",
    );
    updateOperationParameter(first, "name", "Среда Marketplace");
    updateOperationParameter(first, "description", "Сборка и тесты");
    const changed = operationInputs(editable)[0];
    expect(changed?.target.name).toBe("Среда Marketplace");
    expect(changed?.parameters).toEqual(changed?.after);
    expect(changed?.parameters.description).toBe("Сборка и тесты");
    expect(changed?.parameters.secretValue).toBeUndefined();
  });

  it("показывает рецепт образа как форму, сохраняя привязку к сотруднику", () => {
    const editable = editableOperations([
      {
        ...operation(),
        type: "CREATE_ROLE_IMAGE_RECIPE",
        target: { kind: "ROLE_IMAGE_RECIPE", name: "Образ разработчика" },
        parameters: {
          projectRef: "prj_market",
          agentRef: "agt_developer",
          agentVersion: 7,
          name: "Образ разработчика",
          environmentKey: "standard",
        },
        after: {
          projectRef: "prj_market",
          agentRef: "agt_developer",
          agentVersion: 7,
          name: "Образ разработчика",
          environmentKey: "standard",
        },
      },
    ]);
    const first = editable[0];
    expect(first).toBeDefined();
    if (!first) return;
    expect(friendlyPlanOperationType(first)).toBe("CREATE_ROLE_IMAGE_RECIPE");

    updateOperationParameter(first, "name", "Образ Marketplace");
    updateOperationParameter(first, "environmentKey", "documents");

    const changed = operationInputs(editable)[0];
    expect(changed?.target.name).toBe("Образ Marketplace");
    expect(changed?.parameters).toEqual(changed?.after);
    expect(changed?.parameters.agentRef).toBe("agt_developer");
    expect(changed?.parameters.agentVersion).toBe(7);
    expect(changed?.parameters.environmentKey).toBe("documents");
  });

  it("редактирует только публичную конфигурацию подключения, сохраняя тип интеграции", () => {
    const editable = editableOperations([
      {
        ...operation(),
        type: "CREATE_INTEGRATION_CONNECTION",
        target: { kind: "INTEGRATION_CONNECTION", name: "GitHub" },
        parameters: {
          definitionKey: "github",
          name: "GitHub",
          publicConfiguration: { organization: "marketplace" },
        },
        after: {
          definitionKey: "github",
          name: "GitHub",
          publicConfiguration: { organization: "marketplace" },
        },
      },
    ]);
    const first = editable[0];
    expect(first).toBeDefined();
    if (!first) return;
    expect(friendlyPlanOperationType(first)).toBe(
      "CREATE_INTEGRATION_CONNECTION",
    );

    updateOperationParameter(first, "name", "Marketplace GitHub");
    updateOperationParameter(first, "publicConfiguration", {
      organization: "marketplace",
      webhookEnabled: false,
    });

    const changed = operationInputs(editable)[0];
    expect(changed?.parameters).toEqual(changed?.after);
    expect(changed?.parameters.definitionKey).toBe("github");
    expect(changed?.parameters.publicConfiguration).toEqual({
      organization: "marketplace",
      webhookEnabled: false,
    });
    expect(changed?.parameters.credentialValue).toBeUndefined();
  });

  it("показывает запуск процесса как форму без доверия к свободному target kind", () => {
    const editable = editableOperations([
      {
        ...operation(),
        type: "LAUNCH_RUN",
        action: "EXECUTE",
        target: { kind: "EXECUTION", name: "Недельная сводка" },
        parameters: {
          projectRef: "prj_market",
          targetType: "WORKFLOW",
          targetRef: "wfl_weekly",
          title: "Недельная сводка",
          task: "Составь сводку за неделю",
          input: {},
        },
        after: { state: "QUEUED" },
      },
    ]);
    const first = editable[0];
    expect(first).toBeDefined();
    if (!first) return;
    expect(friendlyPlanOperationType(first)).toBe("LAUNCH_RUN");
    updateOperationParameter(first, "task", "Проверь неделю и дай сводку");
    const changed = operationInputs(editable)[0];
    expect(changed?.parameters.targetRef).toBe("wfl_weekly");
    expect(changed?.parameters.task).toBe("Проверь неделю и дай сводку");
    expect(changed?.target.kind).toBe("EXECUTION");
  });

  it("редактирует массив этапов процесса через обычную форму, сохраняя серверный target", () => {
    const editable = editableOperations([
      {
        ...operation(),
        type: "CREATE_WORKFLOW",
        target: { kind: "WORKFLOW", name: "Недельная сводка" },
        parameters: {
          projectRef: "prj_market",
          name: "Недельная сводка",
          purpose: "Собрать отчёт",
          coordinatorAgentRef: "agt_manager",
          steps: [{ name: "Сбор", agentRef: "agt_analyst" }],
        },
        after: {
          projectRef: "prj_market",
          name: "Недельная сводка",
          purpose: "Собрать отчёт",
          coordinatorAgentRef: "agt_manager",
          steps: [{ name: "Сбор", agentRef: "agt_analyst" }],
        },
      },
    ]);
    const first = editable[0];
    expect(first).toBeDefined();
    if (!first) return;
    expect(friendlyPlanOperationType(first)).toBe("CREATE_WORKFLOW");
    updateOperationParameter(first, "steps", [
      { name: "Анализ", agentRef: "agt_analyst" },
    ]);
    const changed = operationInputs(editable)[0];
    expect(changed?.parameters).toEqual(changed?.after);
    expect(changed?.parameters.steps).toEqual([
      { name: "Анализ", agentRef: "agt_analyst" },
    ]);
    expect(changed?.target.kind).toBe("WORKFLOW");
  });

  it("редактирует задание и cron автоматизации вместе с планируемым состоянием", () => {
    const parameters = {
      projectRef: "prj_market",
      name: "Недельная сводка",
      targetType: "WORKFLOW",
      targetRef: "wfl_weekly",
      preset: "CUSTOM",
      cronExpression: "0 9 * * 1",
      timeOfDay: "",
      timezone: "Europe/Saratov",
      input: {},
      automationText: "Составь сводку за неделю",
      sessionPolicy: "NEW_EACH_RUN",
      notificationPolicy: "CONTROL_CENTER_ONLY",
    };
    const editable = editableOperations([
      {
        ...operation(),
        type: "CREATE_SCHEDULE",
        target: { kind: "SCHEDULE", name: "Недельная сводка" },
        parameters,
        after: parameters,
      },
    ]);
    const first = editable[0];
    expect(first).toBeDefined();
    if (!first) return;
    expect(friendlyPlanOperationType(first)).toBe("CREATE_SCHEDULE");
    updateOperationParameter(first, "automationText", "Проверь итоги недели");
    updateOperationParameter(first, "cronExpression", "0 10 * * 1");
    const changed = operationInputs(editable)[0];
    expect(changed?.parameters).toEqual(changed?.after);
    expect(changed?.parameters.automationText).toBe("Проверь итоги недели");
    expect(changed?.parameters.cronExpression).toBe("0 10 * * 1");
  });

  it("создаёт независимый draft из Vue reactive proxy", () => {
    const source = reactive(operation());

    const editable = editableOperations([source]);

    expect(editable[0]?.value).not.toBe(source);
    expect(editable[0]?.value.target).not.toBe(source.target);
    expect(editable[0]?.value).toEqual(operation());
  });

  it("сохраняет полный набор явных параметров без скрытого преобразования", () => {
    const editable = editableOperations([operation()]);
    const first = editable[0];
    expect(first).toBeDefined();
    if (!first) return;
    first.parametersText = JSON.stringify({
      name: "Корпоративные продажи",
      language: "ru",
    });
    first.beforeText = JSON.stringify({ lifecycle: "DRAFT" });

    expect(operationInputs(editable)).toEqual([
      {
        ...operation(),
        parameters: {
          name: "Корпоративные продажи",
          language: "ru",
        },
        before: { lifecycle: "DRAFT" },
      },
    ]);
  });

  it("отклоняет scalar и array вместо JSON-объекта", () => {
    const editable = editableOperations([operation()]);
    const first = editable[0];
    expect(first).toBeDefined();
    if (!first) return;
    first.afterText = "[]";

    expect(() => operationInputs(editable)).toThrow("JSON_OBJECT_REQUIRED");
  });

  it("не скрывает редактируемое исходное состояние операции", () => {
    const editable = editableOperations([operation()]);
    const first = editable[0];
    expect(first).toBeDefined();
    if (!first) return;
    first.beforeText = JSON.stringify({ lifecycle: "ACTIVE", version: 7 });

    expect(operationInputs(editable)[0]?.before).toEqual({
      lifecycle: "ACTIVE",
      version: 7,
    });
  });

  it("показывает archive как явное удаление", () => {
    expect(operationActionLabel("ARCHIVE")).toBe("delete");
  });

  it.each([
    ["CREATE", "create"],
    ["UPDATE", "update"],
    ["ARCHIVE", "delete"],
    ["EXECUTE", "execute"],
  ] as const)("показывает действие %s явным глаголом %s", (action, label) => {
    expect(operationActionLabel(action)).toBe(label);
  });

  it("показывает человеку выбранный объект, а не только технический ref", () => {
    expect(
      operationTargetLabel({
        kind: "PROJECT",
        name: "Отдел продаж",
        ref: "prj_12345678",
        version: 3,
      }),
    ).toBe("Отдел продаж");
  });

  it("закрыто использует тип объекта, если имя цели отсутствует", () => {
    expect(operationTargetLabel({ kind: "PROJECT", name: "  " })).toBe(
      "PROJECT",
    );
  });
});

describe("assistant runtime presentation", () => {
  function assistant(
    runtimeState: SystemAssistant["runtimeState"],
    nextActions: SystemAssistant["nextActions"],
  ): SystemAssistant {
    return {
      ref: "asst_system",
      version: 1,
      name: "Kodex",
      system: true,
      removable: false,
      corePromptRevision: "core-v1",
      ownerInstructions: "",
      runtimeState,
      warmSessionRef: "sess_system",
      readinessSummary: "Восстановление runtime",
      nextActions,
    };
  }

  it("не показывает READY после снятия runtime action", () => {
    expect(assistantEffectiveRuntimeState(assistant("READY", []))).toBe(
      "RECOVERING",
    );
  });

  it("сохраняет READY только для фактически доступного нового диалога", () => {
    expect(
      assistantEffectiveRuntimeState(
        assistant("READY", ["CREATE_CONVERSATION", "ADD_TURN"]),
      ),
    ).toBe("READY");
  });

  it("не подменяет явное состояние выполнения", () => {
    expect(assistantEffectiveRuntimeState(assistant("BUSY", []))).toBe("BUSY");
  });

  it("отличает provider-free first-run от готовой warm session", () => {
    expect(
      assistantRequiresProviderAccount({
        ...assistant("FAILED", []),
        warmSessionRef: "",
      }),
    ).toBe(true);
    expect(assistantRequiresProviderAccount(assistant("READY", []))).toBe(
      false,
    );
  });
});
