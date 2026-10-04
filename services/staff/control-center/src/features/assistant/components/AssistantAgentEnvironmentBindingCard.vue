<script setup lang="ts">
import { computed, ref, watch } from "vue";

import { loadAgentRuntime } from "@/features/agents/detail/runtime-api";
import { assistantAgentEnvironmentBindingTarget } from "@/features/assistant/model";
import { readAssistantProjectHelper } from "@/features/assistant/project-helper-readback";
import { usePlatformStore } from "@/features/platform/store";
import { assertRuntimeResourceIdentity } from "@/features/runtime/resource-scope";
import type {
  AssistantPlan,
  AgentRuntimeConfigurationView,
} from "@/shared/api/generated/openapi/types.gen";

const props = defineProps<{ plan: AssistantPlan; operationRef: string }>();
const platform = usePlatformStore();
const target = computed(() =>
  assistantAgentEnvironmentBindingTarget(
    props.plan,
    props.operationRef,
    platform.bootstrap?.organizationRef,
  ),
);
const view = ref<AgentRuntimeConfigurationView>();
const loading = ref(false);
const problem = ref(false);
const current = computed(
  () =>
    target.value &&
    view.value &&
    view.value.environmentBinding.agentRef === target.value.agentRef &&
    view.value.environmentBinding.environmentRef ===
      target.value.environmentRef &&
    view.value.environmentBinding.versionRef === target.value.versionRef,
);
let refresh: (() => Promise<void>) | undefined;

watch(
  [target, () => platform.bootstrap?.organizationRef],
  ([value, organizationRef], _previous, onCleanup) => {
    view.value = undefined;
    problem.value = false;
    if (!value || !organizationRef) return;
    const controller = new AbortController();
    onCleanup(() => {
      controller.abort();
      refresh = undefined;
    });
    refresh = async () => {
      if (controller.signal.aborted) return;
      loading.value = true;
      try {
        const operation = props.plan.operations.find(
          (item) => item.ref === props.operationRef,
        );
        if (!operation)
          throw new Error("Assistant binding operation is missing");
        await readAssistantProjectHelper(
          props.plan,
          operation,
          organizationRef,
          controller.signal,
        );
        const next = await loadAgentRuntime(value.agentRef, controller.signal);
        if (
          next.configuration.agentRef !== value.agentRef ||
          next.environmentBinding.agentRef !== value.agentRef
        )
          throw new Error("Assistant binding agent readback mismatch");
        assertRuntimeResourceIdentity(
          { kind: "PROJECT", projectRef: value.projectRef },
          next.environment,
          organizationRef,
        );
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
  },
  { immediate: true },
);
</script>

<template>
  <section
    v-if="target"
    class="assistant-agent-environment-binding-card"
    aria-live="polite"
  >
    <strong>{{ $t("assistant.bindingCard.title") }}</strong>
    <p v-if="loading && !view">{{ $t("common.loading") }}</p>
    <p v-else-if="problem" class="field-error" role="alert">
      {{ $t("assistant.bindingCard.loadFailed") }}
    </p>
    <p v-else-if="current">
      {{
        $t("assistant.bindingCard.bound", {
          environment: view?.environment.name,
        })
      }}
    </p>
    <p v-else-if="view">{{ $t("assistant.bindingCard.changed") }}</p>
    <div class="assistant-agent-environment-binding-card__actions">
      <button
        class="button"
        type="button"
        :disabled="loading"
        @click="refresh?.()"
      >
        {{ $t("common.refresh") }}
      </button>
      <RouterLink
        v-if="view && !problem"
        class="button button--primary"
        :to="{
          name: 'agent',
          params: { projectRef: target.projectRef, agentRef: target.agentRef },
          query: { tab: 'environment', assistantForm: '1' },
        }"
      >
        {{ $t("assistant.bindingCard.open") }}
      </RouterLink>
    </div>
  </section>
</template>

<style scoped>
.assistant-agent-environment-binding-card {
  display: grid;
  gap: 8px;
  margin-top: 10px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.assistant-agent-environment-binding-card__actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
</style>
