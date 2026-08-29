import { defineStore } from "pinia";
import { computed, ref } from "vue";

import { requestSignal } from "@/shared/api/client";
import type { AppProblem } from "@/shared/api/problem";

import {
  createRuntimeSecret,
  loadRuntimeSecretPage,
  normalizeRuntimeSecretProblem,
  revokeRuntimeSecret,
  rotateRuntimeSecret,
} from "./api";
import type {
  RuntimeSecret,
  RuntimeSecretCreateInput,
  RuntimeSecretRotateInput,
} from "./model";

export const useRuntimeSecretsStore = defineStore("runtime-secrets", () => {
  const items = ref<RuntimeSecret[]>([]);
  const projectRef = ref("");
  const query = ref("");
  const nextPageToken = ref("");
  const loading = ref(false);
  const loadingMore = ref(false);
  const problem = ref<AppProblem>();
  const mutationProblem = ref<AppProblem>();
  const busyRef = ref("");
  let generation = 0;
  let controller: AbortController | undefined;

  const empty = computed(
    () => !loading.value && !problem.value && items.value.length === 0,
  );
  const hasMore = computed(() => nextPageToken.value.length > 0);

  async function load(nextProjectRef: string, nextQuery = ""): Promise<void> {
    const current = ++generation;
    controller?.abort();
    const currentController = new AbortController();
    controller = currentController;
    projectRef.value = nextProjectRef;
    query.value = nextQuery;
    nextPageToken.value = "";
    loading.value = true;
    problem.value = undefined;
    try {
      const page = await loadRuntimeSecretPage(
        nextProjectRef,
        nextQuery,
        undefined,
        AbortSignal.any([currentController.signal, requestSignal()]),
      );
      if (current !== generation) return;
      items.value = page.items;
      nextPageToken.value = page.nextPageToken;
    } catch (error) {
      if (current !== generation || currentController.signal.aborted) return;
      problem.value = normalizeRuntimeSecretProblem(error);
      items.value = [];
    } finally {
      if (current === generation) loading.value = false;
    }
  }

  async function loadMore(): Promise<void> {
    if (!hasMore.value || loading.value || loadingMore.value) return;
    const current = generation;
    const cursor = nextPageToken.value;
    const currentController = controller ?? new AbortController();
    controller = currentController;
    loadingMore.value = true;
    problem.value = undefined;
    try {
      const page = await loadRuntimeSecretPage(
        projectRef.value,
        query.value,
        cursor,
        AbortSignal.any([currentController.signal, requestSignal()]),
      );
      if (current !== generation) return;
      const merged = new Map(items.value.map((item) => [item.ref, item]));
      for (const item of page.items) merged.set(item.ref, item);
      items.value = [...merged.values()];
      nextPageToken.value = page.nextPageToken;
    } catch (error) {
      if (current === generation && !currentController.signal.aborted)
        problem.value = normalizeRuntimeSecretProblem(error);
    } finally {
      if (current === generation) loadingMore.value = false;
    }
  }

  async function reload(): Promise<void> {
    await load(projectRef.value, query.value);
  }

  async function create(input: RuntimeSecretCreateInput): Promise<void> {
    busyRef.value = "create";
    mutationProblem.value = undefined;
    try {
      await createRuntimeSecret(projectRef.value, input);
      await reload();
    } catch (error) {
      mutationProblem.value = normalizeRuntimeSecretProblem(error);
      throw mutationProblem.value;
    } finally {
      busyRef.value = "";
    }
  }

  async function rotate(
    secret: RuntimeSecret,
    input: RuntimeSecretRotateInput,
  ): Promise<void> {
    busyRef.value = secret.ref;
    mutationProblem.value = undefined;
    try {
      await rotateRuntimeSecret(secret, input);
      await reload();
    } catch (error) {
      mutationProblem.value = normalizeRuntimeSecretProblem(error);
      throw mutationProblem.value;
    } finally {
      busyRef.value = "";
    }
  }

  async function revoke(secret: RuntimeSecret): Promise<void> {
    busyRef.value = secret.ref;
    mutationProblem.value = undefined;
    try {
      await revokeRuntimeSecret(secret);
      await reload();
    } catch (error) {
      mutationProblem.value = normalizeRuntimeSecretProblem(error);
      throw mutationProblem.value;
    } finally {
      busyRef.value = "";
    }
  }

  function clearMutationProblem(): void {
    mutationProblem.value = undefined;
  }

  function dispose(): void {
    generation += 1;
    controller?.abort();
    controller = undefined;
    items.value = [];
    nextPageToken.value = "";
    problem.value = undefined;
    mutationProblem.value = undefined;
    busyRef.value = "";
  }

  return {
    items,
    projectRef,
    query,
    nextPageToken,
    loading,
    loadingMore,
    problem,
    mutationProblem,
    busyRef,
    empty,
    hasMore,
    load,
    loadMore,
    reload,
    create,
    rotate,
    revoke,
    clearMutationProblem,
    dispose,
  };
});
