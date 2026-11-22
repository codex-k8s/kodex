import { onScopeDispose, ref, shallowRef, watch, type Ref } from "vue";
import * as api from "@/shared/api/kubernetes-capture";
import type { KubernetesCaptureStatus } from "@/shared/api/generated/openapi/types.gen";
import { ownerInvalidationSignal } from "@/shared/api/owner-lifetime";
import { asProblem, type AppProblem } from "@/shared/api/problem";

// Только current original-owner history; любой denied/stale ответ очищает прежнюю проекцию.
export function useKubernetesCaptureHistory(
  context: Ref<Pick<api.KubernetesCaptureContext, "runRef" | "nodeRef">>,
  port: Pick<typeof api, "listKubernetesCaptures"> = api,
) {
  const items = shallowRef<KubernetesCaptureStatus[]>([]),
    nextPageToken = ref(""),
    problem = ref<AppProblem>(),
    busy = ref(false);
  const owner = ownerInvalidationSignal();
  let disposed = false,
    generation = 0,
    controller: AbortController | undefined,
    timer: ReturnType<typeof setTimeout> | undefined;
  function cancel() {
    generation++;
    controller?.abort();
    controller = undefined;
    if (timer !== undefined) clearTimeout(timer);
    timer = undefined;
  }
  function current(id: number) {
    return !disposed && !owner.aborted && id === generation;
  }
  function close() {
    disposed = true;
    cancel();
    items.value = [];
    nextPageToken.value = "";
    problem.value = undefined;
    busy.value = false;
  }
  owner.addEventListener("abort", close, { once: true });
  onScopeDispose(() => {
    owner.removeEventListener("abort", close);
    close();
  });
  async function refresh(token = "") {
    if (disposed || owner.aborted) return;
    cancel();
    const id = generation,
      selected = { ...context.value };
    controller = new AbortController();
    busy.value = true;
    problem.value = undefined;
    try {
      const page = await port.listKubernetesCaptures(
        selected,
        controller.signal,
        token,
      );
      if (current(id)) {
        items.value = page.items;
        nextPageToken.value = page.nextPageToken;
      }
    } catch (error) {
      if (current(id)) {
        items.value = [];
        nextPageToken.value = "";
        problem.value = asProblem(error);
      }
    } finally {
      if (current(id)) {
        busy.value = false;
        if (!token) timer = setTimeout(() => void refresh(), 5000);
      }
    }
  }
  watch(
    () => [context.value.runRef, context.value.nodeRef],
    () => {
      cancel();
      items.value = [];
      nextPageToken.value = "";
      problem.value = undefined;
      busy.value = false;
      void refresh();
    },
    { immediate: true },
  );
  return { items, nextPageToken, problem, busy, refresh };
}
