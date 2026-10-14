<script setup lang="ts">
import { computed, ref, watch } from "vue";
import type {
  ArtifactRevision,
  AssistantPlan,
} from "@/shared/api/generated/openapi/types.gen";
import { ownerRequestSignal } from "@/shared/api/owner-lifetime";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { appliedProjectFileRevision } from "../project-file-plan";
import { readAppliedProjectFileRevision } from "../project-file-readback";

const props = defineProps<{ plan: AssistantPlan; operationRef: string }>();
const target = computed(() =>
  appliedProjectFileRevision(props.plan, props.operationRef),
);
const revision = ref<ArtifactRevision>();
const loading = ref(false);
const problem = ref(false);
let refresh: (() => Promise<void>) | undefined;
watch(
  target,
  (value, _previous, onCleanup) => {
    revision.value = undefined;
    problem.value = false;
    loading.value = false;
    refresh = undefined;
    if (!value) return;
    const controller = new AbortController();
    const signal = AbortSignal.any([controller.signal, ownerRequestSignal()]);
    const clear = () => {
      revision.value = undefined;
      problem.value = false;
      loading.value = false;
    };
    signal.addEventListener("abort", clear, { once: true });
    onCleanup(() => {
      signal.removeEventListener("abort", clear);
      controller.abort();
      refresh = undefined;
    });
    refresh = async () => {
      const closed = () => signal.aborted;
      if (loading.value || closed()) return;
      loading.value = true;
      try {
        const next = await readAppliedProjectFileRevision(
          value.projectRef,
          value.revision,
          signal,
        );
        if (closed()) return;
        revision.value = next;
        problem.value = false;
      } catch {
        if (!closed()) {
          revision.value = undefined;
          problem.value = true;
        }
      } finally {
        if (!closed()) loading.value = false;
      }
    };
    void refresh();
  },
  { immediate: true },
);
</script>
<template>
  <section v-if="target" class="assistant-file-revision" aria-live="polite">
    <header>
      <strong>{{
        $t(revision ? "fileRevision.applied" : "fileRevision.title")
      }}</strong
      ><StatusBadge v-if="revision" :state="revision.scanState" />
    </header>
    <p v-if="loading" role="status">{{ $t("common.loading") }}</p>
    <p v-if="problem" class="field-error" role="alert">
      {{ $t("fileRevision.inspectFailed") }}
    </p>
    <template v-if="revision">
      <p>
        <strong>{{ revision.fileName }}</strong> ·
        {{ $t("fileRevision.number", { revision: revision.revision }) }} ·
        {{ $t("fileRevision.bytes", { count: revision.sizeBytes }) }}
      </p>
      <details>
        <summary>{{ $t("common.details") }}</summary>
        <code>{{ revision.digest }}</code>
      </details>
    </template>
    <div class="assistant-file-revision__actions">
      <button
        class="button"
        type="button"
        :disabled="loading"
        @click="refresh?.()"
      >
        {{ $t("common.refresh") }}
      </button>
      <RouterLink
        v-if="revision"
        class="button button--primary"
        :to="{
          name: 'files',
          params: { projectRef: target.projectRef },
          query: { artifactRef: revision.artifactRef },
        }"
        >{{ $t("assistant.createdFile.open") }}</RouterLink
      >
    </div>
  </section>
</template>
<style scoped>
.assistant-file-revision {
  display: grid;
  gap: 7px;
  min-width: 0;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 8px;
}
.assistant-file-revision header,
.assistant-file-revision__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 7px;
}
.assistant-file-revision p {
  margin: 0;
}
.assistant-file-revision p,
.assistant-file-revision code {
  overflow-wrap: anywhere;
}
.assistant-file-revision .button {
  min-height: 32px;
}
@media (max-width: 720px), (pointer: coarse) {
  .assistant-file-revision .button {
    min-height: 44px;
  }
}
</style>
