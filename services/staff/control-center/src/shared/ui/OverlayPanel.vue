<script setup lang="ts">
import { X } from "@lucide/vue";
import { nextTick, onBeforeUnmount, ref, watch } from "vue";

import {
  focusableElements,
  trappedFocusTarget,
} from "@/shared/ui/dialog-focus";
import {
  canDismissOverlay,
  restoreOverlayFocus,
  type OverlayCloseReason,
} from "@/shared/ui/overlay-panel";

export type OverlayPanelMode = "modal" | "drawer" | "sheet" | "responsive";

const props = withDefaults(
  defineProps<{
    open: boolean;
    mode: OverlayPanelMode;
    ariaLabel: string;
    closeLabel: string;
    busy?: boolean;
    closeOnEscape?: boolean;
    closeOnOutside?: boolean;
    teleportTo?: string;
  }>(),
  {
    busy: false,
    closeOnEscape: true,
    closeOnOutside: true,
    teleportTo: "body",
  },
);
const emit = defineEmits<{
  "update:open": [open: boolean];
  close: [reason: OverlayCloseReason];
}>();

const panel = ref<HTMLElement>();
let returnFocusTo: HTMLElement | null = null;
let listening = false;

function requestClose(reason: OverlayCloseReason): void {
  if (
    !canDismissOverlay(reason, {
      busy: props.busy,
      closeOnEscape: props.closeOnEscape,
      closeOnOutside: props.closeOnOutside,
    })
  )
    return;
  emit("update:open", false);
  emit("close", reason);
}

function handleDocumentKeydown(event: KeyboardEvent): void {
  if (!props.open) return;
  if (event.key === "Escape") {
    event.preventDefault();
    requestClose("escape");
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

function startListening(): void {
  if (listening || typeof document === "undefined") return;
  document.addEventListener("keydown", handleDocumentKeydown, true);
  listening = true;
}

function stopListening(): void {
  if (!listening || typeof document === "undefined") return;
  document.removeEventListener("keydown", handleDocumentKeydown, true);
  listening = false;
}

watch(
  () => props.open,
  async (open) => {
    if (!open) {
      stopListening();
      restoreOverlayFocus(returnFocusTo);
      returnFocusTo = null;
      return;
    }
    returnFocusTo =
      typeof document !== "undefined" &&
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
    startListening();
    await nextTick();
    const initialTarget = panel.value
      ? focusableElements(panel.value)[0]
      : undefined;
    (initialTarget ?? panel.value)?.focus();
  },
  { immediate: true },
);

onBeforeUnmount(() => {
  stopListening();
  restoreOverlayFocus(returnFocusTo);
});
</script>

<template>
  <Teleport :to="teleportTo">
    <div
      v-if="open"
      class="overlay-panel__backdrop"
      :class="`overlay-panel__backdrop--${mode}`"
      role="presentation"
      @pointerdown.self="requestClose('outside')"
    >
      <section
        ref="panel"
        class="overlay-panel"
        :class="`overlay-panel--${mode}`"
        role="dialog"
        aria-modal="true"
        :aria-label="ariaLabel"
        tabindex="-1"
      >
        <header v-if="$slots.header" class="overlay-panel__header">
          <div class="overlay-panel__heading"><slot name="header" /></div>
          <button
            type="button"
            class="overlay-panel__close"
            :aria-label="closeLabel"
            :disabled="busy"
            @click="requestClose('button')"
          >
            <X :size="18" aria-hidden="true" />
          </button>
        </header>
        <div class="overlay-panel__body"><slot /></div>
        <footer v-if="$slots.footer" class="overlay-panel__footer">
          <slot name="footer" />
        </footer>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.overlay-panel__backdrop {
  position: fixed;
  z-index: 1000;
  inset: 0;
  display: flex;
  background: rgb(16 22 30 / 48%);
}
.overlay-panel__backdrop--modal {
  align-items: center;
  justify-content: center;
  padding: 20px;
}
.overlay-panel__backdrop--drawer,
.overlay-panel__backdrop--responsive {
  justify-content: flex-end;
}
.overlay-panel__backdrop--sheet {
  align-items: flex-end;
}
.overlay-panel {
  display: flex;
  min-width: 0;
  max-height: calc(100dvh - 40px);
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
  box-shadow: 0 18px 48px rgb(16 22 30 / 20%);
  color: var(--text);
}
.overlay-panel--modal {
  width: min(620px, 100%);
}
.overlay-panel--drawer,
.overlay-panel--responsive {
  width: min(520px, 100%);
  height: 100dvh;
  max-height: none;
  border-width: 0 0 0 1px;
  border-radius: 0;
}
.overlay-panel--sheet {
  width: 100%;
  max-height: min(78dvh, 720px);
  border-width: 1px 0 0;
  border-radius: 10px 10px 0 0;
}
.overlay-panel__header,
.overlay-panel__footer {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 12px;
  padding: 14px 16px;
}
.overlay-panel__header {
  border-bottom: 1px solid var(--border);
}
.overlay-panel__footer {
  justify-content: flex-end;
  border-top: 1px solid var(--border);
}
.overlay-panel__heading {
  min-width: 0;
  flex: 1;
}
.overlay-panel__close {
  display: inline-grid;
  width: 32px;
  height: 32px;
  flex: 0 0 auto;
  place-items: center;
  border: 1px solid transparent;
  border-radius: 6px;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
}
.overlay-panel__close:hover {
  border-color: var(--border);
  background: var(--panel);
  color: var(--text);
}
.overlay-panel__close:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}
.overlay-panel__body {
  min-height: 0;
  flex: 1;
  overflow: auto;
  padding: 16px;
}
@media (max-width: 760px) {
  .overlay-panel__backdrop--responsive {
    align-items: flex-end;
  }
  .overlay-panel--responsive {
    width: 100%;
    height: auto;
    max-height: 82dvh;
    border-width: 1px 0 0;
    border-radius: 10px 10px 0 0;
  }
  .overlay-panel__close {
    width: 44px;
    height: 44px;
  }
}
</style>
