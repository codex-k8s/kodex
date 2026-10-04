<script setup lang="ts">
import { computed, ref, watch } from "vue";

import { requestSignal } from "@/shared/api/client";
import { getAgent } from "@/shared/api/generated/openapi/sdk.gen";
import type {
  Agent,
  AssistantPlan,
} from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";
import {
  assistantInstructionDraftTarget,
  assistantProjectHelperScope,
} from "@/features/assistant/model";
import { readAssistantProjectHelper } from "@/features/assistant/project-helper-readback";
import { usePlatformStore } from "@/features/platform/store";

const props = defineProps<{ plan: AssistantPlan; operationRef: string }>();
const platform = usePlatformStore();
const target = computed(() =>
  assistantInstructionDraftTarget(
    props.plan,
    props.operationRef,
    platform.bootstrap?.organizationRef,
  ),
);
const operation = computed(() =>
  props.plan.operations.find(
    (item) =>
      item.ref === props.operationRef &&
      item.type === "CREATE_INSTRUCTION_DRAFT",
  ),
);
const projectHelper = computed(
  () =>
    operation.value &&
    assistantProjectHelperScope(
      props.plan,
      operation.value,
      platform.bootstrap?.organizationRef,
    ),
);
const agent = ref<Agent>();
const loading = ref(false);
const problem = ref(false);
const currentDraft = computed(
  () =>
    props.plan.state === "APPLIED" &&
    agent.value?.draftInstructions?.content ===
      operation.value?.parameters.instructions,
);
let refresh: (() => Promise<void>) | undefined;

watch(
  [operation, target, () => platform.bootstrap?.organizationRef],
  ([value, exact, organizationRef], _previous, onCleanup) => {
    agent.value = undefined;
    loading.value = false;
    problem.value = false;
    if (!value || !exact) return;
    const agentRef = exact.agentRef;
    const controller = new AbortController();
    onCleanup(() => {
      controller.abort();
      refresh = undefined;
    });
    refresh = async () => {
      if (controller.signal.aborted) return;
      loading.value = true;
      try {
        const helper = await readAssistantProjectHelper(
          props.plan,
          value,
          organizationRef,
          controller.signal,
        );
        const next =
          helper ??
          (
            await unwrap(
              getAgent({
                path: { agentRef },
                signal: requestSignal(controller.signal),
              }),
            )
          ).data;
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (controller.signal.aborted) return;
        if (next.ref !== agentRef || next.projectRef !== exact.projectRef)
          throw new Error("Assistant instruction draft readback mismatch");
        agent.value = next;
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
  <section
    v-if="operation && target"
    class="instruction-draft-card"
    aria-live="polite"
  >
    <strong>{{
      $t(
        projectHelper
          ? "assistant.instructionDraft.helperTitle"
          : "assistant.instructionDraft.title",
      )
    }}</strong>
    <p v-if="loading && !agent">{{ $t("common.loading") }}</p>
    <p v-if="problem" class="field-error" role="alert">
      {{ $t("assistant.instructionDraft.loadFailed") }}
    </p>
    <p v-if="agent">
      {{
        $t(
          projectHelper && !currentDraft
            ? "assistant.instructionDraft.helperChanged"
            : currentDraft
              ? "assistant.instructionDraft.saved"
              : "assistant.instructionDraft.changed",
        )
      }}
    </p>
    <div class="instruction-draft-card__actions">
      <button
        class="button"
        type="button"
        :disabled="loading"
        @click="refresh?.()"
      >
        {{ $t("common.refresh") }}
      </button>
      <RouterLink
        v-if="agent && !problem"
        class="button button--primary"
        :to="{
          name: 'agent',
          params: {
            projectRef: target.projectRef,
            agentRef: target.agentRef,
          },
          query: { tab: 'instructions', assistantForm: '1' },
        }"
        >{{ $t("assistant.instructionDraft.open") }}</RouterLink
      >
    </div>
  </section>
</template>

<style scoped>
.instruction-draft-card {
  display: grid;
  gap: 8px;
  min-width: 0;
  margin-top: 10px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.instruction-draft-card p {
  margin: 0;
}
.instruction-draft-card__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
</style>
