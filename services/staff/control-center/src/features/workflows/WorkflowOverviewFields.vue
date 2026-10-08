<script setup lang="ts">
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import { computed, useId } from "vue";
import type {
  AsyncEntityOption,
  AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";

const props = defineProps<{
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
const completionCriteriaMaxLength = 2000;
// OpenAPI maxLength считает Unicode code points, включая emoji как один символ.
const completionCriteriaLength = computed(
  () => Array.from(props.completionCriteria).length,
);
const completionCriteriaTooLong = computed(
  () => completionCriteriaLength.value > completionCriteriaMaxLength,
);
const completionCriteriaDescription = computed(() =>
  [
    `${fieldPrefix}-completion-count`,
    ...(completionCriteriaTooLong.value
      ? [`${fieldPrefix}-completion-error`]
      : []),
  ].join(" "),
);
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
    <label class="field field--wide">
      <span :id="`${fieldPrefix}-completion-label`">{{
        $t("workflows.completion")
      }}</span>
      <VoiceTextarea
        :id="`${fieldPrefix}-completion`"
        :model-value="completionCriteria"
        :aria-labelledby="`${fieldPrefix}-completion-label`"
        :aria-describedby="completionCriteriaDescription"
        :aria-invalid="completionCriteriaTooLong"
        :disabled="disabled"
        @update:model-value="emit('update:completionCriteria', $event.trim())"
      />
      <small :id="`${fieldPrefix}-completion-count`">{{
        $t("workflows.completionLength", {
          count: completionCriteriaLength,
          max: completionCriteriaMaxLength,
        })
      }}</small>
      <small
        v-if="completionCriteriaTooLong"
        :id="`${fieldPrefix}-completion-error`"
        class="field-error workflow-overview-fields__error"
        role="status"
        >{{
          $t("workflows.completionTooLong", {
            max: completionCriteriaMaxLength,
          })
        }}</small
      >
    </label>
  </div>
</template>

<style scoped>
.workflow-overview-fields {
  display: contents;
}
.workflow-overview-fields > .field {
  align-content: start;
}
.workflow-overview-fields .workflow-overview-fields__error {
  margin: 0;
  padding: 6px 10px;
  color: var(--danger);
}
</style>
