<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  ref,
  useId,
  watch,
  type CSSProperties,
} from "vue";

import { focusableElements } from "@/shared/ui/dialog-focus";
import {
  canDismissPopover,
  calculatePopoverPosition,
  restorePopoverFocus,
  shouldRestorePopoverFocus,
  type DismissiblePopoverCloseReason,
  type DismissiblePopoverPlacement,
} from "@/shared/ui/dismissible-popover";

export type DismissiblePopoverRole = "dialog" | "menu" | "listbox";
export type DismissiblePopoverWidth = "sm" | "md" | "lg" | "anchor";

const props = withDefaults(
  defineProps<{
    open: boolean;
    ariaLabel: string;
    role?: DismissiblePopoverRole;
    placement?: DismissiblePopoverPlacement;
    width?: DismissiblePopoverWidth;
    focusOnOpen?: boolean;
    closeOnEscape?: boolean;
    closeOnOutside?: boolean;
    block?: boolean;
    contained?: boolean;
    teleportTo?: string;
  }>(),
  {
    role: "dialog",
    placement: "bottom-start",
    width: "md",
    focusOnOpen: true,
    closeOnEscape: true,
    closeOnOutside: true,
    teleportTo: "body",
  },
);
const emit = defineEmits<{
  "update:open": [open: boolean];
  close: [reason: DismissiblePopoverCloseReason];
}>();

const anchor = ref<HTMLElement>();
const panel = ref<HTMLElement>();
const popoverId = `dismissible-popover-${useId()}`;
const positioned = ref(false);
const position = ref({ left: 0, maxHeight: 0, top: 0 });
let returnFocusTo: HTMLElement | null = null;
let resizeObserver: ResizeObserver | undefined;
let listening = false;
let restoreFocusAfterClose = true;

const triggerAttrs = computed(() => ({
  "aria-controls": props.open ? popoverId : undefined,
  "aria-expanded": props.open,
  "aria-haspopup": props.role,
}));
const panelStyle = computed<CSSProperties>(() => ({
  left: `${position.value.left.toString()}px`,
  maxHeight: `${position.value.maxHeight.toString()}px`,
  top: `${position.value.top.toString()}px`,
  visibility: positioned.value ? "visible" : "hidden",
  ...(props.width === "anchor" && anchor.value
    ? { width: `${anchor.value.getBoundingClientRect().width.toString()}px` }
    : {}),
}));
const teleportTarget = computed<string | HTMLElement>(() => {
  if (props.teleportTo !== "body" || typeof document === "undefined")
    return props.teleportTo;
  return (
    anchor.value?.closest<HTMLElement>('[role="dialog"][aria-modal="true"]') ??
    document.body
  );
});

function updatePosition(): void {
  if (!props.open || !anchor.value || !panel.value) return;
  position.value = calculatePopoverPosition({
    anchor: anchor.value.getBoundingClientRect(),
    panelHeight: panel.value.getBoundingClientRect().height,
    panelWidth: panel.value.getBoundingClientRect().width,
    placement: props.placement,
    viewportHeight: window.innerHeight,
    viewportWidth: window.innerWidth,
  });
  positioned.value = true;
}

function restoreFocus(): void {
  restorePopoverFocus(returnFocusTo, restoreFocusAfterClose);
  returnFocusTo = null;
  restoreFocusAfterClose = true;
}

function requestClose(reason: DismissiblePopoverCloseReason): void {
  if (
    !canDismissPopover(reason, {
      closeOnEscape: props.closeOnEscape,
      closeOnOutside: props.closeOnOutside,
    })
  )
    return;
  restoreFocusAfterClose = shouldRestorePopoverFocus(reason);
  emit("update:open", false);
  emit("close", reason);
}

function toggle(): void {
  if (props.open) requestClose("toggle");
  else emit("update:open", true);
}

function handlePointerDown(event: PointerEvent): void {
  if (!props.open || !props.closeOnOutside) return;
  const target = event.target;
  if (!(target instanceof Node)) return;
  if (anchor.value?.contains(target) || panel.value?.contains(target)) return;
  requestClose("outside");
}

function handleKeydown(event: KeyboardEvent): void {
  if (!props.open || event.key !== "Escape" || !props.closeOnEscape) return;
  event.preventDefault();
  event.stopPropagation();
  requestClose("escape");
}

function startListening(): void {
  if (listening || typeof document === "undefined") return;
  document.addEventListener("pointerdown", handlePointerDown, true);
  document.addEventListener("keydown", handleKeydown, true);
  window.addEventListener("resize", updatePosition);
  window.addEventListener("scroll", updatePosition, true);
  if (typeof ResizeObserver !== "undefined") {
    resizeObserver = new ResizeObserver(updatePosition);
    if (anchor.value) resizeObserver.observe(anchor.value);
    if (panel.value) resizeObserver.observe(panel.value);
  }
  listening = true;
}

function stopListening(): void {
  if (!listening || typeof document === "undefined") return;
  document.removeEventListener("pointerdown", handlePointerDown, true);
  document.removeEventListener("keydown", handleKeydown, true);
  window.removeEventListener("resize", updatePosition);
  window.removeEventListener("scroll", updatePosition, true);
  resizeObserver?.disconnect();
  resizeObserver = undefined;
  listening = false;
}

watch(
  () => props.open,
  async (open) => {
    if (!open) {
      stopListening();
      restoreFocus();
      return;
    }
    returnFocusTo =
      typeof document !== "undefined" &&
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
    positioned.value = false;
    await nextTick();
    updatePosition();
    startListening();
    if (props.focusOnOpen) {
      const target = panel.value
        ? (focusableElements(panel.value)[0] ?? panel.value)
        : undefined;
      target?.focus();
    }
  },
  { immediate: true },
);

onBeforeUnmount(() => {
  stopListening();
  restoreFocus();
});
</script>

<template>
  <span
    ref="anchor"
    class="dismissible-popover__anchor"
    :class="{ 'dismissible-popover__anchor--block': block }"
  >
    <slot name="trigger" :open="open" :toggle="toggle" :attrs="triggerAttrs" />
  </span>
  <Teleport :to="teleportTarget">
    <div
      v-if="open"
      :id="popoverId"
      ref="panel"
      class="dismissible-popover"
      :class="[
        `dismissible-popover--${width}`,
        { 'dismissible-popover--contained': contained },
      ]"
      :role="role"
      :aria-label="ariaLabel"
      :style="panelStyle"
      tabindex="-1"
    >
      <slot :close="() => requestClose('programmatic')" />
    </div>
  </Teleport>
</template>

<style scoped>
.dismissible-popover__anchor {
  display: inline-flex;
  min-width: 0;
  max-width: 100%;
}
.dismissible-popover__anchor--block {
  display: flex;
  width: 100%;
}
.dismissible-popover {
  position: fixed;
  z-index: 1100;
  min-width: 0;
  max-width: calc(100vw - 16px);
  overflow: auto;
  overscroll-behavior: contain;
  border: 1px solid var(--border);
  border-radius: 8px;
  outline: none;
  background: var(--surface);
  box-shadow: 0 12px 32px rgb(16 22 30 / 18%);
  color: var(--text);
}
.dismissible-popover--contained {
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.dismissible-popover--sm {
  width: min(240px, calc(100vw - 16px));
}
.dismissible-popover--md {
  width: min(320px, calc(100vw - 16px));
}
.dismissible-popover--lg {
  width: min(420px, calc(100vw - 16px));
}
.dismissible-popover--anchor {
  width: auto;
}
</style>
