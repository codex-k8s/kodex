<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { ProviderAccountSelector } from "@/features/providers";
import ProviderModelSelector from "@/features/providers/ProviderModelSelector.vue";
import type { ModelSelection } from "@/features/providers/model-catalog";
import type {
  ProviderAccountCandidateInput,
  RuntimeSelection,
} from "@/shared/api/generated/openapi/types.gen";
import { pinnedRuntimeInput, type RuntimeForm } from "./runtime-input";

const form = defineModel<RuntimeForm>({ required: true });
const props = defineProps<{
  agentRef: string;
  runtimes: RuntimeSelection[];
  reasoningEffort?: string;
  disabled: boolean;
}>();
const emit = defineEmits<{
  "candidate-change": [candidate: ProviderAccountCandidateInput | undefined];
}>();
const selection = ref<ModelSelection>();
const accountAllowed = ref(false);
const runtime = computed(() =>
  props.runtimes.find((item) => item.ref === form.value.runtimeProfileRef),
);
const context = computed(() => ({
  purpose: "CONFIGURE" as const,
  agentRef: props.agentRef,
  runtimeProfileRef: form.value.runtimeProfileRef,
  providerDefinitionKey: runtime.value?.provider ?? "",
  model: form.value.model,
  reasoningEffort: props.reasoningEffort,
}));
const accountRefs = computed(() =>
  form.value.providerAccounts.map((item) => item.accountRef),
);
watch(context, () => (accountAllowed.value = false), { flush: "sync" });
watch(accountRefs, () => (accountAllowed.value = false), { flush: "sync" });
watch(
  () => [selection.value, accountAllowed.value, form.value] as const,
  ([current, allowed, draft]) => {
    const profile = runtime.value;
    if (
      !profile?.ready ||
      !current ||
      !allowed ||
      draft.providerAccounts.length !== 1 ||
      current.providerDefinitionKey !== profile.provider ||
      current.model !== draft.model ||
      current.accounts[0]?.accountRef !== accountRefs.value[0]
    ) {
      emit("candidate-change", undefined);
      return;
    }
    const pinned = pinnedRuntimeInput(draft, current, profile.provider);
    const candidate = pinned.providerAccounts[0];
    emit(
      "candidate-change",
      candidate && {
        ...candidate,
        runtimeProfileRef: profile.ref,
        model: draft.model,
      },
    );
  },
  { immediate: true },
);

function chooseProfile(event: Event): void {
  const target = event.target;
  if (props.disabled || !(target instanceof HTMLSelectElement)) return;
  const profile = props.runtimes.find((item) => item.ref === target.value);
  if (!profile?.ready) return;
  form.value = {
    runtimeProfileRef: profile.ref,
    model: profile.model,
    providerPolicyMode: "FIXED",
    providerAccounts: [],
  };
}
</script>

<template>
  <div class="field">
    <label>
      <span>{{ $t("runtime.alternativeProfile") }}</span>
      <select
        :value="form.runtimeProfileRef"
        :disabled="disabled"
        @change="chooseProfile"
      >
        <option v-if="!runtime" :value="form.runtimeProfileRef" disabled>
          {{ form.runtimeProfileRef }}
        </option>
        <option
          v-for="profile in runtimes"
          :key="profile.ref"
          :value="profile.ref"
        >
          {{ profile.name }} · {{ profile.provider }}
        </option>
      </select>
    </label>
    <ProviderAccountSelector
      :model-value="form.providerAccounts"
      :definition-key="context.providerDefinitionKey"
      policy-mode="FIXED"
      :usage-context="context"
      :disabled="disabled || !runtime?.ready"
      @update:model-value="form = { ...form, providerAccounts: $event }"
      @submission-allowed-change="accountAllowed = $event"
    />
    <ProviderModelSelector
      :model-value="form.model"
      :definition-key="context.providerDefinitionKey"
      :account-refs="accountRefs"
      :disabled="disabled || !runtime?.ready"
      @update:model-value="form = { ...form, model: $event }"
      @selection-change="selection = $event"
    />
  </div>
</template>
