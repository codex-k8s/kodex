import { onBeforeUnmount, onMounted, type ComputedRef } from "vue";
import { useRouter } from "vue-router";

export function useUnsavedChanges(
  dirty: ComputedRef<boolean>,
  message: () => string,
  options: { ignoreQueryOnly?: boolean } = {},
): void {
  const router = useRouter();
  const confirmLeave = () => !dirty.value || window.confirm(message());
  const removeNavigationGuard = router.beforeEach((to, from) =>
    options.ignoreQueryOnly && to.path === from.path ? true : confirmLeave(),
  );
  function beforeUnload(event: BeforeUnloadEvent): void {
    if (!dirty.value) return;
    event.preventDefault();
  }
  onMounted(() => window.addEventListener("beforeunload", beforeUnload));
  onBeforeUnmount(() => {
    removeNavigationGuard();
    window.removeEventListener("beforeunload", beforeUnload);
  });
}
