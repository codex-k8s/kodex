<script setup lang="ts">
import { computed, ref, watch } from "vue";

import { assistantCreatedEntityTarget } from "@/features/assistant/model";
import { usePlatformStore } from "@/features/platform/store";
import { requestSignal } from "@/shared/api/client";
import {
  getAgent,
  getProject,
  getProjectAssistant,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  Agent,
  AssistantPlan,
  Project,
} from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{ plan: AssistantPlan; operationRef: string }>();
const platform = usePlatformStore();
const target = computed(() =>
  assistantCreatedEntityTarget(props.plan, props.operationRef),
);
const cachedEntity = computed<Project | Agent | undefined>(() => {
  const current = target.value;
  if (!current || current.kind === "PROJECT_ASSISTANT") return undefined;
  return current.kind === "PROJECT"
    ? platform.projects[current.resourceRef]
    : platform.agents[current.resourceRef];
});
const updated = computed(() =>
  props.plan.operations.some(
    (operation) =>
      operation.ref === props.operationRef &&
      (operation.type === "UPDATE_PROJECT" ||
        operation.type === "UPDATE_AGENT"),
  ),
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
  const confirmed = entity.value;
  if (!current || !confirmed) return undefined;
  return current.kind === "PROJECT"
    ? {
        name: "project",
        params: { projectRef: current.projectRef },
        query: { assistantForm: "1" },
      }
    : {
        name: "agent",
        params: {
          projectRef: current.projectRef,
          agentRef: confirmed.ref,
        },
        query: { assistantForm: "1" },
      };
});
let refresh: (() => Promise<void>) | undefined;

watch(
  [target, cachedEntity],
  ([value, cached], _previous, onCleanup) => {
    entity.value = undefined;
    loading.value = false;
    problem.value = false;
    if (!value) return;
    if (cached) {
      entity.value = cached;
      refresh = undefined;
    }
    const controller = new AbortController();
    onCleanup(() => {
      controller.abort();
      refresh = undefined;
    });
    refresh = async () => {
      if (controller.signal.aborted) return;
      loading.value = true;
      if (value.kind === "PROJECT_ASSISTANT") entity.value = undefined;
      try {
        let resourceRef = value.resourceRef;
        if (value.kind === "PROJECT_ASSISTANT") {
          const profile = (
            await unwrap(
              getProjectAssistant({
                path: { projectRef: value.projectRef },
                signal: requestSignal(controller.signal),
              }),
            )
          ).data;
          if (
            profile.ref !== value.resourceRef ||
            profile.projectRef !== value.projectRef
          )
            throw new Error("Assistant profile readback mismatch");
          resourceRef = profile.agentRef;
        }
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
                    path: { agentRef: resourceRef },
                    signal: requestSignal(controller.signal),
                  }),
                )
              ).data;
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (controller.signal.aborted) return;
        if (
          next.ref !== resourceRef ||
          (value.kind !== "PROJECT" &&
            (!("projectRef" in next) || next.projectRef !== value.projectRef))
        )
          throw new Error("Assistant entity readback mismatch");
        entity.value = next;
        if (value.kind === "PROJECT")
          platform.projects[next.ref] = next as Project;
        else platform.agents[next.ref] = next as Agent;
        problem.value = false;
      } catch {
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (!controller.signal.aborted) problem.value = true;
      } finally {
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (!controller.signal.aborted) loading.value = false;
      }
    };
    if (!cached) void refresh();
  },
  { immediate: true },
);
</script>

<template>
  <section v-if="target" class="assistant-created-entity" aria-live="polite">
    <header>
      <strong>{{
        $t(
          `assistant.createdEntity.${target.kind}.${updated ? "updatedTitle" : "title"}`,
        )
      }}</strong>
      <StatusBadge v-if="state" :state="state" />
    </header>
    <p v-if="loading && !entity">{{ $t("common.loading") }}</p>
    <p v-if="problem" class="field-error" role="alert">
      {{ $t("assistant.createdEntity.loadFailed") }}
    </p>
    <template v-if="entity">
      <p>{{ entity.name }}</p>
      <p>
        {{
          $t(
            `assistant.createdEntity.${target.kind}.${updated ? "updatedNext" : "next"}`,
          )
        }}
      </p>
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
