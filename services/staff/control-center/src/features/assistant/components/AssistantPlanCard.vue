<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import type { AssistantPlanOperation } from "@/shared/api/generated/openapi/types.gen";

const props = defineProps<{ operations: AssistantPlanOperation[] }>();
const { t } = useI18n();
const groups = computed(() =>
  [props.operations.slice(0, 5), props.operations.slice(5)].filter(
    (group) => group.length,
  ),
);
const selectedCount = computed(
  () => props.operations.filter((operation) => operation.selected).length,
);
const problemCount = computed(
  () =>
    props.operations.filter(
      (operation) =>
        !operation.permitted || operation.validationProblems.length > 0,
    ).length,
);
function field(operation: AssistantPlanOperation, key: string): string {
  for (const source of [
    operation.parameters,
    operation.after,
    operation.before,
  ]) {
    const value = source[key];
    if (typeof value === "string" && value.trim()) return value.trim();
  }
  return "";
}
function grant(operation: AssistantPlanOperation) {
  if (
    ![
      "CHANGE_INTEGRATION_GRANT",
      "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT",
      "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT",
    ].includes(operation.type)
  )
    return;
  const enabled = operation.parameters.enabled;
  const policy = operation.parameters.approvalPolicy;
  return {
    capability: field(operation, "capabilityKey") || operation.title,
    recipient:
      field(operation, "recipientName") || field(operation, "agentName"),
    connection: operation.target.name,
    enabled:
      typeof enabled === "boolean"
        ? t(
            enabled
              ? "assistant.planEditor.grantEnableShort"
              : "assistant.planEditor.grantDisableShort",
          )
        : "",
    policy:
      typeof policy === "string" &&
      ["NONE", "HUMAN_EACH_EFFECT", "HUMAN_SCOPED"].includes(policy)
        ? t(`integrations.approvalPolicies.${policy}`)
        : "",
  };
}
</script>

<template>
  <section class="plan-summary-list">
    <small>{{
      $t("assistant.planEditor.cardSelection", {
        selected: selectedCount,
        count: operations.length,
      })
    }}</small>
    <p v-if="problemCount" class="plan-summary-list__problem" role="alert">
      {{ $t("assistant.planEditor.cardProblems", { count: problemCount }) }}
    </p>
    <component
      :is="index ? 'details' : 'div'"
      v-for="(group, index) in groups"
      :key="index"
      class="plan-summary-list__group"
    >
      <summary v-if="index">
        {{ $t("assistant.planEditor.cardMore", { count: group.length }) }}
      </summary>
      <ol :start="index ? 6 : 1" class="assistant-plan-card__operations">
        <li
          v-for="operation in group"
          :key="operation.ref"
          :data-operation-ref="operation.ref"
        >
          <template v-if="grant(operation)">
            <strong class="plan-summary-list__capability">{{
              grant(operation)?.capability
            }}</strong>
            <span>{{
              [grant(operation)?.recipient, grant(operation)?.connection]
                .filter(Boolean)
                .join(" · ")
            }}</span>
            <small>{{
              [grant(operation)?.enabled, grant(operation)?.policy]
                .filter(Boolean)
                .join(" · ")
            }}</small>
          </template>
          <slot v-else name="operation" :operation="operation" />
          <small v-if="!operation.selected">{{
            $t("assistant.planEditor.cardNotSelected")
          }}</small>
          <p
            v-if="!operation.permitted || operation.validationProblems.length"
            class="plan-summary-list__problem"
          >
            {{ $t("assistant.planEditor.cardOperationProblem") }}
          </p>
        </li>
      </ol>
    </component>
  </section>
</template>

<style scoped>
.plan-summary-list {
  display: grid;
  gap: 8px;
  min-width: 0;
}
.plan-summary-list > small {
  color: var(--muted);
}
.plan-summary-list summary {
  padding: 8px 0;
  cursor: pointer;
  color: var(--accent);
}
.plan-summary-list summary:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
.assistant-plan-card__operations {
  display: grid;
  gap: 6px;
  margin: 0;
  padding: 0;
  list-style: none;
  font-size: 0.84rem;
}
.assistant-plan-card__operations li {
  display: grid;
  gap: 3px;
  min-width: 0;
  padding: 8px 10px;
  border-left: 3px solid var(--accent);
  background: var(--panel);
  overflow-wrap: anywhere;
}
.assistant-plan-card__operations span,
.assistant-plan-card__operations small {
  color: var(--muted);
}
.plan-summary-list__problem {
  margin: 0;
  color: var(--danger);
  font-size: 0.8rem;
}
</style>
