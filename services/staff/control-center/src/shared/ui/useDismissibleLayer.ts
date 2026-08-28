import { onBeforeUnmount, onMounted, type Ref } from "vue";

export function useDismissibleLayer(
  root: Ref<HTMLElement | undefined>,
  close: () => void,
): void {
  function handlePointerDown(event: PointerEvent): void {
    const target = event.target;
    if (target instanceof Node && !root.value?.contains(target)) close();
  }

  function handleKeyDown(event: KeyboardEvent): void {
    if (event.key === "Escape") close();
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
