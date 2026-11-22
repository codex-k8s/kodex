import {
  computed,
  onScopeDispose,
  shallowRef,
  watch,
  type ComputedRef,
} from "vue";
import * as api from "@/shared/api/kubernetes-file-input";
import type {
  KubernetesFileInputView,
  KubernetesFileCleanupView,
} from "@/shared/api/generated/openapi/types.gen";
import { ownerInvalidationSignal } from "@/shared/api/owner-lifetime";
import { asProblem, type AppProblem } from "@/shared/api/problem";
export interface KubernetesFileContext {
  invocationRef: string;
  runRef: string;
  nodeRef: string;
  active: boolean;
}
// Read-only poll не повторяет подготовку/INPUT. Context change, owner invalidation и любой error скрывают cached metadata.
export function useKubernetesFileInputs(
  context: ComputedRef<KubernetesFileContext>,
  port: Pick<
    typeof api,
    "listKubernetesFileInputs" | "listKubernetesFileCleanup"
  > = api,
) {
  const items = shallowRef<KubernetesFileInputView[]>([]),
    cleanupItems = shallowRef<KubernetesFileCleanupView[]>([]),
    problem = shallowRef<AppProblem>(),
    busy = shallowRef(false),
    invalidated = shallowRef(false),
    owner = ownerInvalidationSignal();
  let generation = 0,
    disposed = false,
    controller: AbortController | undefined,
    timer: ReturnType<typeof setTimeout> | undefined;
  const inactive = computed(
    () => disposed || invalidated.value || owner.aborted,
  );
  function cancel() {
    generation++;
    controller?.abort();
    controller = undefined;
    if (timer !== undefined) clearTimeout(timer);
    timer = undefined;
  }
  function isCurrent(id: number, signal: AbortSignal) {
    return !disposed && !signal.aborted && generation === id;
  }
  async function refresh() {
    if (inactive.value) return;
    cancel();
    const id = generation,
      invocationRef = context.value.invocationRef;
    controller = new AbortController();
    const signal = AbortSignal.any([controller.signal, owner]);
    busy.value = true;
    problem.value = undefined;
    try {
      const result = await port.listKubernetesFileInputs(invocationRef, signal);
      if (isCurrent(id, signal)) {
        items.value = result;
        cleanupItems.value = result.flatMap((item) =>
          item.cleanup
            ? [
                {
                  sourceRef: item.sourceRef,
                  invocationRef: item.invocationRef,
                  cleanup: item.cleanup,
                },
              ]
            : [],
        );
      }
    } catch (error) {
      if (isCurrent(id, signal)) {
        items.value = [];
        cleanupItems.value = [];
        problem.value = asProblem(error);
        // Отдельный minimal read после source denial не возвращает скрытую provenance.
        if ([403, 404].includes(problem.value.status) && !signal.aborted) {
          try {
            const result = await port.listKubernetesFileCleanup(
              invocationRef,
              signal,
            );
            if (isCurrent(id, signal)) {
              cleanupItems.value = result;
              problem.value = undefined;
            }
          } catch (error) {
            if (isCurrent(id, signal)) problem.value = asProblem(error);
          }
        }
      }
    } finally {
      if (isCurrent(id, signal)) {
        busy.value = false;
        if (!signal.aborted && !problem.value && context.value.active)
          timer = setTimeout(() => void refresh(), 2000);
      }
    }
  }
  function invalidate() {
    cancel();
    items.value = [];
    cleanupItems.value = [];
    problem.value = undefined;
    busy.value = false;
  }
  watch(
    () => [
      context.value.invocationRef,
      context.value.runRef,
      context.value.nodeRef,
    ],
    () => {
      invalidate();
      void refresh();
    },
    { immediate: true },
  );
  watch(
    () => context.value.active,
    (active) => {
      if (!active && timer !== undefined) {
        clearTimeout(timer);
        timer = undefined;
      } else if (active && !busy.value) void refresh();
    },
  );
  function invalidateOwner() {
    invalidated.value = true;
    invalidate();
  }
  owner.addEventListener("abort", invalidateOwner, { once: true });
  onScopeDispose(() => {
    disposed = true;
    invalidate();
    owner.removeEventListener("abort", invalidateOwner);
  });
  return { items, cleanupItems, problem, busy, inactive, refresh };
}
