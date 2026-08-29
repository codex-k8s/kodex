import { nextTick, onBeforeUnmount, onMounted, type Ref } from "vue";

export type DismissibleLayerCloseReason = "escape" | "outside";

export interface DismissibleLayerOptions {
  enabled?: Readonly<Ref<boolean>>;
  returnFocusTo?: Readonly<Ref<HTMLElement | undefined>>;
}

export function useDismissibleLayer(
  root: Ref<HTMLElement | undefined>,
  close: (reason: DismissibleLayerCloseReason) => void,
  options: DismissibleLayerOptions = {},
): void {
  function handlePointerDown(event: PointerEvent): void {
    if (options.enabled && !options.enabled.value) return;
    const target = event.target;
    if (target instanceof Node && !root.value?.contains(target))
      close("outside");
  }

  function handleKeyDown(event: KeyboardEvent): void {
    if (event.key !== "Escape" || (options.enabled && !options.enabled.value))
      return;
    event.preventDefault();
    close("escape");
    void nextTick(() => options.returnFocusTo?.value?.focus());
  }

  onMounted(() => {
    document.addEventListener("pointerdown", handlePointerDown, true);
    document.addEventListener("keydown", handleKeyDown);
  });
  onBeforeUnmount(() => {
    document.removeEventListener("pointerdown", handlePointerDown, true);
    document.removeEventListener("keydown", handleKeyDown);
  });
}
