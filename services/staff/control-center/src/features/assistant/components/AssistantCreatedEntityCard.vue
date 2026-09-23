<script setup lang="ts">
import { computed, ref, watch } from "vue";

import { assistantCreatedEntityTarget } from "@/features/assistant/model";
import { requestSignal } from "@/shared/api/client";
import { getAgent, getProject } from "@/shared/api/generated/openapi/sdk.gen";
import type {
  Agent,
  AssistantPlan,
  Project,
} from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{ plan: AssistantPlan; operationRef: string }>();
const emit = defineEmits<{ navigate: [] }>();
const target = computed(() =>
  assistantCreatedEntityTarget(props.plan, props.operationRef),
);
const entity = ref<Project | Agent>();
const loading = ref(false);
const problem = ref(false);
const state = computed(() => {
  const current = entity.value;
  if (!current) return undefined;
  return "lifecycle" in current ? current.lifecycle : current.state;
});
const destination = computed(() => {
  const current = target.value;
  if (!current) return undefined;
  return current.kind === "PROJECT"
    ? { name: "project", params: { projectRef: current.projectRef } }
    : {
        name: "agent",
        params: {
          projectRef: current.projectRef,
          agentRef: current.resourceRef,
        },
      };
});
let refresh: (() => Promise<void>) | undefined;

watch(
  target,
  (value, _previous, onCleanup) => {
    entity.value = undefined;
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
        const next =
          value.kind === "PROJECT"
            ? (
                await unwrap(
                  getProject({
                    path: { projectRef: value.resourceRef },
                    signal: requestSignal(controller.signal),
                  }),
                )
              ).data
            : (
                await unwrap(
                  getAgent({
                    path: { agentRef: value.resourceRef },
                    signal: requestSignal(controller.signal),
                  }),
                )
              ).data;
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (controller.signal.aborted) return;
        if (
          next.ref !== value.resourceRef ||
          (value.kind === "AGENT" &&
            (!("projectRef" in next) || next.projectRef !== value.projectRef))
        )
          throw new Error("Assistant created entity readback mismatch");
        entity.value = next;
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
  <section v-if="target" class="assistant-created-entity" aria-live="polite">
    <header>
      <strong>{{ $t(`assistant.createdEntity.${target.kind}.title`) }}</strong>
      <StatusBadge v-if="state" :state="state" />
    </header>
    <p v-if="loading && !entity">{{ $t("common.loading") }}</p>
    <p v-if="problem" class="field-error" role="alert">
      {{ $t("assistant.createdEntity.loadFailed") }}
    </p>
    <template v-if="entity">
      <p>{{ entity.name }}</p>
      <p>{{ $t(`assistant.createdEntity.${target.kind}.next`) }}</p>
    </template>
    <div class="assistant-created-entity__actions">
      <button
        class="button"
        type="button"
        :disabled="loading"
        @click="refresh?.()"
      >
        {{ $t("common.refresh") }}
      </button>
      <RouterLink
        v-if="entity && destination"
        class="button button--primary"
        :to="destination"
        @click="emit('navigate')"
        >{{ $t(`assistant.createdEntity.${target.kind}.open`) }}</RouterLink
      >
    </div>
  </section>
</template>

<style scoped>
.assistant-created-entity {
  display: grid;
  gap: 8px;
  min-width: 0;
  margin-top: 10px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.assistant-created-entity header,
.assistant-created-entity__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
.assistant-created-entity p {
  margin: 0;
  overflow-wrap: anywhere;
}
</style>
