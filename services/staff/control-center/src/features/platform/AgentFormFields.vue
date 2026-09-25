<script setup lang="ts">
import { computed, watch } from "vue";

import { isAgentDraftComplete } from "@/features/platform/agent-form";
import type { RuntimeSelection } from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";

const props = withDefaults(
  defineProps<{
    name: string;
    purpose: string;
    roleDescription: string;
    initialInstructions: string;
    runtimeRef: string;
    runtimes: readonly RuntimeSelection[];
    runtimeProblem?: AppProblem;
    disabled?: boolean;
    runtimeExpanded?: boolean;
  }>(),
  { disabled: false, runtimeExpanded: false },
);
const emit = defineEmits<{
  "update:name": [value: string];
  "update:purpose": [value: string];
  "update:roleDescription": [value: string];
  "update:initialInstructions": [value: string];
  "update:runtimeRef": [value: string];
  valid: [value: boolean];
}>();
const valid = computed(
  () =>
    isAgentDraftComplete(props) &&
    props.runtimes.some(
      (runtime) => runtime.ready && runtime.ref === props.runtimeRef,
    ),
);
watch(valid, (value) => emit("valid", value), { immediate: true });
</script>

<template>
  <div class="agent-form-fields">
    <label class="field">
      <span>{{ $t("common.name") }}</span>
      <input
        :value="name"
        :disabled="disabled"
        required
        maxlength="120"
        @input="
          emit('update:name', ($event.target as HTMLInputElement).value.trim())
        "
      />
    </label>
    <label class="field">
      <span>{{ $t("common.purpose") }}</span>
      <input
        :value="purpose"
        :disabled="disabled"
        required
        maxlength="1000"
        @input="
          emit(
            'update:purpose',
            ($event.target as HTMLInputElement).value.trim(),
          )
        "
      />
    </label>
    <label class="field field--wide">
      <span>{{ $t("agents.role") }}</span>
      <VoiceTextarea
        :model-value="roleDescription"
        :disabled="disabled"
        required
        maxlength="1000"
        @update:model-value="emit('update:roleDescription', $event.trim())"
      />
    </label>
    <label class="field field--wide">
      <span>{{ $t("agents.instructions") }}</span>
      <VoiceTextarea
        :model-value="initialInstructions"
        :disabled="disabled"
        required
        minlength="20"
        maxlength="65536"
        @update:model-value="emit('update:initialInstructions', $event.trim())"
      />
    </label>
    <details class="field--wide advanced-settings" :open="runtimeExpanded">
      <summary>{{ $t("common.advanced") }}</summary>
      <label class="field">
        <span>{{ $t("agents.runtime") }}</span>
        <select
          :value="runtimeRef"
          :disabled="disabled"
          required
          @change="
            emit(
              'update:runtimeRef',
              ($event.target as HTMLSelectElement).value,
            )
          "
        >
          <option value="" disabled>{{ $t("agents.runtime") }}</option>
          <option
            v-for="runtime in runtimes.filter((item) => item.ready)"
            :key="runtime.ref"
            :value="runtime.ref"
          >
            {{ runtime.name }}
          </option>
        </select>
        <small>{{ $t("agents.runtimeHelp") }}</small>
      </label>
      <ProblemNotice v-if="runtimeProblem" :problem="runtimeProblem" compact />
    </details>
  </div>
</template>

<style scoped>
.agent-form-fields {
  display: contents;
}
.field {
  display: grid;
  gap: 5px;
  min-width: 0;
}
.field--wide {
  grid-column: 1 / -1;
}
.advanced-settings {
  display: grid;
  gap: 12px;
}
.advanced-settings summary {
  cursor: pointer;
}
.advanced-settings .field {
  margin-top: 12px;
}
</style>
