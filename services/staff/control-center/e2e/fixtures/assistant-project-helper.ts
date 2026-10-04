import type {
  AssistantPlan,
  AssistantPlanOperation,
  RuntimeEnvironmentPolicyInput,
} from "../../src/shared/api/generated/openapi/types.gen";

export const helperPins = {
  projectAssistantRef: "agt_project_helper",
  assistantScope: "PROJECT",
  scopeKind: "PROJECT",
  organizationRef: "org_fixture",
  projectRef: "prj_project_helper",
  assistantProfileRef: "asstp_project_helper",
  agentVersion: 3,
  runtimeEnvironmentBindingRef: "envbind_project_helper",
  runtimeEnvironmentVersionRef: "envver_project_helper",
  runtimeEnvironmentDigest: "a".repeat(64),
};
export const helperPolicy: RuntimeEnvironmentPolicyInput = {
  resources: {
    cpuRequestMilli: 500,
    cpuLimitMilli: 2000,
    memoryRequestMib: 512,
    memoryLimitMib: 2048,
    ephemeralStorageRequestMib: 512,
    ephemeralStorageLimitMib: 4096,
  },
  volumes: [],
  networkDestinations: ["DNS", "PROVIDER_PROXY", "RUNTIME_CALLBACK"],
  webAccess: { mode: "NONE", rules: [] },
  kubernetesAccess: "NONE",
};
export function projectHelperPlan(
  kind: string,
  applied: boolean,
  tamper?: string,
  sourceProject?: string,
): AssistantPlan {
  const type =
    kind === "ENV"
      ? "PREPARE_RUNTIME_ENVIRONMENT_REVISION"
      : kind === "BIND"
        ? "BIND_AGENT_RUNTIME_ENVIRONMENT"
        : "CREATE_INSTRUCTION_DRAFT";
  const fields =
    type === "PREPARE_RUNTIME_ENVIRONMENT_REVISION"
      ? {
          environmentRef: "env_project_helper",
          name: "Рабочая среда проектного помощника",
          description: "Настройка из общесистемного диалога",
          imageArtifactRef: "imgart_project_helper",
        }
      : type === "CREATE_INSTRUCTION_DRAFT"
        ? {
            agentRef: helperPins.projectAssistantRef,
            instructions:
              "Помогай участникам проекта и проверяй результат перед завершением задачи.",
          }
        : {
            agentRef: helperPins.projectAssistantRef,
            environmentRef: "env_project_helper_next",
            versionRef: "envver_project_helper_next",
          };
  const parameters = { ...helperPins, ...fields };
  const operation: AssistantPlanOperation = {
    ref: "operation_project_helper",
    type,
    action: "UPDATE",
    title:
      kind === "ENV"
        ? "Подготовить окружение помощника проекта"
        : kind === "BIND"
          ? "Назначить окружение помощнику проекта"
          : "Подготовить инструкции помощника проекта",
    summary: "Настройка только после явного подтверждения",
    target: {
      kind: kind === "ENV" ? "ENVIRONMENT" : "AGENT",
      ref:
        kind === "ENV" ? "env_project_helper" : helperPins.projectAssistantRef,
      version: kind === "ENV" ? 5 : 3,
      name: "Помощник проекта",
    },
    expectedVersion: kind === "ENV" ? 5 : 3,
    parameters,
    before: {
      ...parameters,
      ...(kind === "BIND"
        ? {
            environmentRef: "env_project_helper",
            environmentName: "Рабочая среда проектного помощника",
            versionRef: helperPins.runtimeEnvironmentVersionRef,
            bindingRef: helperPins.runtimeEnvironmentBindingRef,
          }
        : {}),
      ...(kind === "ENV"
        ? {
            versionRef: helperPins.runtimeEnvironmentVersionRef,
            versionDigest: helperPins.runtimeEnvironmentDigest,
            specification: { Tools: [], Values: [], SecretBindings: [] },
            policyInput: helperPolicy,
          }
        : {}),
    },
    after:
      kind === "BIND"
        ? Object.fromEntries(
            Object.entries(parameters).filter(([key]) => key !== "agentRef"),
          )
        : structuredClone(parameters),
    selected: true,
    permitted: true,
    validationProblems: [],
  };
  if (tamper === "project") operation.after.projectRef = "prj_foreign_helper";
  if (tamper === "organization")
    operation.before.organizationRef = "org_foreign_helper";
  if (tamper === "locator") operation.parameters.projectAssistantRef = "";
  const now = "2026-10-04T00:00:00Z";
  const plan: AssistantPlan = {
    ref: "plan_project_helper",
    conversationRef: "conv_system_helper",
    ...(sourceProject ? { projectRef: sourceProject } : {}),
    version: 2,
    revision: 1,
    state: applied ? "APPLIED" : "DRAFT",
    applied,
    contentDigest: "b".repeat(64),
    auditSummary:
      "Настроить помощника выбранного проекта из общесистемного диалога",
    operations: [operation],
    validationProblems: [],
    nextActions: [],
  };
  if (applied)
    plan.receipt = {
      ref: "receipt_project_helper",
      planRef: plan.ref,
      planRevision: plan.revision,
      outcome: "APPLIED",
      operationReceipts: [
        {
          operationRef: operation.ref,
          resourceRef:
            tamper === "receipt"
              ? "agt_foreign_helper"
              : kind === "ENV"
                ? "envdraft_project_helper"
                : helperPins.projectAssistantRef,
          outcome: "APPLIED",
          auditRef: "audit_project_helper",
        },
      ],
      conflicts: [],
      auditRefs: ["audit_project_helper"],
      createdResourceRefs: [],
      createdAt: now,
    };
  return plan;
}
