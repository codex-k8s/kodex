import { describe, expect, it } from "vitest";
import { computed, reactive } from "vue";

import {
  assistantAgentEnvironmentBindingTarget,
  assistantActiveUserTurn,
  assistantAwaitingReply,
  assistantCreatedScheduleTarget,
  assistantCreatedEntityTarget,
  assistantCreatedWorkflowTarget,
  assistantEffectiveRuntimeState,
  assistantEnvironmentDraftTarget,
  assistantIntegrationConnectionTarget,
  assistantLaunchedRunTarget,
  assistantRequiresProviderAccount,
  assistantRoleImageBuildTarget,
  editableOperations,
  friendlyPlanOperationType,
  honestEditedPlanSummaries,
  latestAssistantSnapshot,
  operationActionLabel,
  operationInputs,
  operationSupportingTitle,
  operationTargetLabel,
  updateOperationParameter,
} from "@/features/assistant/model";
import type {
  AssistantConversation,
  AssistantTurn,
  AssistantPlan,
  AssistantPlanReceipt,
  AssistantPlanOperation,
  SystemAssistant,
} from "@/shared/api/generated/openapi/types.gen";

describe("assistant reply indicator", () => {
  it("распознаёт создание проектного помощника отдельно от обычного сотрудника", () => {
    const [value] = editableOperations([
      {
        ref: "op_assistant",
        type: "CREATE_PROJECT_ASSISTANT",
        action: "CREATE",
        title: "Помощник",
        summary: "Отдельная конфигурация",
        target: { kind: "PROJECT_ASSISTANT", name: "Помощник" },
        parameters: {
          name: "Помощник",
          purpose: "Помощь",
          instructions: "Инструкции",
        },
        before: {},
        after: {},
        selected: true,
        permitted: true,
        validationProblems: [],
      },
    ]);
    expect(value && friendlyPlanOperationType(value)).toBe(
      "CREATE_PROJECT_ASSISTANT",
    );
    if (value) value.value.target.kind = "AGENT";
    expect(value && friendlyPlanOperationType(value)).toBeUndefined();
  });
  it("результат создания помощника хранит ссылку профиля, а не ссылку сотрудника", () => {
    const create: AssistantPlanOperation = {
      ...operation(),
      type: "CREATE_PROJECT_ASSISTANT",
      target: { kind: "PROJECT_ASSISTANT", name: "Помощник" },
    };
    const applied: AssistantPlan = {
      ref: "plan_profile",
      version: 2,
      revision: 1,
      state: "APPLIED",
      conversationRef: "cnv_profile",
      projectRef: "project_fixture",
      operations: [create],
      applied: true,
      auditSummary: "Создать помощника",
      contentDigest: "a".repeat(64),
      validationProblems: [],
      nextActions: [],
      receipt: {
        ref: "receipt_profile",
        planRef: "plan_profile",
        planRevision: 1,
        outcome: "APPLIED",
        operationReceipts: [
          {
            operationRef: create.ref,
            resourceRef: "asstp_fixture",
            outcome: "APPLIED",
            auditRef: "audit_profile",
          },
        ],
        conflicts: [],
        auditRefs: [],
        createdResourceRefs: [],
        createdAt: "2026-10-04T00:00:00Z",
      },
    };
    expect(assistantCreatedEntityTarget(applied, create.ref)).toEqual({
      kind: "PROJECT_ASSISTANT",
      projectRef: "project_fixture",
      resourceRef: "asstp_fixture",
    });
    expect(
      assistantCreatedEntityTarget({ ...applied, state: "VALID" }, create.ref),
    ).toBeUndefined();
    expect(
      assistantCreatedEntityTarget(
        { ...applied, projectRef: undefined },
        create.ref,
      ),
    ).toBeUndefined();
    expect(
      assistantCreatedEntityTarget(
        {
          ...applied,
          operations: [
            { ...create, target: { kind: "AGENT", name: "Помощник" } },
          ],
        },
        create.ref,
      ),
    ).toBeUndefined();
    const confirmedReceipt = applied.receipt;
    if (!confirmedReceipt) throw new Error("Missing fixture receipt");
    expect(
      assistantCreatedEntityTarget(
        { ...applied, receipt: { ...confirmedReceipt, planRevision: 2 } },
        create.ref,
      ),
    ).toBeUndefined();
    expect(
      assistantCreatedEntityTarget(
        {
          ...applied,
          receipt: {
            ...confirmedReceipt,
            operationReceipts: [
              ...confirmedReceipt.operationReceipts,
              ...confirmedReceipt.operationReceipts,
            ],
          },
        },
        create.ref,
      ),
    ).toBeUndefined();
  });
  const conversation = (
    role: "USER" | "ASSISTANT",
    state: "QUEUED" | "RUNNING" | "COMPLETED" | "FAILED",
  ) =>
    ({
      state: "ACTIVE",
      turns: [{ role, state, sequence: 1 }],
    }) as AssistantConversation;

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

describe("assistant очередь после позднего ответа предыдущего запуска", () => {
  const turn = (
    sequence: number,
    role: AssistantTurn["role"],
    state: AssistantTurn["state"],
    runRef: string,
  ): AssistantTurn => ({
    ref: `trn_${String(sequence)}`,
    sequence,
    role,
    state,
    runRef,
    content: "Тестовое сообщение",
    createdAt: "2026-10-07T00:00:00Z",
  });
  const queuedConversation = (): AssistantConversation =>
    ({
      state: "ACTIVE",
      turns: [
        turn(1, "USER", "COMPLETED", "run_first"),
        turn(2, "USER", "RUNNING", "run_q1"),
        turn(3, "USER", "QUEUED", "run_q2"),
        turn(4, "ASSISTANT", "COMPLETED", "run_first"),
      ],
    }) as AssistantConversation;

  it("сохраняет ожидание и exact RUNNING USER, когда последним пришёл старый ответ", () => {
    const value = queuedConversation();
    expect(assistantAwaitingReply(value)).toBe(true);
    expect(assistantActiveUserTurn(value)?.runRef).toBe("run_q1");
    const reordered = { ...value, turns: [...value.turns].reverse() };
    expect(assistantAwaitingReply(reordered)).toBe(true);
    expect(assistantActiveUserTurn(reordered)?.runRef).toBe("run_q1");
    expect(value.turns.map((item) => item.sequence)).toEqual([1, 2, 3, 4]);
  });

  it("выбирает RUNNING прежде QUEUED и первый queued по server sequence", () => {
    const value = queuedConversation();
    value.turns[1] = turn(2, "USER", "QUEUED", "run_q1");
    expect(
      assistantActiveUserTurn({ ...value, turns: [...value.turns].reverse() })
        ?.runRef,
    ).toBe("run_q1");
    value.turns[2] = turn(3, "USER", "RUNNING", "run_q2");
    expect(assistantActiveUserTurn(value)?.runRef).toBe("run_q2");
  });

  it("реактивно следует завершению, interrupt и Stop без перезагрузки", () => {
    const value = reactive(queuedConversation());
    const awaiting = computed(() => assistantAwaitingReply(value));
    const active = computed(() => assistantActiveUserTurn(value)?.runRef);
    expect(active.value).toBe("run_q1");
    value.turns[1] = turn(2, "USER", "CANCELLED", "run_q1");
    value.turns[2] = turn(3, "USER", "CANCELLED", "run_q2");
    value.turns.push(turn(5, "USER", "RUNNING", "run_interrupt"));
    expect(awaiting.value).toBe(true);
    expect(active.value).toBe("run_interrupt");
    value.turns[4] = turn(5, "USER", "CANCELLED", "run_interrupt");
    expect(awaiting.value).toBe(false);
    expect(active.value).toBeUndefined();
  });

  it.each(["CLOSED", "ARCHIVED"] as const)(
    "не показывает активность %s диалога",
    (state) => {
      const value = { ...queuedConversation(), state };
      expect(assistantAwaitingReply(value)).toBe(false);
      expect(assistantActiveUserTurn(value)).toBeUndefined();
    },
  );

  it.each(["COMPLETED", "FAILED", "CANCELLED"] as const)(
    "не выбирает terminal USER %s как активный",
    (state) => {
      const value = {
        ...queuedConversation(),
        turns: [
          turn(1, "USER", state, "run_done"),
          turn(2, "ASSISTANT", state, "run_done"),
        ],
      };
      expect(assistantAwaitingReply(value)).toBe(false);
      expect(assistantActiveUserTurn(value)).toBeUndefined();
    },
  );

  it("не считает queued ASSISTANT активным USER и удалённый диалог активным", () => {
    const value = {
      ...queuedConversation(),
      turns: [turn(1, "ASSISTANT", "QUEUED", "run_other")],
    };
    expect(assistantAwaitingReply(value)).toBe(false);
    expect(assistantActiveUserTurn(value)).toBeUndefined();
    expect(assistantActiveUserTurn()).toBeUndefined();
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
  it("общесистемный образ не выводит scope из projectRef плана", () => {
    const owner = {
      scopeKind: "ORGANIZATION",
      organizationRef: "org_synthetic",
      systemAssistantRef: "agt_system",
    };
    const systemPlan: AssistantPlan = {
      ...plan,
      operations: [
        {
          ...imageOperation,
          type: "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE",
          parameters: owner,
          after: owner,
        },
      ],
    };
    expect(
      assistantRoleImageBuildTarget(systemPlan, "op_image", "org_synthetic"),
    ).toEqual({
      resourceScope: { kind: "ORGANIZATION", organizationRef: "org_synthetic" },
      recipeRef: "rimg_exact",
    });
    expect(
      assistantRoleImageBuildTarget(systemPlan, "op_image"),
    ).toBeUndefined();
    expect(
      assistantRoleImageBuildTarget(systemPlan, "op_image", "org_foreign"),
    ).toBeUndefined();
    const first = systemPlan.operations[0];
    if (!first) throw new Error("Missing system image operation");
    const mixed = {
      ...systemPlan,
      operations: [{ ...first, after: { ...owner, projectRef: "" } }],
    };
    expect(
      assistantRoleImageBuildTarget(mixed, "op_image", "org_synthetic"),
    ).toBeUndefined();
  });

  it("наблюдает новую сборку только у точно обновлённого рецепта", () => {
    const updated = {
      ...imageOperation,
      type: "UPDATE_ROLE_IMAGE_RECIPE" as const,
      action: "UPDATE" as const,
      target: {
        kind: "ROLE_IMAGE_RECIPE",
        ref: "rimg_exact",
        name: "Образ разработчика",
        version: 4,
      },
    };
    expect(
      assistantRoleImageBuildTarget(
        { ...plan, operations: [updated] },
        "op_image",
      ),
    ).toEqual({
      projectRef: "prj_market",
      recipeRef: "rimg_exact",
    });
    expect(
      assistantRoleImageBuildTarget(
        {
          ...plan,
          operations: [
            { ...updated, target: { ...updated.target, ref: "rimg_other" } },
          ],
        },
        "op_image",
      ),
    ).toBeUndefined();
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
      assistantEnvironmentDraftTarget(
        {
          ...environmentPlan,
          operations: [
            {
              ...environmentOperation,
              type: "PREPARE_RUNTIME_ENVIRONMENT_REVISION",
              action: "UPDATE",
              target: {
                kind: "ENVIRONMENT",
                ref: "renv_exact",
                name: "Среда разработчика",
              },
            },
          ],
        },
        "op_environment",
      ),
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

  it("открывает привязку окружения только по точной применённой квитанции", () => {
    const bindingOperation: AssistantPlanOperation = {
      ...operation(),
      ref: "op_binding",
      type: "BIND_AGENT_RUNTIME_ENVIRONMENT",
      action: "UPDATE",
      target: { kind: "AGENT", ref: "agt_exact", name: "Developer" },
      after: { environmentRef: "renv_exact", versionRef: "renvv_exact" },
    };
    const bindingPlan: AssistantPlan = {
      ...plan,
      operations: [bindingOperation],
      receipt: {
        ...receipt,
        operationReceipts: [
          {
            operationRef: "op_binding",
            resourceRef: "agt_exact",
            outcome: "APPLIED",
            auditRef: "aud_binding",
          },
        ],
      },
    };
    expect(
      assistantAgentEnvironmentBindingTarget(bindingPlan, "op_binding"),
    ).toEqual({
      projectRef: "prj_market",
      agentRef: "agt_exact",
      environmentRef: "renv_exact",
      versionRef: "renvv_exact",
    });
    expect(
      assistantAgentEnvironmentBindingTarget(
        { ...bindingPlan, state: "DRAFT" },
        "op_binding",
      ),
    ).toBeUndefined();
    expect(
      assistantAgentEnvironmentBindingTarget(
        {
          ...bindingPlan,
          receipt: {
            ...receipt,
            operationReceipts: [
              {
                operationRef: "op_binding",
                resourceRef: "agt_other",
                outcome: "APPLIED",
                auditRef: "aud_binding",
              },
            ],
          },
        },
        "op_binding",
      ),
    ).toBeUndefined();
  });

  it("связывает созданное и изменённое подключение с точной квитанцией", () => {
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
          operations: [
            {
              ...connectionOperation,
              type: "UPDATE_INTEGRATION_CONNECTION",
              action: "UPDATE",
              target: {
                kind: "INTEGRATION_CONNECTION",
                ref: "conn_exact",
                name: "GitHub",
              },
            },
          ],
        },
        "op_connection",
      ),
    ).toEqual({ connectionRef: "conn_exact" });
    expect(
      assistantIntegrationConnectionTarget(
        {
          ...connectionPlan,
          operations: [
            {
              ...connectionOperation,
              type: "TEST_INTEGRATION_CONNECTION",
              action: "EXECUTE",
              target: {
                kind: "INTEGRATION_CONNECTION",
                ref: "conn_exact",
                name: "GitHub",
              },
            },
          ],
        },
        "op_connection",
      ),
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
    const updated = {
      ...schedulePlan,
      operations: [
        {
          ...scheduleOperation,
          type: "UPDATE_SCHEDULE" as const,
          action: "UPDATE" as const,
          target: {
            kind: "SCHEDULE",
            ref: "sch_exact",
            name: "Еженедельная сводка",
          },
        },
      ],
    };
    expect(assistantCreatedScheduleTarget(updated, "op_schedule")).toEqual({
      projectRef: "prj_market",
      scheduleRef: "sch_exact",
    });
    const updatedOperation = updated.operations[0];
    expect(updatedOperation).toBeDefined();
    if (!updatedOperation) return;
    expect(
      assistantCreatedScheduleTarget(
        {
          ...updated,
          operations: [
            {
              ...updatedOperation,
              target: { kind: "SCHEDULE", ref: "sch_other", name: "Другая" },
            },
          ],
        },
        "op_schedule",
      ),
    ).toBeUndefined();
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

  it("связывает созданные и изменённые проект и сотрудника с их квитанциями", () => {
    const projectOperation: AssistantPlanOperation = {
      ...operation(),
      ref: "op_project",
      type: "CREATE_PROJECT",
      target: { kind: "PROJECT", name: "Маркетплейс" },
    };
    const agentOperation: AssistantPlanOperation = {
      ...operation(),
      ref: "op_agent",
      type: "CREATE_AGENT",
      target: { kind: "AGENT", name: "Разработчик" },
    };
    const entityPlan: AssistantPlan = {
      ...plan,
      projectRef: "prj_current",
      operations: [projectOperation, agentOperation],
      receipt: {
        ...receipt,
        operationReceipts: [
          {
            operationRef: "op_project",
            resourceRef: "prj_new",
            outcome: "APPLIED",
            auditRef: "aud_project",
          },
          {
            operationRef: "op_agent",
            resourceRef: "agt_new",
            outcome: "APPLIED",
            auditRef: "aud_agent",
          },
        ],
      },
    };
    expect(assistantCreatedEntityTarget(entityPlan, "op_project")).toEqual({
      kind: "PROJECT",
      projectRef: "prj_new",
      resourceRef: "prj_new",
    });
    expect(assistantCreatedEntityTarget(entityPlan, "op_agent")).toEqual({
      kind: "AGENT",
      projectRef: "prj_current",
      resourceRef: "agt_new",
    });
    expect(
      assistantCreatedEntityTarget(
        {
          ...entityPlan,
          operations: [
            {
              ...projectOperation,
              type: "UPDATE_PROJECT",
              action: "UPDATE",
              target: {
                kind: "PROJECT",
                ref: "prj_new",
                name: "Маркетплейс",
              },
            },
            {
              ...agentOperation,
              type: "UPDATE_AGENT",
              action: "UPDATE",
              target: {
                kind: "AGENT",
                ref: "agt_new",
                name: "Разработчик",
              },
            },
          ],
        },
        "op_project",
      ),
    ).toEqual({
      kind: "PROJECT",
      projectRef: "prj_new",
      resourceRef: "prj_new",
    });
    expect(
      assistantCreatedEntityTarget(
        {
          ...entityPlan,
          operations: [
            projectOperation,
            {
              ...agentOperation,
              type: "UPDATE_AGENT",
              action: "UPDATE",
              target: {
                kind: "AGENT",
                ref: "agt_new",
                name: "Разработчик",
              },
            },
          ],
        },
        "op_agent",
      ),
    ).toEqual({
      kind: "AGENT",
      projectRef: "prj_current",
      resourceRef: "agt_new",
    });
    expect(
      assistantCreatedEntityTarget(
        { ...entityPlan, projectRef: undefined },
        "op_agent",
      ),
    ).toBeUndefined();
    expect(
      assistantCreatedEntityTarget(
        { ...entityPlan, revision: entityPlan.revision + 1 },
        "op_project",
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

describe("assistant plan card", () => {
  it("не повторяет сокращённый заголовок перед тем же описанием", () => {
    const item = operation();
    expect(
      operationSupportingTitle({
        ...item,
        title: "Подготовить новую ревизию…",
        summary: "Подготовить новую ревизию окружения без изменения секретов",
      }),
    ).toBeUndefined();
    expect(
      operationSupportingTitle({ ...item, title: "Продажи" }),
    ).toBeUndefined();
  });

  it("сохраняет отдельный информативный заголовок", () => {
    expect(operationSupportingTitle(operation())).toBe("Создать проект");
  });
});

describe("assistant plan editor model", () => {
  it("показывает изменение инструкций Kodex как редактируемую штатную форму", () => {
    const parameters = {
      systemAssistantRef: "agt_system123",
      instructions: "Перед изменением сущности перечисляй ожидаемый результат.",
    };
    const editable = editableOperations([
      {
        ...operation(),
        type: "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS",
        action: "UPDATE",
        target: {
          kind: "SYSTEM_ASSISTANT",
          ref: "agt_system123",
          name: "Kodex",
        },
        expectedVersion: 9,
        parameters,
        before: {
          systemAssistantRef: "agt_system123",
          name: "Kodex",
          instructions: "",
        },
        after: { ...parameters },
      },
    ]);
    const first = editable[0];
    expect(first).toBeDefined();
    if (!first) return;
    expect(friendlyPlanOperationType(first)).toBe(
      "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS",
    );
    updateOperationParameter(
      first,
      "instructions",
      "Перед изменением сущности перечисляй результат и риски.",
    );
    const changed = operationInputs(editable)[0];
    expect(changed?.parameters.instructions).toBe(
      "Перед изменением сущности перечисляй результат и риски.",
    );
    expect(changed?.parameters.systemAssistantRef).toBe("agt_system123");
  });

  it("убирает устаревшую сводку после изменения полей формы", () => {
    const item = {
      ...operation(),
      type: "UPDATE_PROJECT" as const,
      action: "UPDATE" as const,
      summary: "Назначение: временное",
      parameters: { purpose: "временное" },
      after: { purpose: "временное" },
    };
    const plan: AssistantPlan = {
      ref: "pln_edit",
      version: 1,
      revision: 1,
      state: "DRAFT",
      conversationRef: "conv_edit",
      operations: [item],
      auditSummary: "Назначение: временное",
      applied: false,
      contentDigest: "a".repeat(64),
      validationProblems: [],
      nextActions: [],
    };
    const edited = {
      ...item,
      parameters: { purpose: "итоговое" },
      after: { purpose: "итоговое" },
    };
    const labels = { plan: "План исправлен", operation: "Поля исправлены" };

    expect(
      honestEditedPlanSummaries(plan, plan.auditSummary, [edited], labels),
    ).toMatchObject({
      auditSummary: "План исправлен",
      operations: [{ summary: "Поля исправлены" }],
    });
    expect(
      honestEditedPlanSummaries(
        plan,
        "Проверенное пояснение пользователя",
        [{ ...edited, summary: "Проверенное описание" }],
        labels,
      ),
    ).toMatchObject({
      auditSummary: "Проверенное пояснение пользователя",
      operations: [{ summary: "Проверенное описание" }],
    });
    expect(
      honestEditedPlanSummaries(plan, plan.auditSummary, [item], labels),
    ).toMatchObject({
      auditSummary: plan.auditSummary,
      operations: [{ summary: item.summary }],
    });
  });

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

  it("не дублирует бинарное содержимое файла в итоговом снимке", () => {
    const editable = editableOperations([
      {
        ...operation(),
        type: "CREATE_PROJECT_FILE",
        target: { kind: "ARTIFACT", name: "pixel.png" },
        parameters: {
          projectRef: "prj_example",
          fileName: "pixel.png",
          mediaType: "image/png",
          contentEncoding: "BASE64",
          content: "",
        },
        after: {
          projectRef: "prj_example",
          fileName: "pixel.png",
          mediaType: "image/png",
          contentEncoding: "BASE64",
          content: "",
        },
      },
    ]);
    const first = editable[0];
    expect(first).toBeDefined();
    if (!first) return;

    updateOperationParameter(first, "content", "iVBORw0KGgo=");

    const changed = operationInputs(editable)[0];
    expect(changed?.parameters.content).toBe("iVBORw0KGgo=");
    expect(changed?.after).not.toHaveProperty("content");
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

  it("правит инструкции как версионированный черновик, а не создаёт сотрудника", () => {
    const text = "Координируй проект и сообщай подтверждённые результаты.";
    const editable = editableOperations([
      {
        ...operation(),
        type: "CREATE_INSTRUCTION_DRAFT",
        action: "UPDATE",
        target: {
          kind: "AGENT",
          ref: "agt_existing",
          name: "Менеджер",
          version: 7,
        },
        expectedVersion: 7,
        parameters: { agentRef: "agt_existing", instructions: text },
        before: { agentRef: "agt_existing", name: "Менеджер" },
        after: { agentRef: "agt_existing", instructions: text },
      },
    ]);
    const first = editable[0];
    expect(first).toBeDefined();
    if (!first) return;
    expect(friendlyPlanOperationType(first)).toBe("CREATE_INSTRUCTION_DRAFT");
    updateOperationParameter(
      first,
      "instructions",
      "Проверяй результаты проекта и докладывай владельцу.",
    );
    const changed = operationInputs(editable)[0];
    expect(changed?.action).toBe("UPDATE");
    expect(changed?.target.ref).toBe("agt_existing");
    expect(changed?.expectedVersion).toBe(7);
    expect(changed?.parameters.instructions).toBe(changed?.after.instructions);
    expect(changed?.before).toEqual({
      agentRef: "agt_existing",
      name: "Менеджер",
    });
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

  it("показывает право интеграции в форме с неизменной identity подключения", () => {
    const editable = editableOperations([
      {
        ...operation(),
        type: "CHANGE_INTEGRATION_GRANT",
        action: "UPDATE",
        target: {
          kind: "INTEGRATION_CONNECTION",
          ref: "conn_exact",
          name: "GitHub",
          version: 4,
        },
        expectedVersion: 4,
        parameters: {
          connectionRef: "conn_exact",
          agentRef: "agt_exact",
          capabilityKey: "github.repo.read",
          enabled: true,
          expectedVersion: 4,
        },
        before: { capabilityKey: "github.repo.read", enabled: false },
        after: { capabilityKey: "github.repo.read", enabled: true },
      },
    ]);
    const first = editable[0];
    expect(first).toBeDefined();
    if (!first) return;
    expect(friendlyPlanOperationType(first)).toBe("CHANGE_INTEGRATION_GRANT");
    updateOperationParameter(first, "capabilityKey", "github.repo.write");
    const changed = operationInputs(editable)[0];
    expect(changed?.target).toMatchObject({
      kind: "INTEGRATION_CONNECTION",
      ref: "conn_exact",
      version: 4,
    });
    expect(changed?.parameters).toMatchObject({
      connectionRef: "conn_exact",
      agentRef: "agt_exact",
      capabilityKey: "github.repo.write",
      enabled: true,
      expectedVersion: 4,
    });
    expect(changed?.parameters.workflowRef).toBeUndefined();
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

  it("редактирует среду через отдельную форму без подмены защищённого снимка", () => {
    const parameters = {
      environmentRef: "renv_exact",
      projectRef: "prj_market",
      name: "Среда Marketplace",
      description: "Первая ревизия",
      imageArtifactRef: "imgart_exact",
    };
    const editable = editableOperations([
      {
        ...operation(),
        type: "PREPARE_RUNTIME_ENVIRONMENT_REVISION",
        action: "UPDATE",
        target: {
          kind: "ENVIRONMENT",
          ref: "renv_exact",
          name: "Среда Marketplace",
        },
        parameters,
        before: {
          ...parameters,
          specification: { secretBindings: [{ secretRef: "sec_exact" }] },
        },
        after: { ...parameters },
      },
    ]);
    const first = editable[0];
    expect(first).toBeDefined();
    if (!first) return;
    expect(friendlyPlanOperationType(first)).toBe(
      "PREPARE_RUNTIME_ENVIRONMENT_REVISION",
    );
    updateOperationParameter(first, "description", "Вторая ревизия");
    const changed = operationInputs(editable)[0];
    expect(changed?.parameters.description).toBe("Вторая ревизия");
    expect(changed?.before).toEqual({
      ...parameters,
      specification: { secretBindings: [{ secretRef: "sec_exact" }] },
    });
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

  it("показывает обновление образа в форме без изменения exact ref", () => {
    const before = {
      projectRef: "prj_market",
      recipeRef: "rimg_exact",
      name: "Образ",
      environmentKey: "standard",
    };
    const editable = editableOperations([
      {
        ...operation(),
        type: "UPDATE_ROLE_IMAGE_RECIPE",
        action: "UPDATE",
        target: {
          kind: "ROLE_IMAGE_RECIPE",
          ref: "rimg_exact",
          name: "Образ",
          version: 4,
        },
        expectedVersion: 4,
        parameters: { ...before, name: "Образ 2" },
        before,
        after: { ...before, name: "Образ 2" },
      },
    ]);
    const first = editable[0];
    expect(first).toBeDefined();
    if (!first) return;
    expect(friendlyPlanOperationType(first)).toBe("UPDATE_ROLE_IMAGE_RECIPE");
    updateOperationParameter(first, "environmentKey", "documents");
    const changed = operationInputs(editable)[0];
    expect(changed?.target.ref).toBe("rimg_exact");
    expect(changed?.parameters.environmentKey).toBe("documents");
    expect(changed?.before).toEqual(before);
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

  it("показывает изменение подключения той же дружественной формой", () => {
    const editable = editableOperations([
      {
        ...operation(),
        type: "UPDATE_INTEGRATION_CONNECTION",
        action: "UPDATE",
        target: {
          kind: "INTEGRATION_CONNECTION",
          ref: "con_source",
          name: "Source",
          version: 3,
        },
        expectedVersion: 3,
        parameters: {
          connectionRef: "con_source",
          definitionKey: "github",
          name: "Source code",
          publicConfiguration: { organization: "marketplace" },
        },
        before: {
          connectionRef: "con_source",
          definitionKey: "github",
          name: "Source",
          publicConfiguration: { organization: "marketplace" },
        },
        after: {
          connectionRef: "con_source",
          definitionKey: "github",
          name: "Source code",
          publicConfiguration: { organization: "marketplace" },
        },
      },
    ]);
    const first = editable[0];
    expect(first).toBeDefined();
    if (!first) return;
    expect(friendlyPlanOperationType(first)).toBe(
      "UPDATE_INTEGRATION_CONNECTION",
    );
    updateOperationParameter(first, "name", "Production source");
    const changed = operationInputs(editable)[0];
    expect(changed?.parameters.name).toBe("Production source");
    expect(changed?.parameters.definitionKey).toBe("github");
    expect(changed?.before.name).toBe("Source");
  });

  it("показывает публикацию интеграции без редактирования закреплённой ревизии", () => {
    const editable = editableOperations([
      {
        ...operation(),
        type: "PUBLISH_INTEGRATION_DEFINITION",
        action: "UPDATE",
        target: {
          kind: "INTEGRATION_DEFINITION",
          ref: "mcfg_source",
          name: "Source API",
          version: 4,
        },
        expectedVersion: 4,
        parameters: {
          configurationRef: "mcfg_source",
          revisionRef: "mrev_valid",
          revisionDigest: "a".repeat(64),
        },
        before: { currentRevisionRef: "mrev_old" },
        after: { currentRevisionRef: "mrev_valid" },
      },
    ]);
    const first = editable[0];
    expect(first).toBeDefined();
    if (!first) return;
    expect(friendlyPlanOperationType(first)).toBe(
      "PUBLISH_INTEGRATION_DEFINITION",
    );
    expect(operationInputs(editable)[0]?.parameters).toEqual({
      configurationRef: "mcfg_source",
      revisionRef: "mrev_valid",
      revisionDigest: "a".repeat(64),
    });
    first.value.target.kind = "PROJECT";
    expect(friendlyPlanOperationType(first)).toBeUndefined();
  });

  it("показывает запуск процесса как форму только при совпадении target и параметров", () => {
    const editable = editableOperations([
      {
        ...operation(),
        type: "LAUNCH_RUN",
        action: "EXECUTE",
        target: {
          kind: "WORKFLOW",
          ref: "wfl_weekly",
          name: "Недельная сводка",
        },
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
    expect(changed?.target.kind).toBe("WORKFLOW");
    first.value.target.kind = "AGENT";
    expect(friendlyPlanOperationType(first)).toBeUndefined();
    first.value.target.kind = "WORKFLOW";
    first.value.target.ref = "wfl_other";
    expect(friendlyPlanOperationType(first)).toBeUndefined();
  });

  it("сохраняет дружелюбное чтение старого плана запуска с target EXECUTION", () => {
    const [first] = editableOperations([
      {
        ...operation(),
        type: "LAUNCH_RUN",
        action: "EXECUTE",
        target: { kind: "EXECUTION", name: "Недельная сводка" },
        parameters: {
          projectRef: "prj_market",
          targetType: "AGENT",
          targetRef: "agt_manager",
          title: "Недельная сводка",
          task: "Составь сводку за неделю",
          input: {},
        },
        after: { state: "QUEUED" },
      },
    ]);
    expect(first && friendlyPlanOperationType(first)).toBe("LAUNCH_RUN");
  });

  it.each([
    ["TEST_INTEGRATION_CONNECTION", "EXECUTE", "INTEGRATION_CONNECTION"],
    ["ARCHIVE_AGENT", "ARCHIVE", "AGENT"],
    ["ARCHIVE_WORKFLOW", "ARCHIVE", "WORKFLOW"],
  ] as const)(
    "показывает %s как дружелюбное подтверждение точного объекта",
    (type, action, kind) => {
      const editable = editableOperations([
        {
          ...operation(),
          type,
          action,
          target: {
            kind,
            ref: "target_existing",
            name: "Точный объект",
            version: 8,
          },
          expectedVersion: 8,
          parameters: {},
          before: { state: "ACTIVE" },
          after: {
            state: type.startsWith("ARCHIVE_") ? "ARCHIVED" : "QUEUED",
          },
        },
      ]);
      const first = editable[0];
      expect(first).toBeDefined();
      if (!first) return;
      expect(friendlyPlanOperationType(first)).toBe(type);
      first.value.action = "UPDATE";
      expect(friendlyPlanOperationType(first)).toBeUndefined();
    },
  );

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

  it("редактирует описание процесса, не открывая граф этапов для подмены", () => {
    const parameters = {
      workflowRef: "wfl_weekly",
      projectRef: "prj_market",
      name: "Недельная сводка",
      purpose: "Собрать отчёт",
      instructions: "Проверь источники",
      completionCriteria: "Сводка готова",
      maxConcurrency: 1,
      timeoutSeconds: 3600,
    };
    const editable = editableOperations([
      {
        ...operation(),
        type: "UPDATE_WORKFLOW",
        action: "UPDATE",
        target: {
          kind: "WORKFLOW",
          ref: "wfl_weekly",
          name: "Недельная сводка",
        },
        parameters,
        after: { ...parameters },
      },
    ]);
    const first = editable[0];
    expect(first).toBeDefined();
    if (!first) return;
    expect(friendlyPlanOperationType(first)).toBe("UPDATE_WORKFLOW");
    updateOperationParameter(first, "purpose", "Собрать проверенный отчёт");
    const changed = operationInputs(editable)[0];
    expect(changed?.parameters.purpose).toBe("Собрать проверенный отчёт");
    expect(changed?.parameters.steps).toBeUndefined();
    expect(changed?.target.ref).toBe("wfl_weekly");
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

  it("редактирует точную существующую автоматизацию без изменения её ссылки и версии", () => {
    const parameters = {
      scheduleRef: "sch_weekly",
      projectRef: "prj_market",
      name: "Недельная сводка",
      targetType: "AGENT",
      targetRef: "agt_manager",
      preset: "WEEKLY",
      cronExpression: "0 9 * * 1",
      timeOfDay: "09:00",
      dayOfWeek: "MONDAY",
      timezone: "Europe/Saratov",
      input: {},
      automationText: "Составь сводку за неделю",
      sessionPolicy: "NEW_EACH_RUN",
      notificationPolicy: "CONTROL_CENTER_ONLY",
      dstGapPolicy: "SHIFT_FORWARD",
      dstFoldPolicy: "RUN_ONCE_EARLIEST",
      misfirePolicy: "COALESCE",
      overlapPolicy: "FORBID",
      promptInputs: {},
    };
    const editable = editableOperations([
      {
        ...operation(),
        type: "UPDATE_SCHEDULE",
        action: "UPDATE",
        target: {
          kind: "SCHEDULE",
          ref: "sch_weekly",
          name: "Недельная сводка",
          version: 4,
        },
        expectedVersion: 4,
        parameters,
        before: { ...parameters, automationText: "Старая задача" },
        after: parameters,
      },
    ]);
    const first = editable[0];
    expect(first).toBeDefined();
    if (!first) return;
    expect(friendlyPlanOperationType(first)).toBe("UPDATE_SCHEDULE");
    updateOperationParameter(
      first,
      "automationText",
      "Проверь завершённые работы",
    );
    const changed = operationInputs(editable)[0];
    expect(changed?.target.ref).toBe("sch_weekly");
    expect(changed?.expectedVersion).toBe(4);
    expect(changed?.parameters.automationText).toBe(
      "Проверь завершённые работы",
    );
    expect(changed?.parameters).toEqual(changed?.after);
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

  it("показывает более свежую готовность из глобального чтения вместо устаревшей сводки администрирования", () => {
    const administration = {
      ...assistant("RECOVERING", []),
      lastHeartbeatAt: "2026-09-27T23:02:00Z",
    };
    const live = {
      ...assistant("READY", ["CREATE_CONVERSATION"]),
      lastHeartbeatAt: "2026-09-27T23:03:00Z",
      readinessSummary: "Готов к команде",
    } satisfies SystemAssistant;
    expect(latestAssistantSnapshot(administration, live)).toBe(live);
    expect(
      latestAssistantSnapshot({ ...administration, version: 2 }, live),
    ).toMatchObject({ runtimeState: "RECOVERING", version: 2 });
    expect(
      latestAssistantSnapshot(administration, {
        ...live,
        ref: "other_assistant",
      }),
    ).toBe(administration);
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
