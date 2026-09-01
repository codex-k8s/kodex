<script setup lang="ts">
import { Grid2X2, List } from "@lucide/vue";

import {
  viewModeFromNavigationKey,
  type ViewMode,
} from "@/shared/ui/view-mode-toggle";

const props = withDefaults(
  defineProps<{
    modelValue: ViewMode;
    ariaLabel: string;
    listLabel: string;
    gridLabel: string;
    disabled?: boolean;
  }>(),
  { disabled: false },
);
const emit = defineEmits<{ "update:modelValue": [mode: ViewMode] }>();

function select(mode: ViewMode): void {
  if (!props.disabled && mode !== props.modelValue)
    emit("update:modelValue", mode);
}

function handleKeydown(event: KeyboardEvent): void {
  const mode = viewModeFromNavigationKey(props.modelValue, event.key);
  if (!mode) return;
  event.preventDefault();
  select(mode);
}
</script>

<template>
  <div
    class="view-mode-toggle"
    role="group"
    :aria-label="ariaLabel"
    @keydown="handleKeydown"
  >
    <button
      type="button"
      :aria-label="listLabel"
      :title="listLabel"
      :aria-pressed="modelValue === 'list'"
      :disabled="disabled"
      @click="select('list')"
    >
      <List :size="17" aria-hidden="true" />
    </button>
    <button
      type="button"
      :aria-label="gridLabel"
      :title="gridLabel"
      :aria-pressed="modelValue === 'grid'"
      :disabled="disabled"
      @click="select('grid')"
    >
      <Grid2X2 :size="17" aria-hidden="true" />
    </button>
  </div>
</template>

<style scoped>
.view-mode-toggle {
  display: inline-grid;
  grid-template-columns: repeat(2, 32px);
  height: 32px;
  overflow: hidden;
  border: 1px solid var(--border-strong);
  border-radius: 6px;
  background: var(--surface);
}
.view-mode-toggle button {
  display: grid;
  width: 32px;
  height: 30px;
  place-items: center;
  border: 0;
  border-right: 1px solid var(--border);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
}
.view-mode-toggle button:last-child {
  border-right: 0;
}
.view-mode-toggle button[aria-pressed="true"] {
  background: var(--accent-soft);
  color: var(--accent);
}
.view-mode-toggle button:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}
@media (max-width: 760px) {
  .view-mode-toggle {
    grid-template-columns: repeat(2, 44px);
    height: 44px;
  }
  .view-mode-toggle button {
    width: 44px;
    height: 42px;
  }
}
</style>
