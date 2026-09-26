<script setup lang="ts">
import { computed, ref, watch } from "vue";

import { assistantCreatedWorkflowTarget } from "@/features/assistant/model";
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
let refresh: (() => Promise<void>) | undefined;

watch(
  target,
  (value, _previous, onCleanup) => {
    workflow.value = undefined;
    loading.value = false;
    problem.value = false;
    if (!value) return;
    const controller = new AbortController();
    onCleanup(() => {
      controller.abort();
      refresh = undefined;
    });
    refresh = async () => {
      if (controller.signal.aborted) return;
      loading.value = true;
      try {
        const next = (
          await unwrap(
            getWorkflow({
              path: { workflowRef: value.workflowRef },
              signal: requestSignal(controller.signal),
            }),
          )
        ).data;
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (controller.signal.aborted) return;
        if (
          next.ref !== value.workflowRef ||
          next.projectRef !== value.projectRef
        )
          throw new Error("Assistant workflow readback mismatch");
        workflow.value = next;
        problem.value = false;
      } catch {
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (!controller.signal.aborted) problem.value = true;
      } finally {
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (!controller.signal.aborted) loading.value = false;
      }
    };
    void refresh();
  },
  { immediate: true },
);
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
        @click="refresh?.()"
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

<style scoped>
.assistant-workflow-card {
  display: grid;
  gap: 8px;
  min-width: 0;
  margin-top: 10px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.assistant-workflow-card header,
.assistant-workflow-card__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
.assistant-workflow-card p {
  margin: 0;
  overflow-wrap: anywhere;
}
</style>
