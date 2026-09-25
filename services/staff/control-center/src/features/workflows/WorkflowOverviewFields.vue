<script setup lang="ts">
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import { useId } from "vue";
import type {
  AsyncEntityOption,
  AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";

defineProps<{
  name: string;
  purpose: string;
  coordinatorAgentRef: string;
  selectedCoordinator?: AsyncEntityOption;
  loadAgents: (
    query: string,
    cursor: string | undefined,
    signal: AbortSignal,
  ) => Promise<AsyncEntityOptionPage>;
  projectRef: string;
  timeoutSeconds: number;
  maxConcurrency: number;
  completionCriteria: string;
  disabled?: boolean;
}>();
const emit = defineEmits<{
  "update:name": [value: string];
  "update:purpose": [value: string];
  selectCoordinator: [option: AsyncEntityOption];
  clearCoordinator: [];
  "update:timeoutSeconds": [value: number];
  "update:maxConcurrency": [value: number];
  "update:completionCriteria": [value: string];
}>();
const fieldPrefix = `workflow-overview-${useId()}`;
</script>

<template>
  <div class="workflow-overview-fields">
    <label class="field">
      <span>{{ $t("common.name") }}</span>
      <input
        :id="`${fieldPrefix}-name`"
        :name="`${fieldPrefix}-name`"
        :value="name"
        required
        maxlength="160"
        :disabled="disabled"
        @input="
          emit('update:name', ($event.target as HTMLInputElement).value.trim())
        "
      />
    </label>
    <div class="field">
      <span>{{ $t("workflows.coordinator") }}</span>
      <AsyncEntityPicker
        :model-value="coordinatorAgentRef || null"
        :selected="selectedCoordinator"
        :load-page="loadAgents"
        :context-key="projectRef"
        :disabled="disabled || !projectRef"
        :trigger-label="$t('workflows.coordinator')"
        @select="emit('selectCoordinator', $event)"
        @update:model-value="$event === null && emit('clearCoordinator')"
      />
    </div>
    <label class="field field--wide">
      <span>{{ $t("common.purpose") }}</span>
      <VoiceTextarea
        :model-value="purpose"
        required
        maxlength="1000"
        :disabled="disabled"
        @update:model-value="emit('update:purpose', $event.trim())"
      />
    </label>
    <label class="field">
      <span>{{ $t("workflows.timeout") }}</span>
      <input
        :id="`${fieldPrefix}-timeout`"
        :name="`${fieldPrefix}-timeout`"
        :value="timeoutSeconds"
        type="number"
        min="1"
        max="604800"
        required
        :disabled="disabled"
        @input="
          emit(
            'update:timeoutSeconds',
            Number(($event.target as HTMLInputElement).value),
          )
        "
      />
    </label>
    <label class="field">
      <span>{{ $t("workflows.completion") }}</span>
      <VoiceTextarea
        :model-value="completionCriteria"
        maxlength="2000"
        :disabled="disabled"
        @update:model-value="emit('update:completionCriteria', $event.trim())"
      />
    </label>
    <label class="field">
      <span>{{ $t("workflows.concurrency") }}</span>
      <input
        :id="`${fieldPrefix}-concurrency`"
        :name="`${fieldPrefix}-concurrency`"
        :value="maxConcurrency"
        type="number"
        min="1"
        max="100"
        required
        :disabled="disabled"
        @input="
          emit(
            'update:maxConcurrency',
            Number(($event.target as HTMLInputElement).value),
          )
        "
      />
    </label>
  </div>
</template>

<style scoped>
.workflow-overview-fields {
  display: contents;
}
</style>
