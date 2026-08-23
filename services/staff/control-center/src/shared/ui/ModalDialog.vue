<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from "vue";

import {
  focusableElements,
  trappedFocusTarget,
} from "@/shared/ui/dialog-focus";

const props = defineProps<{ title: string; busy?: boolean }>();
const emit = defineEmits<{ close: [] }>();
const panel = ref<HTMLElement>();
let returnFocusTo: HTMLElement | null = null;

function handleKeydown(event: KeyboardEvent): void {
  if (event.key === "Escape") {
    if (!props.busy) emit("close");
    return;
  }
  if (event.key !== "Tab" || !panel.value) return;
  const target = trappedFocusTarget(
    focusableElements(panel.value),
    document.activeElement,
    event.shiftKey,
  );
  if (!target) return;
  event.preventDefault();
  target.focus();
}

onMounted(() => {
  returnFocusTo =
    document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null;
  void nextTick(() => {
    if (!panel.value?.contains(document.activeElement)) panel.value?.focus();
  });
});
onBeforeUnmount(() => {
  if (returnFocusTo?.isConnected) returnFocusTo.focus();
});
</script>

<template>
  <div
    class="modal-backdrop"
    role="presentation"
    @mousedown.self="!busy && emit('close')"
  >
    <section
      ref="panel"
      class="modal"
      role="dialog"
      aria-modal="true"
      :aria-label="title"
      tabindex="-1"
      @keydown="handleKeydown"
    >
      <header class="modal__header">
        <h2>{{ title }}</h2>
        <button
          class="icon-button"
          type="button"
          :aria-label="$t('common.close')"
          :disabled="busy"
          @click="emit('close')"
        >
          ×
        </button>
      </header>
      <div class="modal__body"><slot /></div>
      <footer class="modal__footer"><slot name="actions" /></footer>
    </section>
  </div>
</template>
