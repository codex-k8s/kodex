<script setup lang="ts">
import AssistantRoleImageBuildCard from "../../src/features/assistant/components/AssistantRoleImageBuildCard.vue";
import AssistantPlanEditor from "../../src/features/assistant/components/AssistantPlanEditor.vue";
import { ref } from "vue";
import { usePlatformStore } from "../../src/features/platform/store";
import type {
  AssistantPlan,
  BootstrapState,
  AssistantPlanOperationInput,
} from "../../src/shared/api/generated/openapi/types.gen";

usePlatformStore().bootstrap = {
  organizationRef: "org_synthetic",
  platformRole: "OWNER",
} as BootstrapState;
const organizationScope =
  new URLSearchParams(window.location.search).get("scope") === "ORGANIZATION";
const imageOwner = organizationScope
  ? {
      scopeKind: "ORGANIZATION",
      organizationRef: "org_synthetic",
      systemAssistantRef: "agt_system",
    }
  : {};
const editorMode = new URLSearchParams(window.location.search).get("editor");
const savedOperations = ref<AssistantPlanOperationInput[]>([]);

const plan: AssistantPlan = {
  ref: "plan_synthetic_image",
  version: 2,
  revision: 1,
  state: "APPLIED",
  conversationRef: "conversation_synthetic_image",
  projectRef: "project_synthetic_image",
  auditSummary: "Проверка восстановления образа",
  applied: true,
  contentDigest: "a".repeat(64),
  validationProblems: [],
  nextActions: [],
  operations: [
    {
      ref: "operation_synthetic_image",
      type: organizationScope
        ? "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE"
        : "CREATE_ROLE_IMAGE_RECIPE",
      action: "CREATE",
      title: "Подготовить образ",
      summary: "Подготовить проверочный образ",
      target: { kind: "ROLE_IMAGE_RECIPE", name: "Проверочный образ" },
      parameters: imageOwner,
      before: {},
      after: imageOwner,
      selected: true,
      permitted: true,
      validationProblems: [],
    },
  ],
  receipt: {
    ref: "receipt_synthetic_image",
    planRef: "plan_synthetic_image",
    planRevision: 1,
    outcome: "APPLIED",
    operationReceipts: [
      {
        operationRef: "operation_synthetic_image",
        resourceRef: "recipe_synthetic_image",
        outcome: "APPLIED",
        auditRef: "audit_synthetic_image",
      },
    ],
    conflicts: [],
    auditRefs: ["audit_synthetic_image"],
    createdResourceRefs: ["recipe_synthetic_image"],
    createdAt: "2026-09-25T00:00:00Z",
  },
};
const assistantOwner = {
  agentRef: "agent_synthetic",
  organizationRef: "org_synthetic",
  assistantScope: organizationScope ? "SYSTEM" : "PROJECT",
  scopeKind: organizationScope ? "ORGANIZATION" : "PROJECT",
  ...(organizationScope
    ? { projectRef: "", assistantProfileRef: "" }
    : {
        projectRef: "project_synthetic_image",
        assistantProfileRef: "asstp_synthetic",
      }),
};
const imageParameters = {
  ...imageOwner,
  name: "Проверочный образ",
  environmentKey: "standard",
  dockerfile: "FROM alpine:3.22\nRUN echo ready\n",
};
const runtimeParameters = {
  ...assistantOwner,
  runtimeProfileRef: "runtime_synthetic",
  runtimeProfilePin: {
    ref: "runtime_synthetic",
    version: 7,
    runtimeRevision: "runtime-revision",
  },
  model: "model-synthetic",
  reasoningEffort: "low",
  providerPolicyMode: editorMode === "runtime-multi" ? "LEAST_USED" : "FIXED",
  providerAccounts:
    editorMode === "runtime-multi"
      ? [
          { accountRef: "pacc_synthetic", weight: 1 },
          { accountRef: "pacc_secondary", weight: 1 },
        ]
      : [{ accountRef: "pacc_synthetic", weight: 1 }],
};
const firstOperation = plan.operations[0];
if (!firstOperation) throw new Error("Missing image operation fixture");
const editorPlan: AssistantPlan = {
  ...plan,
  state: "DRAFT",
  applied: false,
  receipt: undefined,
  operations: editorMode?.startsWith("runtime")
    ? [
        {
          ...firstOperation,
          type: "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION",
          title: "Настроить модель помощника",
          summary: "Обновить модель и степень рассуждений",
          action: "UPDATE",
          target: {
            kind: "AGENT",
            ref: "agent_synthetic",
            name: "Kodex",
            version: 3,
          },
          expectedVersion: 3,
          parameters: runtimeParameters,
          before: {
            ...assistantOwner,
            agentVersion: 3,
            runtimeProfilePin: { ...runtimeParameters.runtimeProfilePin },
          },
          after: structuredClone(runtimeParameters),
        },
      ]
    : [
        {
          ...firstOperation,
          parameters: imageParameters,
          after: imageParameters,
        },
      ],
};
</script>

<template>
  <main class="assistant-image-fixture">
    <AssistantPlanEditor
      v-if="editorMode"
      :plan="editorPlan"
      @save="(_summary, operations) => (savedOperations = operations)"
    />
    <pre hidden data-testid="saved-operations">{{
      JSON.stringify(savedOperations)
    }}</pre>
    <AssistantRoleImageBuildCard
      v-if="!editorMode"
      :plan="plan"
      operation-ref="operation_synthetic_image"
    />
  </main>
</template>

<style scoped>
.assistant-image-fixture {
  max-width: 980px;
  margin: 20px auto;
  padding: 12px;
}
</style>
