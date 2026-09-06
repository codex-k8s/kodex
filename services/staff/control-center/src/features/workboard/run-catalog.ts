import { ref } from "vue";
import { defineStore } from "pinia";
import { listRuns } from "@/shared/api/generated/openapi/sdk.gen";
import type { Run } from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { asProblem, unwrap, type AppProblem } from "@/shared/api/problem";
import type { RunFilter } from "./model";

export function runFilterStates(filter: RunFilter): Run["state"][] | undefined {
  if (filter === "ACTIVE")
    return ["QUEUED", "RUNNING", "WAITING_HUMAN", "CANCELLING"];
  if (filter === "TERMINAL") return ["SUCCEEDED", "FAILED", "CANCELLED"];
  return undefined;
}

export interface RunCatalogScope {
  projectRef?: string;
  query: string;
  filter: RunFilter;
}

export const useRunCatalogStore = defineStore("run-catalog", () => {
  const items = ref<Run[]>([]);
  const ready = ref(false);
  const pageToken = ref<string>();
  const loading = ref(false);
  const problem = ref<AppProblem>();
  const cursors = new Set<string>();
  let controller: AbortController | undefined;
  let generation = 0;
  let scopeKey = "";
  let invalidated = false;
  let refreshTimer: ReturnType<typeof setTimeout> | undefined;

  function keyFor(scope: RunCatalogScope): string {
    return JSON.stringify([scope.projectRef, scope.query.trim(), scope.filter]);
  }

  function invalidate(scope: RunCatalogScope): void {
    if (keyFor(scope) !== scopeKey) return;
    invalidated = true;
    if (loading.value || refreshTimer) return;
    refreshTimer = setTimeout(() => {
      refreshTimer = undefined;
      if (keyFor(scope) !== scopeKey) return;
      if (loading.value) return;
      invalidated = false;
      void load(scope);
    }, 250);
  }

  function reset(): void {
    clearTimeout(refreshTimer);
    refreshTimer = undefined;
    invalidated = false;
    controller?.abort();
    generation++;
    items.value = [];
    ready.value = false;
    pageToken.value = undefined;
    loading.value = false;
    problem.value = undefined;
    cursors.clear();
    scopeKey = "";
  }

  async function load(scope: RunCatalogScope, more = false): Promise<void> {
    const key = keyFor(scope);
    if (more && (loading.value || !pageToken.value || key !== scopeKey)) return;
    controller?.abort();
    const active = new AbortController();
    controller = active;
    const current = ++generation;
    const cursor = more ? pageToken.value : undefined;
    const states = runFilterStates(scope.filter);
    loading.value = true;
    problem.value = undefined;
    if (!more) {
      clearTimeout(refreshTimer);
      refreshTimer = undefined;
      invalidated = false;
      if (key !== scopeKey) {
        ready.value = false;
        items.value = [];
      }
      pageToken.value = undefined;
      cursors.clear();
      scopeKey = key;
    }
    try {
      const page = (
        await unwrap(
          listRuns({
            query: {
              ...(scope.projectRef ? { projectRef: scope.projectRef } : {}),
              query: scope.query.trim(),
              ...(states ? { states } : {}),
              pageSize: 40,
              pageToken: cursor,
            },
            signal: requestSignal(active.signal),
          }),
        )
      ).data;
      if (current !== generation || active.signal.aborted) return;
      if (
        !Array.isArray(page.items) ||
        page.items.some(
          (run) =>
            (scope.projectRef && run.projectRef !== scope.projectRef) ||
            (states && !states.includes(run.state)),
        ) ||
        (page.nextPageToken &&
          (page.nextPageToken === cursor || cursors.has(page.nextPageToken)))
      )
        throw new Error("Invalid run catalog page");
      const next = more ? [...items.value, ...page.items] : page.items;
      if (new Set(next.map((run) => run.ref)).size !== next.length)
        throw new Error("Repeated run catalog item");
      items.value = next;
      pageToken.value = page.nextPageToken;
      if (page.nextPageToken) cursors.add(page.nextPageToken);
      ready.value = true;
    } catch (error) {
      if (current === generation && !active.signal.aborted)
        problem.value = asProblem(error);
    } finally {
      if (current === generation) {
        loading.value = false;
        if (invalidated) invalidate(scope);
      }
    }
  }
  return { items, ready, pageToken, loading, problem, load, reset, invalidate };
});
