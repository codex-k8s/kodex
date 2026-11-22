import {
  computed,
  onScopeDispose,
  ref,
  shallowRef,
  watch,
  type Ref,
} from "vue";
import * as api from "@/shared/api/kubernetes-capture";
import type { KubernetesCaptureReadback } from "@/shared/api/generated/openapi/types.gen";
import { idempotencyKey } from "@/shared/api/mutation";
import { ownerInvalidationSignal } from "@/shared/api/owner-lifetime";
import { asProblem, type AppProblem } from "@/shared/api/problem";
export type KubernetesCaptureAPI = Pick<
  typeof api,
  | "readKubernetesCapture"
  | "repairKubernetesCapture"
  | "repairKubernetesStagingCleanup"
>;
// Один owner-bound lifecycle: cancel/discard поздних readback и ни одного автоматического repair POST.
export function useKubernetesCapture(
  context: Ref<api.KubernetesCaptureContext & { captureRef: string }>,
  port: KubernetesCaptureAPI = api,
) {
  const current = shallowRef<KubernetesCaptureReadback>();
  const problem = ref<AppProblem>();
  const inactive = ref(false);
  const hidden = ref(false);
  const busy = ref(false),
    uncertain = ref(false);
  const owner = ownerInvalidationSignal();
  let controller: AbortController | undefined,
    generation = 0,
    disposed = false;
  const canRepair = computed(
    () =>
      !disposed &&
      !owner.aborted &&
      !busy.value &&
      !uncertain.value &&
      !hidden.value &&
      current.value?.view.summary.state === "WAITING_OWNER",
  );
  const canRepairCleanup = computed(
    () =>
      !disposed &&
      !owner.aborted &&
      !busy.value &&
      !uncertain.value &&
      !hidden.value &&
      (current.value?.view.cleanup?.eligibleExhaustedCount ?? 0) > 0,
  );
  function cancel() {
    generation++;
    controller?.abort();
    controller = undefined;
  }
  function hideCurrentView() {
    current.value = undefined;
    hidden.value = true;
  }
  function close() {
    disposed = true;
    inactive.value = true;
    cancel();
    hideCurrentView();
    problem.value = undefined;
    uncertain.value = false;
    busy.value = false;
  }
  owner.addEventListener("abort", close, { once: true });
  onScopeDispose(() => {
    owner.removeEventListener("abort", close);
    close();
  });
  function active(id: number) {
    return !disposed && !owner.aborted && id === generation;
  }
  async function refresh() {
    if (disposed || owner.aborted || busy.value) return;
    cancel();
    const id = generation,
      selected = { ...context.value },
      captureRef = selected.captureRef;
    controller = new AbortController();
    busy.value = true;
    problem.value = undefined;
    try {
      const value = await port.readKubernetesCapture(
        selected,
        captureRef,
        controller.signal,
      );
      if (active(id)) {
        current.value = value;
        hidden.value = false;
      }
    } catch (error) {
      if (active(id)) {
        problem.value = asProblem(error);
        hideCurrentView();
      }
    } finally {
      if (active(id)) busy.value = false;
    }
  }
  async function repairSelected(cleanup = false) {
    if (!(cleanup ? canRepairCleanup.value : canRepair.value) || !current.value)
      return;
    cancel();
    const id = generation,
      selected = { ...context.value },
      view = current.value.view,
      key = idempotencyKey();
    controller = new AbortController();
    const signal = controller.signal;
    busy.value = true;
    problem.value = undefined;
    try {
      const value = await (
        cleanup
          ? port.repairKubernetesStagingCleanup
          : port.repairKubernetesCapture
      )(selected, view, signal, key);
      if (active(id)) {
        current.value = value;
        hidden.value = false;
        uncertain.value = false;
      }
    } catch (error) {
      if (!active(id)) return;
      problem.value = asProblem(error);
      uncertain.value = true;
      // Только один metadata read исходного key. Повторный POST и native execution отсутствуют.
      try {
        const value = await port.readKubernetesCapture(
          selected,
          view.summary.captureRef,
          signal,
          cleanup ? "" : key,
          cleanup ? key : "",
        );
        if (active(id)) {
          current.value = value;
          hidden.value = false;
          uncertain.value = !(cleanup
            ? value.cleanupReceipt
            : value.repairReceipt);
        }
      } catch (error) {
        if (active(id)) {
          problem.value = asProblem(error);
          hideCurrentView();
        }
        /* Исход mutation остаётся неизвестным; автоматического повтора нет. */
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
      context.value.captureRef,
    ],
    () => {
      cancel();
      current.value = undefined;
      hidden.value = false;
      problem.value = undefined;
      busy.value = false;
      uncertain.value = false;
      void refresh();
    },
    { immediate: true },
  );

  return {
    current,
    problem,
    busy,
    uncertain,
    canRepair,
    canRepairCleanup,
    inactive,
    hidden,
    refresh,
    repair: () => repairSelected(),
    repairCleanup: () => repairSelected(true),
  };
}
