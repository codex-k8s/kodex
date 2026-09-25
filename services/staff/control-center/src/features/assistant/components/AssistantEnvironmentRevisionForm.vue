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

function stringField(key: string): string {
  try {
    const value = operationParameter(props.operation, key);
    return typeof value === "string" ? value : "";
  } catch {
    return "";
  }
}

const valid = computed(
  () =>
    props.operation.value.type === "PREPARE_RUNTIME_ENVIRONMENT_REVISION" &&
    stringField("environmentRef") === props.operation.value.target.ref &&
    stringField("name").trim().length > 0 &&
    stringField("name").length <= 120 &&
    stringField("description").length <= 1000 &&
    (stringField("imageArtifactRef") === "" ||
      /^imgart_[A-Za-z0-9_-]+$/.test(stringField("imageArtifactRef"))),
);
watch(valid, (value) => emit("valid", value), { immediate: true });

function changeText(key: string, event: Event): void {
  emit("parameter", key, (event.target as HTMLInputElement).value);
  emit("dirty");
}
</script>

<template>
  <div class="assistant-environment-revision">
    <p class="assistant-plan-friendly__hint">
      {{ $t("assistant.planEditor.environmentRevisionBoundary") }}
    </p>
    <label class="field">
      <span>{{ $t("assistant.planEditor.entityName") }}</span>
      <input
        :value="stringField('name')"
        maxlength="120"
        :disabled="disabled"
        @input="changeText('name', $event)"
      />
    </label>
    <label class="field">
      <span>{{ $t("assistant.planEditor.environmentDescription") }}</span>
      <textarea
        :value="stringField('description')"
        rows="3"
        maxlength="1000"
        :disabled="disabled"
        @input="changeText('description', $event)"
      />
    </label>
    <p v-if="!valid" class="field-error" role="alert">
      {{ $t("assistant.planEditor.environmentRevisionNotReady") }}
    </p>
    <p>{{ $t("assistant.planEditor.environmentRevisionNextSteps") }}</p>
  </div>
</template>

<style scoped>
.assistant-environment-revision {
  display: grid;
  gap: 12px;
}
</style>
