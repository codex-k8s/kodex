<script setup lang="ts">
import { X } from "@lucide/vue";
import { nextTick, onBeforeUnmount, onMounted, ref, useId } from "vue";

import {
  focusableElements,
  trappedFocusTarget,
} from "@/shared/ui/dialog-focus";

export type ModalDialogSize = "sm" | "md" | "lg" | "xl" | "full";

const props = withDefaults(
  defineProps<{
    title: string;
    busy?: boolean;
    size?: ModalDialogSize;
  }>(),
  {
    busy: false,
    size: "md",
  },
);
const emit = defineEmits<{ close: [] }>();
const panel = ref<HTMLElement>();
const titleId = `modal-title-${useId()}`;
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
      :class="`modal--${size}`"
      role="dialog"
      aria-modal="true"
      :aria-busy="busy || undefined"
      :aria-labelledby="titleId"
      tabindex="-1"
      @keydown="handleKeydown"
    >
      <header class="modal__header">
        <h2 :id="titleId">{{ title }}</h2>
        <button
          class="icon-button"
          type="button"
          :aria-label="$t('common.close')"
          :disabled="busy"
          @click="emit('close')"
        >
          <X :size="18" aria-hidden="true" />
        </button>
      </header>
      <div class="modal__body"><slot /></div>
      <footer class="modal__footer"><slot name="actions" /></footer>
    </section>
  </div>
</template>

<style scoped>
.modal {
  display: flex;
  width: min(var(--modal-inline-size), 100%);
  min-width: 0;
  max-width: calc(100vw - 40px);
  max-height: calc(100dvh - 40px);
  flex-direction: column;
  overflow: hidden;
}
.modal--sm {
  --modal-inline-size: 420px;
}
.modal--md {
  --modal-inline-size: 680px;
}
.modal--lg {
  --modal-inline-size: 840px;
}
.modal--xl {
  --modal-inline-size: 1080px;
}
.modal--full {
  --modal-inline-size: calc(100vw - 40px);

  height: calc(100dvh - 40px);
}
.modal__header,
.modal__footer {
  flex: 0 0 auto;
}
.modal__header h2 {
  min-width: 0;
  overflow-wrap: anywhere;
}
.modal__body {
  width: 100%;
  min-width: 0;
  min-height: 0;
  max-width: 100%;
  flex: 1 1 auto;
  overflow-x: hidden;
  overflow-y: auto;
  overscroll-behavior: contain;
  overflow-wrap: anywhere;
}
.modal__body > * {
  min-width: 0;
  max-width: 100%;
}
@media (max-width: 520px) {
  .modal {
    width: 100%;
    max-width: 100vw;
    max-height: 88dvh;
  }
  .modal--full {
    width: 100vw;
    height: 100dvh;
    max-height: 100dvh;
    border-radius: 0;
  }
}
</style>
