<script setup lang="ts">
import { useId } from "vue";
const props = defineProps<{ value: string; disabled: boolean }>();
const emit = defineEmits<{ change: [value: "" | "OWNER_APPROVED_UPSTREAM_V1"] }>();
const id = `workflow-rework-${useId()}`;
function change(event: Event): void {
  const element = event.target as HTMLSelectElement;
  const value = element.value;
  element.value = props.value;
  if (!props.disabled && (value === "" || value === "OWNER_APPROVED_UPSTREAM_V1")) emit("change", value);
}
</script>
<template>
  <label class="field">
    <span>{{ $t("workflowResults.reworkPolicy") }}</span>
    <select :id="id" :name="id" :value="value" :disabled="disabled" @change="change">
      <option value="">{{ $t("workflowResults.reworkLegacy") }}</option>
      <option value="OWNER_APPROVED_UPSTREAM_V1">{{ $t("workflowResults.reworkUpstream") }}</option>
      <option v-if="value !== '' && value !== 'OWNER_APPROVED_UPSTREAM_V1'" :value="value" disabled>{{ $t("workflowResults.reworkUnknown") }}</option>
    </select>
    <small>{{ $t("workflowResults.reworkHelp") }}</small>
  </label>
</template>
