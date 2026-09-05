import { onBeforeUnmount, onMounted, type ComputedRef } from "vue";
import { onBeforeRouteLeave, onBeforeRouteUpdate } from "vue-router";

export function useUnsavedChanges(
  dirty: ComputedRef<boolean>,
  message: () => string,
): void {
  const confirmLeave = () => !dirty.value || window.confirm(message());
  onBeforeRouteLeave(confirmLeave);
  onBeforeRouteUpdate(confirmLeave);
  function beforeUnload(event: BeforeUnloadEvent): void {
    if (!dirty.value) return;
    event.preventDefault();
  }
  onMounted(() => window.addEventListener("beforeunload", beforeUnload));
  onBeforeUnmount(() =>
    window.removeEventListener("beforeunload", beforeUnload),
  );
}
