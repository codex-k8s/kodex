<script setup lang="ts">
import { computed, ref, watch } from "vue";

import { loadAgentRuntime } from "@/features/agents/detail/runtime-api";
import { assistantAgentEnvironmentBindingTarget } from "@/features/assistant/model";
import type { AssistantPlan, AgentRuntimeConfigurationView } from "@/shared/api/generated/openapi/types.gen";

const props = defineProps<{ plan: AssistantPlan; operationRef: string }>();
const emit = defineEmits<{ navigate: [] }>();
const target = computed(() => assistantAgentEnvironmentBindingTarget(props.plan, props.operationRef));
const view = ref<AgentRuntimeConfigurationView>();
const loading = ref(false);
const problem = ref(false);
const current = computed(() => target.value && view.value &&
  view.value.environmentBinding.agentRef === target.value.agentRef &&
  view.value.environmentBinding.environmentRef === target.value.environmentRef &&
  view.value.environmentBinding.versionRef === target.value.versionRef);
let refresh: (() => Promise<void>) | undefined;

watch(target, (value, _previous, onCleanup) => {
  view.value = undefined;
  problem.value = false;
  if (!value) return;
  const controller = new AbortController();
  onCleanup(() => { controller.abort(); refresh = undefined; });
  refresh = async () => {
    if (controller.signal.aborted) return;
    loading.value = true;
    try {
      const next = await loadAgentRuntime(value.agentRef, controller.signal);
      // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
      if (!controller.signal.aborted) view.value = next;
    } catch {
      // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
      if (!controller.signal.aborted) problem.value = true;
    } finally {
      // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
      if (!controller.signal.aborted) loading.value = false;
    }
  };
  void refresh();
}, { immediate: true });
</script>

<template>
  <section v-if="target" class="assistant-agent-environment-binding-card" aria-live="polite">
    <strong>{{ $t("assistant.bindingCard.title") }}</strong>
    <p v-if="loading && !view">{{ $t("common.loading") }}</p>
    <p v-else-if="problem" class="field-error" role="alert">{{ $t("assistant.bindingCard.loadFailed") }}</p>
    <p v-else-if="current">{{ $t("assistant.bindingCard.bound", { environment: view?.environment.name }) }}</p>
    <p v-else-if="view">{{ $t("assistant.bindingCard.changed") }}</p>
    <div class="assistant-agent-environment-binding-card__actions">
      <button class="button" type="button" :disabled="loading" @click="refresh?.()">{{ $t("common.refresh") }}</button>
      <RouterLink class="button button--primary" :to="{ name: 'agent', params: { projectRef: target.projectRef, agentRef: target.agentRef }, query: { tab: 'environment' } }" @click="emit('navigate')">
        {{ $t("assistant.bindingCard.open") }}
      </RouterLink>
    </div>
  </section>
</template>

<style scoped>
.assistant-agent-environment-binding-card { display: grid; gap: 8px; margin-top: 10px; padding: 12px; border: 1px solid var(--border); border-radius: 8px; background: var(--panel); }
.assistant-agent-environment-binding-card__actions { display: flex; gap: 8px; flex-wrap: wrap; }
</style>
