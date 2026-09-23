<script setup lang="ts">
import { computed, watch } from "vue";

import {
  operationParameter,
  type EditablePlanOperation,
} from "@/features/assistant/model";

const props = defineProps<{
  operation: EditablePlanOperation;
  disabled: boolean;
}>();
const emit = defineEmits<{
  valid: [value: boolean];
  dirty: [];
  parameter: [key: string, value: unknown];
}>();

function parameter(key: string): unknown {
  try {
    return operationParameter(props.operation, key);
  } catch {
    return undefined;
  }
}
function stringField(key: string): string {
  const value = parameter(key);
  return typeof value === "string" ? value : "";
}
function integerField(key: string): string {
  const value = parameter(key);
  return typeof value === "number" && Number.isInteger(value)
    ? String(value)
    : "";
}
const valid = computed(() => {
  const concurrency = parameter("maxConcurrency");
  const timeout = parameter("timeoutSeconds");
  return (
    props.operation.value.type === "UPDATE_WORKFLOW" &&
    stringField("workflowRef") === props.operation.value.target.ref &&
    stringField("name").trim().length > 0 &&
    stringField("name").length <= 160 &&
    stringField("purpose").length <= 2000 &&
    Number.isInteger(concurrency) &&
    Number(concurrency) >= 1 &&
    Number(concurrency) <= 100 &&
    Number.isInteger(timeout) &&
    Number(timeout) >= 1 &&
    Number(timeout) <= 604800
  );
});
watch(valid, (value) => emit("valid", value), { immediate: true });

function changeText(key: string, event: Event): void {
  emit("parameter", key, (event.target as HTMLInputElement).value);
  emit("dirty");
}
function changeInteger(key: string, event: Event): void {
  const raw = (event.target as HTMLInputElement).value;
  emit("parameter", key, /^\d+$/.test(raw) ? Number(raw) : raw);
  emit("dirty");
}
</script>

<template>
  <div class="assistant-workflow-update">
    <p class="assistant-plan-friendly__hint">
      {{ $t("assistant.planEditor.workflowUpdateBoundary") }}
    </p>
    <label class="field">
      <span>{{ $t("assistant.planEditor.entityName") }}</span>
      <input
        :value="stringField('name')"
        maxlength="160"
        :disabled="disabled"
        @input="changeText('name', $event)"
      />
    </label>
    <label class="field">
      <span>{{ $t("assistant.planEditor.entityPurpose") }}</span>
      <textarea
        :value="stringField('purpose')"
        rows="3"
        maxlength="2000"
        :disabled="disabled"
        @input="changeText('purpose', $event)"
      />
    </label>
    <label class="field">
      <span>{{ $t("assistant.planEditor.workflowInstructions") }}</span>
      <textarea
        :value="stringField('instructions')"
        rows="5"
        :disabled="disabled"
        @input="changeText('instructions', $event)"
      />
    </label>
    <label class="field">
      <span>{{ $t("assistant.planEditor.workflowCompletionCriteria") }}</span>
      <textarea
        :value="stringField('completionCriteria')"
        rows="3"
        :disabled="disabled"
        @input="changeText('completionCriteria', $event)"
      />
    </label>
    <div class="assistant-workflow-update__limits">
      <label class="field">
        <span>{{ $t("assistant.planEditor.workflowConcurrency") }}</span>
        <input
          type="number"
          min="1"
          max="100"
          step="1"
          :value="integerField('maxConcurrency')"
          :disabled="disabled"
          @input="changeInteger('maxConcurrency', $event)"
        />
      </label>
      <label class="field">
        <span>{{ $t("assistant.planEditor.workflowTimeout") }}</span>
        <input
          type="number"
          min="1"
          max="604800"
          step="1"
          :value="integerField('timeoutSeconds')"
          :disabled="disabled"
          @input="changeInteger('timeoutSeconds', $event)"
        />
      </label>
    </div>
    <p v-if="!valid" class="field-error" role="alert">
      {{ $t("assistant.planEditor.workflowNotReady") }}
    </p>
    <p>{{ $t("assistant.planEditor.workflowUpdateNextSteps") }}</p>
  </div>
</template>

<style scoped>
.assistant-workflow-update {
  display: grid;
  gap: 12px;
}
.assistant-workflow-update__limits {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
@media (max-width: 640px) {
  .assistant-workflow-update__limits {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
