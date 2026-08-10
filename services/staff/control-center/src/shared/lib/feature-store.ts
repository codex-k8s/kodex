import { ref } from "vue";

import { asProblem, type AppProblem } from "@/shared/api/problem";
import {
  beginRequest,
  failRequest,
  finishRequest,
  type RemoteState,
} from "@/shared/lib/remote";

/** Общий механизм состояния запроса; семантика операции остаётся внутри feature. */
export function createFeatureRuntime() {
  const mutationProblem = ref<AppProblem | null>(null);
  const mutating = ref(false);
  let generation = 0;

  async function loadInto<T>(
    state: RemoteState<T>,
    loader: () => Promise<T>,
    empty: (value: T) => boolean,
  ): Promise<void> {
    const request = beginRequest(state);
    try {
      const data = await loader();
      finishRequest(state, request, data, empty(data));
    } catch (error) {
      failRequest(state, request, asProblem(error));
    }
  }

  async function mutate(
    operation: () => Promise<unknown>,
    reload: () => Promise<void>,
    conflictState?: RemoteState<unknown>,
  ): Promise<boolean> {
    const request = ++generation;
    mutationProblem.value = null;
    mutating.value = true;
    try {
      await operation();
      if (request !== generation) return false;
      await reload();
      return request === generation;
    } catch (error) {
      if (request !== generation) return false;
      mutationProblem.value = asProblem(error);
      if (mutationProblem.value.kind === "conflict" && conflictState)
        conflictState.phase = "conflict";
      return false;
    } finally {
      if (request === generation) mutating.value = false;
    }
  }

  function invalidate(): void {
    generation += 1;
    mutationProblem.value = null;
    mutating.value = false;
  }

  return { mutationProblem, mutating, loadInto, mutate, invalidate };
}
