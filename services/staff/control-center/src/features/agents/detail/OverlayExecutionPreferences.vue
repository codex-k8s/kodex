<script setup lang="ts">
import { computed, ref, useId, watch } from "vue";
import type { ConfigOverlaySchema } from "@/shared/api/generated/openapi/types.gen";
import { changeOverlayPreference, overlayPreference } from "./overlay-editor";

const props = defineProps<{
  content: string;
  schema: ConfigOverlaySchema;
  disabled: boolean;
}>();
const emit = defineEmits<{ "update:content": [value: string] }>();
const prefix = useId();
const invalid = ref(false);
const fields = computed(() =>
  props.schema.fields.filter(
    (field) =>
      field.key.startsWith("context.") || field.key.startsWith("cache."),
  ),
);
watch(
  () => [props.content, props.schema],
  () => {
    invalid.value = false;
  },
);
function change(key: string, event: Event) {
  if (props.disabled) return;
  try {
    const value = changeOverlayPreference(
      props.content,
      props.schema,
      key,
      (event.target as HTMLSelectElement).value,
    );
    invalid.value = false;
    emit("update:content", value);
  } catch {
    invalid.value = true;
  }
}
</script>

<template>
  <fieldset
    v-if="fields.length"
    class="execution-preferences"
    :disabled="disabled"
  >
    <legend>{{ $t("runtimeOverlay.executionPreferences") }}</legend>
    <p>{{ $t("runtimeOverlay.preferencesHelp") }}</p>
    <div
      v-for="field in fields"
      :key="field.key"
      class="execution-preferences__field"
    >
      <label :for="`${prefix}-${field.key}`">{{ field.description }}</label>
      <select
        :id="`${prefix}-${field.key}`"
        :value="overlayPreference(content, schema, field.key) ?? '__invalid__'"
        :aria-describedby="`${prefix}-${field.key}-help`"
        @change="change(field.key, $event)"
      >
        <option value="">{{ $t("runtimeOverlay.inheritRoute") }}</option>
        <option
          v-if="overlayPreference(content, schema, field.key) === undefined"
          value="__invalid__"
          disabled
        >
          {{ $t("runtimeOverlay.repairPreferences") }}
        </option>
        <option
          v-for="value in field.allowedValues"
          :key="value"
          :value="value"
        >
          {{ $t(`runtimeOverlay.preferenceValues.${value}`) }}
        </option>
      </select>
      <small :id="`${prefix}-${field.key}-help`">{{ field.hover }}</small>
    </div>
    <p v-if="invalid" role="alert">
      {{ $t("runtimeOverlay.repairPreferences") }}
    </p>
  </fieldset>
</template>

<style scoped>
.execution-preferences {
  display: grid;
  gap: 1rem;
  margin: 1rem 0;
  padding: 1rem;
  border: 1px solid var(--border-color, #aaa);
  border-radius: 0.5rem;
}
.execution-preferences__field {
  display: grid;
  gap: 0.35rem;
}
.execution-preferences select {
  width: 100%;
  padding: 0.5rem;
  font: inherit;
}
</style>
