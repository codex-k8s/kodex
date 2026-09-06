<script setup lang="ts">
import { ref } from "vue";
import { Maximize2, X } from "@lucide/vue";
import type { SkillBundleFileInput } from "@/shared/api/generated/openapi/types.gen";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import { skillPathBytes, validSkillPath } from "./skill-import";
const props = defineProps<{
  modelValue: SkillBundleFileInput[];
  disabled?: boolean;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: SkillBundleFileInput[]];
}>();
const expanded = ref(false);
function updatePath(index: number, event: Event): void {
  if (props.disabled || !(event.target instanceof HTMLInputElement)) return;
  const path = event.target.value;
  emit(
    "update:modelValue",
    props.modelValue.map((file, position) =>
      position === index ? { ...file, path } : file,
    ),
  );
}
function remove(index: number): void {
  if (!props.disabled)
    emit(
      "update:modelValue",
      props.modelValue.filter((_, position) => position !== index),
    );
}
</script>
<template>
  <div class="manifest-heading">
    <h3>{{ $t("contextResources.skillFiles") }}</h3>
    <span>{{ modelValue.length }} / 128</span>
    <button
      v-if="modelValue.length > 6"
      type="button"
      class="icon-button"
      :title="$t('managed.expandFields')"
      :aria-label="$t('managed.expandFields')"
      @click="expanded = true"
    >
      <Maximize2 :size="18" />
    </button>
  </div>
  <component
    :is="expanded ? ModalDialog : 'div'"
    :title="$t('contextResources.skillFiles')"
    size="xl"
    @close="expanded = false"
  >
    <div
      v-for="(file, index) in expanded ? modelValue : modelValue.slice(0, 6)"
      :key="index"
      class="context-file"
    >
      <label
        >{{ $t("contextResources.path") }}
        <input
          :value="file.path"
          :aria-label="$t('contextResources.path')"
          maxlength="240"
          :disabled="disabled"
          :aria-invalid="!validSkillPath(file.path) || undefined"
          @input="updatePath(index, $event)"
        />
        <span>{{ skillPathBytes(file.path) }} / 240 B</span>
      </label>
      <code>{{ file.artifactRef }} / r{{ file.artifactRevision }}</code>
      <button
        type="button"
        class="icon-button"
        :disabled="disabled"
        :title="$t('common.delete')"
        :aria-label="$t('common.delete')"
        @click="remove(index)"
      >
        <X :size="18" />
      </button>
    </div>
  </component>
</template>
<style scoped>
.manifest-heading {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  align-items: center;
}
.manifest-heading h3 {
  margin: 0;
  font-size: 16px;
}
.context-file {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) 40px;
  align-items: center;
  gap: 12px;
  min-height: 104px;
  border-bottom: 1px solid var(--border);
}
.context-file label {
  display: grid;
  gap: 6px;
  min-width: 0;
}
.context-file input {
  min-width: 0;
  width: 100%;
  box-sizing: border-box;
}
.context-file code {
  overflow-wrap: anywhere;
}
@media (max-width: 600px) {
  .context-file {
    grid-template-columns: minmax(0, 1fr) 40px;
    min-height: 156px;
  }
  .context-file label {
    grid-column: 1 / -1;
  }
}
</style>
