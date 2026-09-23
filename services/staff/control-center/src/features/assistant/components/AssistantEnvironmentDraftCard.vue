<script setup lang="ts">
import { computed, ref, watch } from "vue";

import AssistantEnvironmentBindingDialog from "@/features/assistant/components/AssistantEnvironmentBindingDialog.vue";
import { assistantEnvironmentDraftTarget } from "@/features/assistant/model";
import { readEnvironmentDraft } from "@/features/runtime/environment-drafts";
import type {
  AssistantPlan,
  RuntimeEnvironmentDraft,
} from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{ plan: AssistantPlan; operationRef: string }>();
const emit = defineEmits<{ navigate: [] }>();
const target = computed(() =>
  assistantEnvironmentDraftTarget(props.plan, props.operationRef),
);
const draft = ref<RuntimeEnvironmentDraft>();
const loading = ref(false);
const problem = ref(false);
const bindingOpen = ref(false);
const boundAgentName = ref("");
const destination = computed(() => {
  const exact = target.value;
  const current = draft.value;
  if (!exact || !current || current.state === "DISCARDED") return;
  if (current.state === "PUBLISHED") {
    if (!current.publishedEnvironmentRef) return;
    return {
      name: "runtime-environment",
      params: {
        projectRef: exact.projectRef,
        environmentRef: current.publishedEnvironmentRef,
      },
    };
  }
  return {
    name: "runtime-environment-new",
    params: { projectRef: exact.projectRef },
    query: { draftRef: exact.draftRef },
  };
});
let refresh: (() => Promise<void>) | undefined;

watch(
  target,
  (value, _previous, onCleanup) => {
    draft.value = undefined;
    loading.value = false;
    problem.value = false;
    bindingOpen.value = false;
    boundAgentName.value = "";
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
        const next = await readEnvironmentDraft(
          value.projectRef,
          value.draftRef,
          controller.signal,
        );
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (controller.signal.aborted) return;
        draft.value = next;
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
  <section v-if="target" class="assistant-environment-card" aria-live="polite">
    <header>
      <strong>{{ $t("assistant.environmentDraft.title") }}</strong>
      <StatusBadge v-if="draft" :state="draft.state" />
    </header>
    <p v-if="loading && !draft">{{ $t("common.loading") }}</p>
    <p v-if="problem" class="assistant-environment-card__problem" role="alert">
      {{ $t("assistant.environmentDraft.loadFailed") }}
    </p>
    <template v-if="draft">
      <p>{{ draft.specification.name }}</p>
      <p v-if="draft.state === 'PUBLISHED'">
        {{ $t("assistant.environmentDraft.published") }}
      </p>
      <p v-else-if="draft.state === 'INVALID'">
        {{ $t("assistant.environmentDraft.invalid") }}
      </p>
      <p v-else-if="draft.state === 'DISCARDED'">
        {{ $t("assistant.environmentDraft.discarded") }}
      </p>
      <p v-else>{{ $t("assistant.environmentDraft.incomplete") }}</p>
      <p v-if="boundAgentName">
        {{ $t("assistant.environmentDraft.bound", { agent: boundAgentName }) }}
      </p>
    </template>
    <div class="assistant-environment-card__actions">
      <button
        class="button"
        type="button"
        :disabled="loading"
        @click="refresh?.()"
      >
        {{ $t("common.refresh") }}
      </button>
      <RouterLink
        v-if="destination"
        class="button button--primary"
        :to="destination"
        @click="emit('navigate')"
      >
        {{ $t("assistant.environmentDraft.continue") }}
      </RouterLink>
      <button
        v-if="draft?.state === 'PUBLISHED' && draft.publishedEnvironmentRef"
        class="button button--primary"
        type="button"
        @click="bindingOpen = true"
      >
        {{ $t("assistant.environmentDraft.bind") }}
      </button>
    </div>
    <AssistantEnvironmentBindingDialog
      v-if="bindingOpen && target && draft?.publishedEnvironmentRef"
      :project-ref="target.projectRef"
      :environment-ref="draft.publishedEnvironmentRef"
      @close="bindingOpen = false"
      @bound="
        boundAgentName = $event;
        bindingOpen = false;
      "
    />
  </section>
</template>

<style scoped>
.assistant-environment-card {
  display: grid;
  gap: 8px;
  min-width: 0;
  margin-top: 10px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.assistant-environment-card header,
.assistant-environment-card__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
.assistant-environment-card p {
  margin: 0;
  overflow-wrap: anywhere;
}
.assistant-environment-card__problem {
  color: var(--danger);
}
</style>
