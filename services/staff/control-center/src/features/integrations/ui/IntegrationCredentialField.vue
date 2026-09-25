<script setup lang="ts">
import { useId } from "vue";

defineProps<{
  modelValue: string;
  credentialSecretKey?: string;
  invalid?: boolean;
  disabled?: boolean;
}>();
const emit = defineEmits<{ "update:modelValue": [value: string] }>();
const fieldPrefix = `integration-credential-${useId()}`;
const helpId = `${fieldPrefix}-help`;
</script>

<template>
  <label class="field field--wide card credential-boundary">
    <strong>{{ $t("integrations.credentials") }}</strong>
    <code v-if="credentialSecretKey">{{ credentialSecretKey }}</code>
    <span>{{ $t("integrations.credentialValue") }}</span>
    <input
      :id="fieldPrefix"
      :name="fieldPrefix"
      :value="modelValue"
      type="password"
      required
      maxlength="16384"
      autocomplete="new-password"
      autocapitalize="none"
      spellcheck="false"
      :disabled="disabled"
      :aria-invalid="invalid || undefined"
      :aria-describedby="helpId"
      @input="
        emit('update:modelValue', ($event.target as HTMLInputElement).value)
      "
    />
    <small :id="helpId">{{ $t("integrations.credentialValueHelp") }}</small>
    <small v-if="invalid" class="field-error">
      {{ $t("integrations.credentialRequired") }}
    </small>
  </label>
</template>

<style scoped>
.credential-boundary {
  display: grid;
  gap: 6px;
  margin: 0;
  border-radius: 8px;
  background: var(--panel);
}
</style>
