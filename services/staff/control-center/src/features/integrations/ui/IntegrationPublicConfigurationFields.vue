<script setup lang="ts">
import IntegrationIntegerBounds from "@/features/integrations/ui/IntegrationIntegerBounds.vue";
import type { IntegrationConfigurationField } from "@/shared/api/generated/openapi/types.gen";
import { useId } from "vue";

withDefaults(
  defineProps<{
    fields: readonly IntegrationConfigurationField[];
    values: Readonly<Record<string, string>>;
    problems?: Readonly<Record<string, string>>;
    submitted?: boolean;
    disabled?: boolean;
  }>(),
  { problems: () => ({}), submitted: false, disabled: false },
);
const emit = defineEmits<{ change: [key: string, value: string] }>();
const fieldPrefix = `integration-configuration-${useId()}`;
</script>

<template>
  <label v-for="field in fields" :key="field.key" class="field field--wide">
    <span>{{ field.label }}</span>
    <select
      v-if="field.allowedValues?.length"
      :id="`${fieldPrefix}-${field.key}`"
      :name="`${fieldPrefix}-${field.key}`"
      :value="values[field.key] ?? ''"
      :required="field.required"
      :disabled="disabled"
      :aria-invalid="submitted && Boolean(problems[field.key])"
      @change="
        emit('change', field.key, ($event.target as HTMLSelectElement).value)
      "
    >
      <option value=""></option>
      <option v-for="value in field.allowedValues" :key="value" :value="value">
        {{ value }}
      </option>
    </select>
    <input
      v-else-if="field.valueType === 'BOOLEAN'"
      type="checkbox"
      :id="`${fieldPrefix}-${field.key}`"
      :name="`${fieldPrefix}-${field.key}`"
      :checked="values[field.key] === 'true'"
      :disabled="disabled"
      @change="
        emit(
          'change',
          field.key,
          ($event.target as HTMLInputElement).checked ? 'true' : 'false',
        )
      "
    />
    <input
      v-else
      :id="`${fieldPrefix}-${field.key}`"
      :name="`${fieldPrefix}-${field.key}`"
      :value="values[field.key] ?? ''"
      :type="field.valueType === 'URL' ? 'url' : 'text'"
      :inputmode="field.valueType === 'INTEGER' ? 'numeric' : undefined"
      :required="field.required"
      :placeholder="field.placeholder"
      :maxlength="
        field.maximumLength ?? (field.valueType === 'URL' ? 2048 : 500)
      "
      :disabled="disabled"
      :aria-invalid="submitted && Boolean(problems[field.key])"
      autocomplete="off"
      @input="
        emit('change', field.key, ($event.target as HTMLInputElement).value)
      "
    />
    <small>
      {{ field.help }}
      <IntegrationIntegerBounds :field="field" />
      <template v-if="field.valueType === 'STRING_LIST'">
        {{ $t("assistant.planEditor.connectionListHint") }}
      </template>
    </small>
    <small v-if="submitted && problems[field.key]" class="field-error">
      {{ problems[field.key] }}
    </small>
  </label>
</template>
