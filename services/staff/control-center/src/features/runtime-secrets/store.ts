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
} from "./api";
import type {
  RuntimeSecret,
  RuntimeSecretCreateInput,
  RuntimeSecretRotateInput,
} from "./model";
import { normalizeSecretPage } from "./model";
import {
  assertRuntimeResourceAddressIdentity,
  runtimeResourceAddressKey,
  runtimeResourceAddressScope,
  type RuntimeResourceAddress,
} from "@/features/runtime/resource-scope";

export const useRuntimeSecretsStore = defineStore("runtime-secrets", () => {
  const items = ref<RuntimeSecret[]>([]);
  const projectRef = ref("");
  const resourceScope = ref<RuntimeResourceAddress>();
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
  let requestedPageSize = 20;

  const empty = computed(
    () => !loading.value && !problem.value && items.value.length === 0,
  );
  const hasMore = computed(() => nextPageToken.value.length > 0);

  function prepareRealtimeScope(scope: RuntimeResourceAddress): void {
    const nextProjectRef = runtimeResourceAddressKey(scope);
    resourceScope.value = scope;
    generation += 1;
    controller?.abort();
    if (projectRef.value !== nextProjectRef) items.value = [];
    projectRef.value = nextProjectRef;
    query.value = "";
    nextPageToken.value = "";
    problem.value = undefined;
    mutationProblem.value = undefined;
    loading.value = true;
    loadingMore.value = false;
  }

  function applySnapshot(
    scope: RuntimeResourceAddress,
    values: RuntimeSecret[],
    sourceNextPageToken?: string,
  ): void {
    const nextProjectRef = runtimeResourceAddressKey(scope);
    if (query.value || projectRef.value !== nextProjectRef) return;
    for (const item of values)
      assertRuntimeResourceAddressIdentity(scope, item);
    const previous = new Map(items.value.map((item) => [item.ref, item]));
    items.value = values.map((item) => {
      const retained = previous.get(item.ref);
      return retained && retained.version > item.version ? retained : item;
    });
    nextPageToken.value = sourceNextPageToken ?? "";
    loading.value = false;
    problem.value = undefined;
  }

  async function load(
    scope: RuntimeResourceAddress,
    nextQuery = "",
    pageSize = 20,
  ): Promise<void> {
    const nextProjectRef = runtimeResourceAddressKey(scope);
    resourceScope.value = scope;
    requestedPageSize = pageSize;
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
          scope,
          nextQuery,
          undefined,
          AbortSignal.any([currentController.signal, requestSignal()]),
          pageSize,
        ),
      );
      if (current !== generation) return;
      for (const item of page.items)
        assertRuntimeResourceAddressIdentity(scope, item);
      items.value = page.items;
      nextPageToken.value = page.nextPageToken;
    } catch (error) {
      if (current !== generation || currentController.signal.aborted) return;
      problem.value = normalizeRuntimeSecretProblem(error);
    } finally {
      if (current === generation) loading.value = false;
    }
  }

  async function loadMore(pageSize = 20): Promise<void> {
    const scope = resourceScope.value;
    if (!scope || !hasMore.value || loading.value || loadingMore.value) return;
    requestedPageSize = pageSize;
    const current = generation;
    const cursor = nextPageToken.value;
    const currentController = controller ?? new AbortController();
    controller = currentController;
    loadingMore.value = true;
    problem.value = undefined;
    try {
      const page = normalizeSecretPage(
        await loadRuntimeSecretPage(
          scope,
          query.value,
          cursor,
          AbortSignal.any([currentController.signal, requestSignal()]),
          pageSize,
        ),
      );
      if (current !== generation) return;
      for (const item of page.items)
        assertRuntimeResourceAddressIdentity(scope, item);
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
    if (resourceScope.value)
      await load(resourceScope.value, query.value, requestedPageSize);
  }

  async function create(input: RuntimeSecretCreateInput): Promise<void> {
    if (
      !resourceScope.value ||
      runtimeResourceAddressScope(resourceScope.value).kind !== "PROJECT"
    )
      throw new Error(
        "Organization secrets require the protected draft workflow",
      );
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
      if (current === generation) retainReceipt(receipt);
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
    const scope = resourceScope.value;
    if (!scope) throw new Error("Runtime secret mutation scope is unavailable");
    assertRuntimeResourceAddressIdentity(scope, secret);
    if (busyRef.value)
      throw new Error("Runtime secret mutation is already in progress");
    const current = generation;
    busyRef.value = secret.ref;
    const mutation = ++mutationGeneration;
    mutationProblem.value = undefined;
    try {
      const receipt = checkedReceipt(
        await rotateRuntimeSecret(secret, input),
        scope,
        secret,
      );
      if (current === generation) retainReceipt(receipt);
    } catch (error) {
      const failure = normalizeRuntimeSecretProblem(error);
      if (current === generation) mutationProblem.value = failure;
      throw failure;
    } finally {
      if (mutation === mutationGeneration) busyRef.value = "";
    }
  }

  async function revoke(secret: RuntimeSecret): Promise<void> {
    const scope = resourceScope.value;
    if (!scope) throw new Error("Runtime secret mutation scope is unavailable");
    assertRuntimeResourceAddressIdentity(scope, secret);
    if (busyRef.value)
      throw new Error("Runtime secret mutation is already in progress");
    const current = generation;
    busyRef.value = secret.ref;
    const mutation = ++mutationGeneration;
    mutationProblem.value = undefined;
    try {
      const receipt = checkedReceipt(
        await revokeRuntimeSecret(secret),
        scope,
        secret,
      );
      if (current === generation) retainReceipt(receipt);
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
    project: RuntimeResourceAddress,
    previous?: RuntimeSecret,
  ): RuntimeSecret {
    const result = normalizeSecretPage({ items: [value] }).items[0];
    if (
      !result ||
      (previous &&
        (result.ref !== previous.ref ||
          result.version <= previous.version ||
          result.currentRevision < previous.currentRevision))
    )
      throw new Error("Invalid runtime secret mutation receipt");
    assertRuntimeResourceAddressIdentity(project, result);
    return result;
  }

  function retainReceipt(receipt: RuntimeSecret): void {
    if (!resourceScope.value) return;
    assertRuntimeResourceAddressIdentity(resourceScope.value, receipt);
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

  function acceptPublication(secret: RuntimeSecret): void {
    if (!resourceScope.value) return;
    retainReceipt(checkedReceipt(secret, resourceScope.value));
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
    resourceScope.value = undefined;
    nextPageToken.value = "";
    problem.value = undefined;
    mutationProblem.value = undefined;
    busyRef.value = "";
    requestedPageSize = 20;
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
    prepareRealtimeScope,
    applySnapshot,
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
