<script setup lang="ts">
import { computed, watch } from "vue";

import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";

const props = withDefaults(
  defineProps<{
    name: string;
    purpose: string;
    language: string;
    disabled?: boolean;
    initialFocus?: boolean;
  }>(),
  { disabled: false, initialFocus: false },
);
const emit = defineEmits<{
  "update:name": [value: string];
  "update:purpose": [value: string];
  "update:language": [value: "ru" | "en"];
  valid: [value: boolean];
}>();
const valid = computed(
  () =>
    props.name.trim().length > 0 &&
    props.name.length <= 120 &&
    props.purpose.trim().length > 0 &&
    props.purpose.length <= 1000 &&
    (props.language === "ru" || props.language === "en"),
);
watch(valid, (value) => emit("valid", value), { immediate: true });
</script>

<template>
  <div class="project-form-fields">
    <label class="field field--wide">
      <span>{{ $t("common.name") }}</span>
      <input
        :value="name"
        :disabled="disabled"
        required
        maxlength="120"
        :data-dialog-initial-focus="initialFocus || undefined"
        @input="
          emit('update:name', ($event.target as HTMLInputElement).value.trim())
        "
      />
    </label>
    <label class="field field--wide">
      <span>{{ $t("common.purpose") }}</span>
      <VoiceTextarea
        :model-value="purpose"
        :disabled="disabled"
        required
        maxlength="1000"
        @update:model-value="emit('update:purpose', $event.trim())"
      />
    </label>
    <label class="field">
      <span>{{ $t("projects.language") }}</span>
      <select
        :value="language"
        :disabled="disabled"
        @change="
          emit(
            'update:language',
            ($event.target as HTMLSelectElement).value as 'ru' | 'en',
          )
        "
      >
        <option value="ru">{{ $t("common.russian") }}</option>
        <option value="en">{{ $t("common.english") }}</option>
      </select>
    </label>
  </div>
</template>

<style scoped>
.project-form-fields {
  display: contents;
}
.field {
  display: grid;
  gap: 5px;
  min-width: 0;
}
.field--wide {
  grid-column: 1 / -1;
}
</style>
