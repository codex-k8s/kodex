import { describe, expect, it } from "vitest";
import { reactive } from "vue";

import {
  assistantAwaitingReply,
  assistantEffectiveRuntimeState,
  assistantEnvironmentDraftTarget,
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
