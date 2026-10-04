<script setup lang="ts">
import { computed, ref, watch, useId } from "vue";
import { loadRuntimeCatalog } from "@/features/agents/detail/runtime-api";
import { readyRuntimes } from "@/features/agents/detail/model";
import { usePlatformStore } from "@/features/platform/store";
import ProviderModelSelector from "@/features/providers/ProviderModelSelector.vue";
import { ProviderAccountSelector } from "@/features/providers";
import type { ModelSelection } from "@/features/providers/model-catalog";
import type { ProviderAccountCandidate } from "@/features/providers/model";
import {
  operationParameter,
  operationInputs,
  type EditablePlanOperation,
} from "@/features/assistant/model";
import {
  assistantRuntimePlanOwner,
  assistantRuntimeReasoning,
} from "@/features/assistant/runtime-configuration-plan";
import type { RuntimeSelection } from "@/shared/api/generated/openapi/types.gen";

const props = defineProps<{
  operation: EditablePlanOperation;
  disabled: boolean;
}>();
const emit = defineEmits<{
  valid: [value: boolean];
  dirty: [];
  parameter: [key: string, value: unknown];
}>();
const platform = usePlatformStore();
const id = `assistant-runtime-${useId()}`;
const runtimes = ref<RuntimeSelection[]>([]);
const catalogFailed = ref(false);
const modelAvailable = ref(false);
const accountsAvailable = ref(false);
const modelSelection = ref<ModelSelection>();
function value(key: string): unknown {
  try {
    return operationParameter(props.operation, key);
  } catch {
    return undefined;
  }
}
function text(key: string): string {
  const result = value(key);
  return typeof result === "string" ? result : "";
}
const ownerValid = computed(() => {
  try {
    const input = operationInputs([props.operation])[0];
    return (
      input !== undefined &&
      assistantRuntimePlanOwner(input, platform.bootstrap?.organizationRef)
    );
  } catch {
    return false;
  }
});
const selectedRuntime = computed(() =>
  readyRuntimes(runtimes.value).find(
    (item) => item.ref === text("runtimeProfileRef"),
  ),
);
const policy = computed(() => {
  const candidate = text("providerPolicyMode");
  return candidate === "FIXED" ||
    candidate === "LEAST_USED" ||
    candidate === "WEIGHTED"
    ? candidate
    : undefined;
});
const accounts = computed<ProviderAccountCandidate[]>(() => {
  const raw = value("providerAccounts");
  if (!Array.isArray(raw)) return [];
  return raw.flatMap((item: unknown) => {
    if (
      !item ||
      typeof item !== "object" ||
      !("accountRef" in item) ||
      !("weight" in item) ||
      typeof item.accountRef !== "string" ||
      typeof item.weight !== "number"
    )
      return [];
    return [{ accountRef: item.accountRef, weight: item.weight }];
  });
});
const reasoning = computed(() =>
  assistantRuntimeReasoning(
    modelSelection.value,
    text("model"),
    selectedRuntime.value?.provider ?? "",
    accounts.value.map((item) => item.accountRef),
  ),
);
const efforts = computed(() => reasoning.value?.efforts ?? []);
const usageContext = computed(() => ({
  purpose: "CONFIGURE" as const,
  agentRef: text("agentRef"),
  runtimeProfileRef: text("runtimeProfileRef"),
  providerDefinitionKey: selectedRuntime.value?.provider ?? "",
  model: text("model"),
  reasoningEffort: text("reasoningEffort"),
}));
const valid = computed(
  () =>
    ownerValid.value &&
    Boolean(selectedRuntime.value) &&
    policy.value !== undefined &&
    modelAvailable.value &&
    accountsAvailable.value &&
    reasoning.value !== undefined &&
    (text("reasoningEffort") === "" ||
      efforts.value.includes(text("reasoningEffort"))),
);
watch(valid, (result) => emit("valid", result), { immediate: true });
watch(
  () => [props.operation.value.ref, platform.bootstrap?.organizationRef],
  (_next, _old, cleanup) => {
    const controller = new AbortController();
    cleanup(() => controller.abort());
    runtimes.value = [];
    catalogFailed.value = false;
    if (!ownerValid.value) return;
    void loadRuntimeCatalog(controller.signal)
      .then((result) => {
        if (!controller.signal.aborted) runtimes.value = result;
      })
      .catch(() => {
        if (!controller.signal.aborted) catalogFailed.value = true;
      });
  },
  { immediate: true },
);
function update(key: string, input: unknown): void {
  if (props.disabled || !ownerValid.value) return;
  emit("parameter", key, input);
  emit("dirty");
}
function change(key: string, event: Event): void {
  update(key, (event.target as HTMLSelectElement).value);
}
</script>

