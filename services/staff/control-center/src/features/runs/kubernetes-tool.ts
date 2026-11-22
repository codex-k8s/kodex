import { onScopeDispose, ref, shallowRef, watch, type Ref } from "vue";
import * as api from "@/shared/api/kubernetes-tool";
import type { KubernetesToolView } from "@/shared/api/generated/openapi/types.gen";
import { ownerInvalidationSignal } from "@/shared/api/owner-lifetime";
import { idempotencyKey } from "@/shared/api/mutation";
import { asProblem, type AppProblem } from "@/shared/api/problem";
export type KubernetesToolAPI = Pick<
  typeof api,
  "listKubernetesTools" | "readKubernetesTool" | "stopKubernetesTool"
>;

// Смена узла/пользователя отбрасывает поздние ответы; Stop не повторяется после потери ACK.
export function useKubernetesTools(
  context: Ref<api.KubernetesToolContext>,
  port: KubernetesToolAPI = api,
) {
  const items = shallowRef<KubernetesToolView[]>([]),
    problem = ref<AppProblem>(),
    nextPageToken = ref("");
  const busy = ref(false),
    writing = ref(false),
    uncertain = ref(false);
  const owner = ownerInvalidationSignal();
  let generation = 0,
    disposed = false,
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
    problem.value = undefined;
    busy.value = false;
    writing.value = false;
    uncertain.value = false;
    nextPageToken.value = "";
  }
  owner.addEventListener("abort", close, { once: true });
  onScopeDispose(() => {
    owner.removeEventListener("abort", close);
    close();
  });
  function schedule() {
    if (!disposed && !owner.aborted)
      timer = setTimeout(() => void refresh(), 5000);
  }
  async function refresh(token = "") {
    if (disposed || owner.aborted || writing.value) return;
    cancel();
    const id = generation,
      selected = { ...context.value };
    controller = new AbortController();
    const signal = controller.signal;
    busy.value = true;
    problem.value = undefined;
    try {
      const page = await port.listKubernetesTools(selected, signal, token);
      if (!current(id)) return;
      items.value = page.items;
      nextPageToken.value = page.nextPageToken;
      uncertain.value = false;
    } catch (error) {
      if (current(id)) {
        items.value = [];
        nextPageToken.value = "";
        problem.value = asProblem(error);
      }
    } finally {
      if (current(id)) {
        busy.value = false;
        if (!token) schedule();
      }
    }
  }
  async function stop(item: KubernetesToolView) {
    if (
      disposed ||
      owner.aborted ||
      busy.value ||
      writing.value ||
      uncertain.value ||
      !api.kubernetesToolIsActive(item) ||
      !items.value.some(
        (v) =>
          v.invocationRef === item.invocationRef && v.version === item.version,
      )
    )
      return;
    cancel();
    const id = generation;
    controller = new AbortController();
    const signal = controller.signal;
    writing.value = true;
    problem.value = undefined;
    uncertain.value = false;
    function replace(view: KubernetesToolView) {
      if (current(id))
        items.value = items.value.map((v) =>
          v.invocationRef === item.invocationRef ? view : v,
        );
    }
    try {
      replace(await port.stopKubernetesTool(item, signal, idempotencyKey()));
    } catch (error) {
      if (!current(id)) return;
      problem.value = asProblem(error);
      uncertain.value = true;
      try {
        const view = await port.readKubernetesTool(item, signal);
        if (current(id)) {
          replace(view);
          uncertain.value = api.kubernetesToolIsActive(view);
        }
      } catch {
        /* Неопределённость сохраняется; повторной mutation нет. */
      }
    } finally {
      if (current(id)) {
        writing.value = false;
        schedule();
      }
    }
  }
  watch(
    () => [context.value.runRef, context.value.nodeRef],
    () => {
      cancel();
      items.value = [];
      busy.value = false;
      writing.value = false;
      uncertain.value = false;
      nextPageToken.value = "";
      void refresh();
    },
    { immediate: true },
  );
  return {
    items,
    problem,
    nextPageToken,
    busy,
    writing,
    uncertain,
    refresh,
    stop,
  };
}
