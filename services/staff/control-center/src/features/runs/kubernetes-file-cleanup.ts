import {
  computed,
  onScopeDispose,
  shallowRef,
  watch,
  type ComputedRef,
} from "vue";
import * as api from "@/shared/api/kubernetes-file-input";
import type { KubernetesFileCleanupReadback } from "@/shared/api/generated/openapi/types.gen";
import { idempotencyKey } from "@/shared/api/mutation";
import { ownerInvalidationSignal } from "@/shared/api/owner-lifetime";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import type { KubernetesFileContext } from "./kubernetes-file-input";
// Отдельное чтение не наследует provenance из props. Неизвестный POST сверяется один раз без повторной записи.
export function useKubernetesFileCleanup(
  context: ComputedRef<KubernetesFileContext & { sourceRef: string }>,
  port: Pick<
    typeof api,
    "readKubernetesFileCleanup" | "repairKubernetesFileCleanup"
  > = api,
) {
  const current = shallowRef<KubernetesFileCleanupReadback>(),
    problem = shallowRef<AppProblem>(),
    busy = shallowRef(false),
    uncertain = shallowRef(false),
    closed = shallowRef(false),
    owner = ownerInvalidationSignal();
  let generation = 0,
    controller: AbortController | undefined,
    pendingKey = "";
  const inactive = computed(() => closed.value || owner.aborted);
  const canRepair = computed(
    () =>
      !inactive.value &&
      !busy.value &&
      !uncertain.value &&
      current.value?.cleanupView.repairAvailable === true &&
      current.value.cleanupView.cleanup.eligibleExhaustedCount === 1,
  );
  function cancel() {
    generation++;
    controller?.abort();
    controller = undefined;
  }
  function active(id: number) {
    return !inactive.value && id === generation;
  }
  function close() {
    closed.value = true;
    cancel();
    current.value = undefined;
    problem.value = undefined;
    busy.value = false;
  }
  owner.addEventListener("abort", close, { once: true });
  onScopeDispose(() => {
    owner.removeEventListener("abort", close);
    close();
  });
  async function refresh() {
    if (inactive.value || busy.value) return;
    cancel();
    const id = generation,
      selected = { ...context.value };
    controller = new AbortController();
    const signal = AbortSignal.any([controller.signal, owner]);
    busy.value = true;
    problem.value = undefined;
    // Даже пока read in flight не показывается ранее разрешённый статус.
    current.value = undefined;
    try {
      const result = await port.readKubernetesFileCleanup(
        selected.invocationRef,
        selected.sourceRef,
        signal,
        pendingKey,
      );
      if (active(id)) {
        current.value = result;
        if (pendingKey) uncertain.value = !result.cleanupReceipt;
      }
    } catch (error) {
      if (active(id)) {
        current.value = undefined;
        problem.value = asProblem(error);
      }
    } finally {
      if (active(id)) busy.value = false;
    }
  }
  async function repair() {
    if (!canRepair.value || !current.value) return;
    cancel();
    const id = generation,
      selected = { ...context.value },
      view = current.value.cleanupView;
    controller = new AbortController();
    const signal = AbortSignal.any([controller.signal, owner]);
    pendingKey = idempotencyKey();
    busy.value = true;
    problem.value = undefined;
    try {
      const result = await port.repairKubernetesFileCleanup(
        selected.invocationRef,
        view,
        signal,
        pendingKey,
      );
      if (active(id)) {
        current.value = result;
        uncertain.value = false;
        pendingKey = "";
      }
    } catch (error) {
      if (!active(id)) return;
      uncertain.value = true;
      current.value = undefined;
      problem.value = asProblem(error);
      try {
        const result = await port.readKubernetesFileCleanup(
          selected.invocationRef,
          selected.sourceRef,
          signal,
          pendingKey,
        );
        if (active(id)) {
          current.value = result;
          uncertain.value = !result.cleanupReceipt;
          if (!uncertain.value) {
            pendingKey = "";
            problem.value = undefined;
          }
        }
      } catch (error) {
        if (active(id)) {
          current.value = undefined;
          problem.value = asProblem(error);
        }
      }
    } finally {
      if (active(id)) busy.value = false;
    }
  }
  watch(
    () => [
      context.value.invocationRef,
      context.value.runRef,
      context.value.nodeRef,
      context.value.sourceRef,
    ],
    () => {
      cancel();
      current.value = undefined;
      problem.value = undefined;
      busy.value = false;
      uncertain.value = false;
      pendingKey = "";
      void refresh();
    },
    { immediate: true },
  );
  return {
    current,
    problem,
    busy,
    uncertain,
    inactive,
    canRepair,
    refresh,
    repair,
  };
}
