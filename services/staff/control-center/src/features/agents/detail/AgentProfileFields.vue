<script setup lang="ts">
import { computed, watch } from "vue";

import type { AgentProfileDraft } from "./model";
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";

const props = defineProps<{
  modelValue: AgentProfileDraft;
  disabled?: boolean;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: AgentProfileDraft];
  valid: [value: boolean];
}>();

const valid = computed(() =>
  Boolean(
    props.modelValue.name.trim() &&
    props.modelValue.name.length <= 120 &&
    props.modelValue.purpose.trim() &&
    props.modelValue.purpose.length <= 1000 &&
    props.modelValue.roleDescription.trim() &&
    props.modelValue.roleDescription.length <= 1000,
  ),
);
watch(valid, (value) => emit("valid", value), { immediate: true });

function update(key: keyof AgentProfileDraft, value: string): void {
  if (props.disabled) return;
  emit("update:modelValue", { ...props.modelValue, [key]: value });
}
</script>

<template>
  <label class="field">
    <span>{{ $t("common.name") }}</span>
    <input
      :value="modelValue.name"
      required
      maxlength="120"
      :disabled="disabled"
      @input="update('name', ($event.target as HTMLInputElement).value)"
    />
  </label>
  <label class="field">
    <span>{{ $t("common.purpose") }}</span>
    <input
      :value="modelValue.purpose"
      required
      maxlength="1000"
      :disabled="disabled"
      @input="update('purpose', ($event.target as HTMLInputElement).value)"
    />
  </label>
  <label class="field field--wide">
    <span>{{ $t("agents.role") }}</span>
    <VoiceTextarea
      :model-value="modelValue.roleDescription"
      required
      maxlength="1000"
      :disabled="disabled"
      @update:model-value="update('roleDescription', $event)"
    />
  </label>
</template>

<style scoped>
.field--wide {
  grid-column: 1 / -1;
}
</style>
