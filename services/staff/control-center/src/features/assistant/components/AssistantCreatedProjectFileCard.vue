<script setup lang="ts">
import { computed, ref, watch } from "vue";

import { assistantCreatedProjectFileTarget } from "@/features/assistant/model";
import { requestSignal } from "@/shared/api/client";
import { getArtifact } from "@/shared/api/generated/openapi/sdk.gen";
import type {
  Artifact,
  AssistantPlan,
} from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{ plan: AssistantPlan; operationRef: string }>();
const target = computed(() =>
  assistantCreatedProjectFileTarget(props.plan, props.operationRef),
);
const artifact = ref<Artifact>();
const loading = ref(false);
const problem = ref(false);
let refresh: (() => Promise<void>) | undefined;

watch(
  target,
  (value, _previous, onCleanup) => {
    artifact.value = undefined;
    problem.value = false;
    if (!value) return;
    const controller = new AbortController();
    onCleanup(() => {
      controller.abort();
      refresh = undefined;
    });
    refresh = async () => {
      loading.value = true;
      try {
        const next = (
          await unwrap(
            getArtifact({
              path: { artifactRef: value.artifactRef },
              signal: requestSignal(controller.signal),
            }),
          )
        ).data;
        if (controller.signal.aborted) return;
        if (
          next.ref !== value.artifactRef ||
          next.projectRef !== value.projectRef
        )
          throw new Error("Assistant file readback mismatch");
        artifact.value = next;
        problem.value = false;
      } catch {
        if (!controller.signal.aborted) problem.value = true;
      } finally {
        if (!controller.signal.aborted) loading.value = false;
      }
    };
    void refresh();
  },
  { immediate: true },
);
</script>

<template>
  <section v-if="target" class="assistant-created-file" aria-live="polite">
    <header>
      <strong>{{ $t("assistant.createdFile.title") }}</strong>
      <StatusBadge v-if="artifact" :state="artifact.lifecycleState" />
    </header>
    <p v-if="loading && !artifact">{{ $t("common.loading") }}</p>
    <p v-if="problem" class="field-error" role="alert">
      {{ $t("assistant.createdFile.loadFailed") }}
    </p>
    <template v-if="artifact">
      <p>
        <strong>{{ artifact.fileName }}</strong>
      </p>
      <p>{{ $t("assistant.createdFile.next") }}</p>
    </template>
    <div class="assistant-created-file__actions">
      <button
        class="button"
        type="button"
        :disabled="loading"
        @click="refresh?.()"
      >
        {{ $t("common.refresh") }}
      </button>
      <RouterLink
        v-if="artifact"
        class="button button--primary"
        :to="{
          name: 'files',
          params: { projectRef: target.projectRef },
          query: { artifactRef: target.artifactRef },
        }"
      >
        {{ $t("assistant.createdFile.open") }}
      </RouterLink>
    </div>
  </section>
</template>

<style scoped>
.assistant-created-file {
  display: grid;
  gap: 8px;
  min-width: 0;
  margin-top: 10px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.assistant-created-file header,
.assistant-created-file__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
.assistant-created-file p {
  margin: 0;
  overflow-wrap: anywhere;
}
</style>
