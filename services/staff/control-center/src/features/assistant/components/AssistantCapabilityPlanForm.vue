<script setup lang="ts">
import { computed, ref, watch } from "vue";

import {
  operationParameter,
  type EditablePlanOperation,
} from "@/features/assistant/model";
import { requestSignal } from "@/shared/api/client";
import {
  getAgent,
  listPlatformCapabilities,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  Agent,
  PlatformCapability,
} from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";

const props = defineProps<{
  operation: EditablePlanOperation;
  projectRef?: string;
  disabled: boolean;
}>();
const emit = defineEmits<{
  valid: [value: boolean];
  dirty: [];
  parameter: [key: string, value: string | boolean];
}>();
const agent = ref<Agent>();
const capabilities = ref<PlatformCapability[]>([]);
const loading = ref(false);
const problem = ref(false);
const agentRef = computed(() => {
  try {
    const value = operationParameter(props.operation, "agentRef");
    return typeof value === "string" ? value : "";
  } catch {
    return "";
  }
});
const capabilityKey = computed(() => {
  try {
    const value = operationParameter(props.operation, "capabilityKey");
    return typeof value === "string" ? value : "";
  } catch {
    return "";
  }
});
const enabled = computed(() => {
  try {
    return operationParameter(props.operation, "enabled");
  } catch {
    return undefined;
  }
});
const selectedCapability = computed(() =>
  capabilities.value.find((item) => item.key === capabilityKey.value),
);
const versionMatches = computed(() => {
  if (!agent.value) return false;
  try {
    return (
      agent.value.version === props.operation.value.expectedVersion &&
      agent.value.version ===
        operationParameter(props.operation, "expectedVersion")
    );
  } catch {
    return false;
  }
});
const valid = computed(() =>
  Boolean(
    agent.value &&
    props.projectRef &&
    agent.value.ref === agentRef.value &&
    agent.value.projectRef === props.projectRef &&
    props.operation.value.target.ref === agentRef.value &&
    versionMatches.value &&
    selectedCapability.value &&
    typeof enabled.value === "boolean",
  ),
);
watch(valid, (value) => emit("valid", value), { immediate: true });

watch(
  [() => props.projectRef, agentRef] as const,
  ([projectRef, ref], _previous, onCleanup) => {
    agent.value = undefined;
    capabilities.value = [];
    loading.value = false;
    problem.value = false;
    if (!projectRef || !ref) return;
    const controller = new AbortController();
    onCleanup(() => controller.abort());
    loading.value = true;
    void Promise.all([
      unwrap(
        getAgent({
          path: { agentRef: ref },
          signal: requestSignal(controller.signal),
        }),
      ),
      unwrap(
        listPlatformCapabilities({ signal: requestSignal(controller.signal) }),
      ),
    ])
      .then(([agentResponse, capabilityResponse]) => {
        if (controller.signal.aborted) return;
        const next = agentResponse.data;
        if (next.ref !== ref || next.projectRef !== projectRef)
          throw new Error("Assistant capability target readback mismatch");
        agent.value = next;
        capabilities.value = capabilityResponse.data.items;
      })
      .catch(() => {
        if (!controller.signal.aborted) problem.value = true;
      })
      .finally(() => {
        if (!controller.signal.aborted) loading.value = false;
      });
  },
  { immediate: true },
);

function changed(key: string, value: string | boolean): void {
  emit("parameter", key, value);
  emit("dirty");
}
</script>

<template>
  <div class="assistant-capability-form">
    <p v-if="loading" role="status">{{ $t("common.loading") }}</p>
    <p v-if="problem" class="field-error" role="alert">
      {{ $t("assistant.planEditor.capabilityLoadFailed") }}
    </p>
    <template v-if="agent">
      <p>
        {{ $t("assistant.planEditor.capabilityAgent") }}:
        <strong>{{ agent.name }}</strong>
      </p>
      <p v-if="!versionMatches" class="field-error" role="alert">
        {{ $t("assistant.planEditor.capabilityStale") }}
      </p>
      <label class="field">
        <span>{{ $t("assistant.planEditor.capabilityName") }}</span>
        <select
          :value="capabilityKey"
          :disabled="disabled || !versionMatches"
          @change="
            changed('capabilityKey', ($event.target as HTMLSelectElement).value)
          "
        >
          <option value="">
            {{ $t("assistant.planEditor.capabilityChoose") }}
          </option>
          <option
            v-for="item in capabilities"
            :key="item.key"
            :value="item.key"
          >
            {{ item.name }}
          </option>
        </select>
      </label>
      <p v-if="selectedCapability">{{ selectedCapability.description }}</p>
      <p v-else-if="capabilityKey" class="field-error" role="alert">
        {{ $t("assistant.planEditor.capabilityUnknown") }}
      </p>
      <label class="assistant-capability-form__enabled">
        <input
          type="checkbox"
          :checked="enabled === true"
          :disabled="disabled || !versionMatches || !selectedCapability"
          @change="
            changed('enabled', ($event.target as HTMLInputElement).checked)
          "
        />
        {{ $t("assistant.planEditor.capabilityEnable") }}
      </label>
      <p>{{ $t("assistant.planEditor.capabilityNextSteps") }}</p>
    </template>
  </div>
</template>

<style scoped>
.assistant-capability-form {
  display: grid;
  gap: 10px;
  min-width: 0;
}
.assistant-capability-form p {
  margin: 0;
}
.assistant-capability-form__enabled {
  display: flex;
  gap: 8px;
  align-items: center;
}
</style>
