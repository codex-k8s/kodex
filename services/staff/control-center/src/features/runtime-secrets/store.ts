import { defineStore } from "pinia";
import { computed, ref } from "vue";

import { requestSignal } from "@/shared/api/client";
import { AppProblem } from "@/shared/api/problem";

import {
  createRuntimeSecret,
  loadRuntimeSecretPage,
  normalizeRuntimeSecretProblem,
  revokeRuntimeSecret,
  rotateRuntimeSecret,
  readRuntimeSecret,
} from "./api";
import type {
  RuntimeSecret,
  RuntimeSecretCreateInput,
  RuntimeSecretRotateInput,
} from "./model";
import { normalizeSecretPage } from "./model";

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
  let mutationGeneration = 0;
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
    if (projectRef.value !== nextProjectRef || query.value !== nextQuery)
      items.value = [];
    projectRef.value = nextProjectRef;
    query.value = nextQuery;
    nextPageToken.value = "";
    loading.value = true;
    loadingMore.value = false;
    mutationProblem.value = undefined;
    problem.value = undefined;
    try {
      const page = normalizeSecretPage(
        await loadRuntimeSecretPage(
          nextProjectRef,
          nextQuery,
          undefined,
          AbortSignal.any([currentController.signal, requestSignal()]),
        ),
      );
      if (current !== generation) return;
      if (page.items.some((item) => item.projectRef !== nextProjectRef))
        throw new Error("Runtime secret catalog scope mismatch");
      items.value = page.items;
      nextPageToken.value = page.nextPageToken;
    } catch (error) {
      if (current !== generation || currentController.signal.aborted) return;
      problem.value = normalizeRuntimeSecretProblem(error);
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
      const page = normalizeSecretPage(
        await loadRuntimeSecretPage(
          projectRef.value,
          query.value,
          cursor,
          AbortSignal.any([currentController.signal, requestSignal()]),
        ),
      );
      if (current !== generation) return;
      if (page.items.some((item) => item.projectRef !== projectRef.value))
        throw new Error("Runtime secret catalog scope mismatch");
      if (page.nextPageToken && page.nextPageToken === cursor)
        throw new Error("Runtime secret catalog cursor did not advance");
      const merged = new Map(items.value.map((item) => [item.ref, item]));
      for (const item of page.items) {
        const existing = merged.get(item.ref);
        if (!existing || item.version >= existing.version)
          merged.set(item.ref, item);
      }
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
    if (busyRef.value)
      throw new Error("Runtime secret mutation is already in progress");
    const project = projectRef.value;
    const current = generation;
    busyRef.value = "create";
    const mutation = ++mutationGeneration;
    mutationProblem.value = undefined;
    try {
      const receipt = checkedReceipt(
        await createRuntimeSecret(project, input),
        project,
      );
      if (current === generation) await reconcile(receipt);
    } catch (error) {
      const failure = normalizeRuntimeSecretProblem(error);
      if (current === generation) mutationProblem.value = failure;
      throw failure;
    } finally {
      if (mutation === mutationGeneration) busyRef.value = "";
    }
  }

  async function rotate(
    secret: RuntimeSecret,
    input: RuntimeSecretRotateInput,
  ): Promise<void> {
    if (secret.projectRef !== projectRef.value)
      throw new Error("Runtime secret mutation scope mismatch");
    if (busyRef.value)
      throw new Error("Runtime secret mutation is already in progress");
    const current = generation;
    busyRef.value = secret.ref;
    const mutation = ++mutationGeneration;
    mutationProblem.value = undefined;
    try {
      const receipt = checkedReceipt(
        await rotateRuntimeSecret(secret, input),
        secret.projectRef,
        secret,
      );
      if (current === generation) await reconcile(receipt);
    } catch (error) {
      const failure = normalizeRuntimeSecretProblem(error);
      if (current === generation) mutationProblem.value = failure;
      throw failure;
    } finally {
      if (mutation === mutationGeneration) busyRef.value = "";
    }
  }

  async function revoke(secret: RuntimeSecret): Promise<void> {
    if (secret.projectRef !== projectRef.value)
      throw new Error("Runtime secret mutation scope mismatch");
    if (busyRef.value)
      throw new Error("Runtime secret mutation is already in progress");
    const current = generation;
    busyRef.value = secret.ref;
    const mutation = ++mutationGeneration;
    mutationProblem.value = undefined;
    try {
      const receipt = checkedReceipt(
        await revokeRuntimeSecret(secret),
        secret.projectRef,
        secret,
      );
      if (current === generation) await reconcile(receipt);
    } catch (error) {
      const failure = normalizeRuntimeSecretProblem(error);
      if (current === generation) mutationProblem.value = failure;
      throw failure;
    } finally {
      if (mutation === mutationGeneration) busyRef.value = "";
    }
  }

  function checkedReceipt(
    value: unknown,
    project: string,
    previous?: RuntimeSecret,
  ): RuntimeSecret {
    const result = normalizeSecretPage({ items: [value] }).items[0];
    if (
      !result ||
      result.projectRef !== project ||
      (previous &&
        (result.ref !== previous.ref ||
          result.version <= previous.version ||
          result.currentRevision < previous.currentRevision))
    )
      throw new Error("Invalid runtime secret mutation receipt");
    return result;
  }

  function retainReceipt(receipt: RuntimeSecret): void {
    if (receipt.projectRef !== projectRef.value) return;
    const previous = items.value.find((item) => item.ref === receipt.ref);
    if (previous && previous.version > receipt.version) return;
    if (query.value && !previous) return;
    items.value = [
      ...items.value.filter((item) => item.ref !== receipt.ref),
      receipt,
    ].sort((left, right) =>
      left.ref < right.ref ? -1 : left.ref > right.ref ? 1 : 0,
    );
  }

  async function acceptPublication(secret: RuntimeSecret): Promise<void> {
    if (secret.projectRef !== projectRef.value) return;
    await reconcile(checkedReceipt(secret, projectRef.value));
  }

  async function reconcile(receipt: RuntimeSecret): Promise<void> {
    const current = generation;
    retainReceipt(receipt);
    const signal = AbortSignal.any([
      controller?.signal ?? requestSignal(),
      AbortSignal.timeout(5000),
    ]);
    let latest = receipt;
    let readProblem: AppProblem | undefined;
    for (let attempt = 0; attempt < 3; attempt += 1) {
      if (attempt)
        await new Promise<void>((resolve) =>
          setTimeout(resolve, attempt * 200),
        );
      if (generation !== current) return;
      if (signal.aborted) break;
      try {
        const read = await readRuntimeSecret(
          receipt.ref,
          receipt.projectRef,
          signal,
        );
        if (
          read.version < receipt.version ||
          read.currentRevision < receipt.currentRevision
        )
          throw new AppProblem({
            status: 409,
            code: "SECRET_REVISION_NOT_VISIBLE",
            retryable: true,
            kind: "conflict",
          });
        latest = read;
        readProblem = undefined;
        break;
      } catch (error) {
        readProblem = normalizeRuntimeSecretProblem(error);
        if (!readProblem.retryable) break;
      }
    }
    if (generation !== current) return;
    await reload();
    if (projectRef.value !== receipt.projectRef || generation !== current + 1)
      return;
    retainReceipt(latest);
    if (readProblem) problem.value = readProblem;
  }

  function clearMutationProblem(): void {
    mutationProblem.value = undefined;
  }

  function dispose(): void {
    generation += 1;
    mutationGeneration += 1;
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
    acceptPublication,
    create,
    rotate,
    revoke,
    clearMutationProblem,
    dispose,
  };
});
