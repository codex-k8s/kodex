<script setup lang="ts">
import { computed, ref, watch } from "vue";

import { operationParameter, type EditablePlanOperation } from "@/features/assistant/model";
import { requestSignal } from "@/shared/api/client";
import {
  getAgent,
  getRuntimeEnvironmentSet,
  listRuntimeEnvironmentSets,
} from "@/shared/api/generated/openapi/sdk.gen";
import type { Agent, RuntimeEnvironmentSet } from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";
import type { AsyncEntityOptionPage } from "@/shared/ui/async-entity-picker";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";

const props = defineProps<{
  operation: EditablePlanOperation;
  projectRef?: string;
  disabled: boolean;
}>();
const emit = defineEmits<{
  valid: [value: boolean];
  dirty: [];
  parameter: [key: string, value: string];
}>();
const agent = ref<Agent>();
const environment = ref<RuntimeEnvironmentSet>();
const agentProblem = ref(false);
const environmentProblem = ref(false);
function stringField(key: string): string {
  try {
    const value = operationParameter(props.operation, key);
    return typeof value === "string" ? value : "";
  } catch {
    return "";
  }
}
const agentRef = computed(() => stringField("agentRef"));
const environmentRef = computed(() => stringField("environmentRef"));
const beforeEnvironmentName = computed(() => {
  const value = props.operation.value.before.environmentName;
  return typeof value === "string" ? value : "";
});
const valid = computed(() => Boolean(
  props.operation.value.type === "BIND_AGENT_RUNTIME_ENVIRONMENT" &&
  props.projectRef && agent.value && environment.value &&
  agent.value.ref === agentRef.value && agent.value.projectRef === props.projectRef &&
  !agent.value.system && agent.value.state !== "ARCHIVED" &&
  agent.value.version === props.operation.value.expectedVersion &&
  environment.value.ref === environmentRef.value &&
  environment.value.projectRef === props.projectRef &&
  environment.value.state === "ACTIVE" && environment.value.ready,
));
watch(valid, (value) => emit("valid", value), { immediate: true });

watch([() => props.projectRef, agentRef] as const, ([projectRef, ref], _previous, onCleanup) => {
  agent.value = undefined;
  agentProblem.value = false;
  if (!projectRef || !ref) return;
  const controller = new AbortController();
  onCleanup(() => controller.abort());
  void unwrap(getAgent({ path: { agentRef: ref }, signal: requestSignal(controller.signal) }))
    .then(({ data }) => {
      if (!controller.signal.aborted) agent.value = data;
    })
    .catch(() => { if (!controller.signal.aborted) agentProblem.value = true; });
}, { immediate: true });

watch([() => props.projectRef, environmentRef] as const, ([projectRef, ref], _previous, onCleanup) => {
  environment.value = undefined;
  environmentProblem.value = false;
  if (!projectRef || !ref) return;
  const controller = new AbortController();
  onCleanup(() => controller.abort());
  void unwrap(getRuntimeEnvironmentSet({ path: { environmentRef: ref }, signal: requestSignal(controller.signal) }))
    .then(({ data }) => {
      if (!controller.signal.aborted) environment.value = data;
    })
    .catch(() => { if (!controller.signal.aborted) environmentProblem.value = true; });
}, { immediate: true });

async function loadEnvironments(query: string, cursor: string | undefined, signal: AbortSignal): Promise<AsyncEntityOptionPage> {
  if (!props.projectRef) return { items: [] };
  const page = (await unwrap(listRuntimeEnvironmentSets({
    path: { projectRef: props.projectRef },
    query: { query, pageSize: 30, ...(cursor ? { pageToken: cursor } : {}) },
    signal: requestSignal(signal),
  }))).data;
  return {
    items: page.items.filter((item) =>
      item.projectRef === props.projectRef && item.state === "ACTIVE" && item.ready,
    ).map((item) => ({ ref: item.ref, title: item.name, description: item.description })),
    ...(page.nextPageToken ? { nextPageToken: page.nextPageToken } : {}),
  };
}

function selectEnvironment(value: string | null | readonly string[]): void {
  if (typeof value !== "string") return;
  emit("parameter", "environmentRef", value);
  emit("dirty");
}
</script>

<template>
  <div class="assistant-agent-environment-binding-form">
    <p class="assistant-plan-friendly__hint">{{ $t("assistant.planEditor.bindingBoundary") }}</p>
    <p v-if="agentProblem || environmentProblem" class="field-error" role="alert">
      {{ $t("assistant.planEditor.bindingLoadFailed") }}
    </p>
    <p v-if="agent"><strong>{{ agent.name }}</strong></p>
    <p v-if="agent && agent.version !== operation.value.expectedVersion" class="field-error" role="alert">
      {{ $t("assistant.planEditor.bindingStale") }}
    </p>
    <p v-if="beforeEnvironmentName">{{ $t("assistant.planEditor.bindingCurrent", { environment: beforeEnvironmentName }) }}</p>
    <label class="field">
      <span>{{ $t("assistant.planEditor.bindingTarget") }}</span>
      <AsyncEntityPicker
        :model-value="environmentRef"
        :selected="environment && { ref: environment.ref, title: environment.name }"
        :load-page="loadEnvironments"
        :context-key="projectRef"
        :disabled="disabled || !agent"
        :placeholder="$t('assistant.planEditor.bindingChoose')"
        :search-placeholder="$t('assistant.planEditor.bindingSearch')"
        @update:model-value="selectEnvironment"
      />
    </label>
    <p v-if="environment && !environment.ready" class="field-error" role="alert">
      {{ $t("assistant.planEditor.bindingUnavailable") }}
    </p>
    <p>{{ $t("assistant.planEditor.bindingNextSteps") }}</p>
  </div>
</template>

<style scoped>
.assistant-agent-environment-binding-form { display: grid; gap: 12px; }
</style>
