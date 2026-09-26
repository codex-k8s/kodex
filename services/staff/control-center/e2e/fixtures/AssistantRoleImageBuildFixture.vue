<script setup lang="ts">
import AssistantRoleImageBuildCard from "../../src/features/assistant/components/AssistantRoleImageBuildCard.vue";
import type { AssistantPlan } from "../../src/shared/api/generated/openapi/types.gen";

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
      type: "CREATE_ROLE_IMAGE_RECIPE",
      action: "CREATE",
      title: "Подготовить образ",
      summary: "Подготовить проверочный образ",
      target: { kind: "ROLE_IMAGE_RECIPE", name: "Проверочный образ" },
      parameters: {},
      before: {},
      after: {},
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
</script>

<template>
  <main class="assistant-image-fixture">
    <AssistantRoleImageBuildCard
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