<template>
  <div class="assistant-runtime-plan">
    <p>{{ $t("assistant.planEditor.runtimeConfigurationBoundary") }}</p>
    <p v-if="!ownerValid || catalogFailed" class="field-error" role="alert">
      {{ $t("assistant.planEditor.runtimeConfigurationUnavailable") }}
    </p>
    <label class="field" :for="`${id}-profile`">
      <span>{{ $t("agents.runtimeRevision") }}</span>
      <select
        :id="`${id}-profile`"
        :name="`${id}-profile`"
        :value="text('runtimeProfileRef')"
        :disabled="disabled || !ownerValid"
        @change="change('runtimeProfileRef', $event)"
      >
        <option
          v-if="!selectedRuntime"
          :value="text('runtimeProfileRef')"
          disabled
        >
          {{ $t("common.loading") }}
        </option>
        <option
          v-for="item in readyRuntimes(runtimes)"
          :key="item.ref"
          :value="item.ref"
        >
          {{ item.name }}
        </option>
      </select>
    </label>
    <label class="field" :for="`${id}-policy`">
      <span>{{ $t("runtime.accountPolicy") }}</span>
      <select
        :id="`${id}-policy`"
        :name="`${id}-policy`"
        :value="policy"
        :disabled="disabled || !ownerValid"
        @change="change('providerPolicyMode', $event)"
      >
        <option
          v-for="mode in ['FIXED', 'LEAST_USED', 'WEIGHTED']"
          :key="mode"
          :value="mode"
        >
          {{ $t(`runtime.policy.${mode}`) }}
        </option>
      </select>
    </label>
    <ProviderAccountSelector
      v-if="selectedRuntime && policy"
      :model-value="accounts"
      :definition-key="selectedRuntime.provider"
      :policy-mode="policy"
      :usage-context="usageContext"
      :disabled="disabled || !ownerValid"
      @update:model-value="update('providerAccounts', $event)"
      @submission-allowed-change="accountsAvailable = $event"
    />
    <ProviderModelSelector
      :model-value="text('model')"
      :definition-key="selectedRuntime?.provider ?? ''"
      :account-refs="accounts.map((item) => item.accountRef)"
      :disabled="disabled || !ownerValid"
      @update:model-value="update('model', $event)"
      @availability-change="modelAvailable = $event"
      @selection-change="modelSelection = $event"
    />
    <label class="field" :for="`${id}-effort`">
      <span>{{ $t("runtimeOverlay.effort") }}</span>
      <select
        :id="`${id}-effort`"
        :name="`${id}-effort`"
        :value="text('reasoningEffort')"
        :disabled="disabled || !ownerValid || !modelAvailable || !reasoning"
        @change="change('reasoningEffort', $event)"
      >
        <option
          v-if="
            text('reasoningEffort') &&
            !efforts.includes(text('reasoningEffort'))
          "
          :value="text('reasoningEffort')"
          disabled
        >
          {{ text("reasoningEffort") }}
        </option>
        <option value="">
          {{
            $t(
              reasoning?.unsupported
                ? "assistant.planEditor.reasoningUnsupported"
                : "assistant.planEditor.reasoningCatalogDefault",
            )
          }}
        </option>
        <option v-for="effort in efforts" :key="effort" :value="effort">
          {{ effort }}
        </option>
      </select>
      <small v-if="reasoning?.unsupported">{{
        $t("assistant.planEditor.reasoningUnsupportedHelp")
      }}</small>
    </label>
  </div>
</template>

<style scoped>
.assistant-runtime-plan {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.assistant-runtime-plan p {
  margin: 0;
}
</style>
