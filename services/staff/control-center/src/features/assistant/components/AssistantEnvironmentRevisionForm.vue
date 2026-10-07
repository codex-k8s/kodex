<script setup lang="ts">
import { computed, useId, watch } from "vue";
import { useServerMessage } from "@/shared/ui/server-message";
import { environmentDisplayField } from "@/features/runtime/environment-display-field";

import {
  operationParameter,
  type EditablePlanOperation,
} from "@/features/assistant/model";

const props = defineProps<{
  operation: EditablePlanOperation;
  disabled: boolean;
  helperApplied?: boolean;
}>();
const fieldPrefix = `assistant-environment-${useId()}`;
const emit = defineEmits<{
  valid: [value: boolean];
  dirty: [];
  parameter: [key: string, value: unknown];
}>();
const localizeServerMessage = useServerMessage();
const nameFieldValue = environmentDisplayField(
  () => stringField("name"),
  (value) => changeText("name", value),
  localizeServerMessage,
);
const descriptionFieldValue = environmentDisplayField(
  () => stringField("description"),
  (value) => changeText("description", value),
  localizeServerMessage,
);

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
const systemAssistantEnvironment = computed(
  () => stringField("systemAssistantRef").length > 0,
);
watch(valid, (value) => emit("valid", value), { immediate: true });

function changeText(key: string, value: string): void {
  emit("parameter", key, value);
  emit("dirty");
}
</script>

<template>
  <div class="assistant-environment-revision">
    <p v-if="!helperApplied" class="assistant-plan-friendly__hint">
      {{
        $t(
          systemAssistantEnvironment
            ? "assistant.planEditor.systemEnvironmentBoundary"
            : "assistant.planEditor.environmentRevisionBoundary",
        )
      }}
    </p>
    <label class="field">
      <span>{{ $t("assistant.planEditor.entityName") }}</span>
      <input
        v-model="nameFieldValue"
        :id="`${fieldPrefix}-name`"
        :name="`${fieldPrefix}-name`"
        maxlength="120"
        :disabled="disabled"
      />
    </label>
    <label class="field">
      <span>{{ $t("assistant.planEditor.environmentDescription") }}</span>
      <textarea
        v-model="descriptionFieldValue"
        :id="`${fieldPrefix}-description`"
        :name="`${fieldPrefix}-description`"
        rows="3"
        maxlength="1000"
        :disabled="disabled"
      />
    </label>
    <p v-if="!valid" class="field-error" role="alert">
      {{ $t("assistant.planEditor.environmentRevisionNotReady") }}
    </p>
    <p>
      {{
        $t(
          helperApplied
            ? "assistant.planEditor.helperEnvironmentDraftPrepared"
            : systemAssistantEnvironment
              ? "assistant.planEditor.systemEnvironmentNextSteps"
              : "assistant.planEditor.environmentRevisionNextSteps",
        )
      }}
    </p>
  </div>
</template>

<style scoped>
.assistant-environment-revision {
  display: grid;
  gap: 12px;
}
</style>
