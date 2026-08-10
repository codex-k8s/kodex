<script setup lang="ts">
import { X } from "@lucide/vue";
import { nextTick, onBeforeUnmount, ref, watch } from "vue";

const props = defineProps<{ open: boolean; title: string }>();
const emit = defineEmits<{ close: [] }>();
const dialog = ref<HTMLElement | null>(null);
let previousFocus: HTMLElement | null = null;

const focusableSelector =
  'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])';

function handleKeydown(event: KeyboardEvent): void {
  if (!props.open || !dialog.value) return;
  if (event.key === "Escape") {
    event.preventDefault();
    emit("close");
    return;
  }
  if (event.key !== "Tab") return;
  const focusable = [
    ...dialog.value.querySelectorAll<HTMLElement>(focusableSelector),
  ];
  if (focusable.length === 0) {
    event.preventDefault();
    dialog.value.focus();
    return;
  }
  const first = focusable[0];
  const last = focusable.at(-1);
  if (
    (event.shiftKey && document.activeElement === first) ||
    (!event.shiftKey && document.activeElement === last)
  ) {
    event.preventDefault();
    (event.shiftKey ? last : first)?.focus();
  }
}

watch(
  () => props.open,
  async (open) => {
    if (open) {
      previousFocus =
        document.activeElement instanceof HTMLElement
          ? document.activeElement
          : null;
      document.addEventListener("keydown", handleKeydown);
      await nextTick();
      dialog.value?.querySelector<HTMLElement>(focusableSelector)?.focus();
      if (document.activeElement === previousFocus) dialog.value?.focus();
    } else {
      document.removeEventListener("keydown", handleKeydown);
      previousFocus?.focus();
      previousFocus = null;
    }
  },
);

onBeforeUnmount(() => document.removeEventListener("keydown", handleKeydown));
</script>

<template>
  <Teleport to="body">
    <div
      v-if="open"
      class="modal-backdrop"
      role="presentation"
      @mousedown.self="$emit('close')"
    >
      <section
        ref="dialog"
        class="modal"
        role="dialog"
        aria-modal="true"
        :aria-label="title"
        tabindex="-1"
      >
        <header class="modal__header">
          <h2>{{ title }}</h2>
          <button
            class="icon-button"
            type="button"
            :aria-label="$t('common.close')"
            @click="$emit('close')"
          >
            <X :size="18" aria-hidden="true" />
          </button>
        </header>
        <div class="modal__body"><slot /></div>
      </section>
    </div>
  </Teleport>
</template>
