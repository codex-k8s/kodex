<script setup lang="ts">
import { computed, ref } from "vue";

import { assistantCreatedWorkflowTarget } from "@/features/assistant/model";
import { watchAssistantReadback } from "@/features/assistant/readback-lifecycle";
import { requestSignal } from "@/shared/api/client";
import { getWorkflow } from "@/shared/api/generated/openapi/sdk.gen";
import type {
  AssistantPlan,
  Workflow,
} from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{ plan: AssistantPlan; operationRef: string }>();
const target = computed(() =>
  assistantCreatedWorkflowTarget(props.plan, props.operationRef),
);
const workflow = ref<Workflow>();
const loading = ref(false);
const problem = ref(false);
const refresh = watchAssistantReadback(target, {
  loading,
  problem,
  reset: () => {
    workflow.value = undefined;
  },
  read: async (value, signal) =>
    (
      await unwrap(
        getWorkflow({
          path: { workflowRef: value.workflowRef },
          signal: requestSignal(signal),
        }),
      )
    ).data,
  apply: (value, next) => {
    if (next.ref !== value.workflowRef || next.projectRef !== value.projectRef)
      throw new Error("Assistant workflow readback mismatch");
    workflow.value = next;
  },
});
</script>

<template>
  <section v-if="target" class="assistant-workflow-card" aria-live="polite">
    <header>
      <strong>{{
        $t(
          plan.operations.find((item) => item.ref === operationRef)?.type ===
            "UPDATE_WORKFLOW"
            ? "assistant.createdWorkflow.updatedTitle"
            : "assistant.createdWorkflow.title",
        )
      }}</strong>
      <StatusBadge v-if="workflow" :state="workflow.state" />
    </header>
    <p v-if="loading && !workflow">{{ $t("common.loading") }}</p>
    <p v-if="problem" class="field-error" role="alert">
      {{ $t("assistant.createdWorkflow.loadFailed") }}
    </p>
    <template v-if="workflow">
      <p>{{ workflow.name }}</p>
      <p v-if="workflow.launchReadiness.allowedToSubmit">
        {{ $t("assistant.createdWorkflow.ready") }}
      </p>
      <p v-else>{{ $t("assistant.createdWorkflow.finish") }}</p>
    </template>
    <div class="assistant-workflow-card__actions">
      <button
        class="button"
        type="button"
        :disabled="loading"
        @click="refresh()"
      >
        {{ $t("common.refresh") }}
      </button>
      <RouterLink
        class="button button--primary"
        :to="{
          name: 'workflow',
          params: {
            projectRef: target.projectRef,
            workflowRef: target.workflowRef,
          },
          query: { assistantForm: '1' },
        }"
        >{{ $t("assistant.createdWorkflow.open") }}</RouterLink
      >
    </div>
  </section>
</template>

<style scoped src="./assistant-result-card.css"></style>
