import { ref } from "vue";
import { listOwnerGates } from "@/shared/api/generated/openapi/sdk.gen";
import type { OwnerGate } from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { asProblem, unwrap, type AppProblem } from "@/shared/api/problem";
import { assertGateScope, hasValidGateScope } from "./gate-scope";

export interface GateCatalogScope {
  organizationRef?: string;
  projectRef?: string;
  query: string;
  view: "PENDING" | "HISTORY";
  pageSize?: number;
}

export function useGateCatalog() {
  const items = ref<OwnerGate[]>([]);
  const total = ref<number>();
  const pageToken = ref<string>();
  const loading = ref(false);
  const problem = ref<AppProblem>();
  let controller: AbortController | undefined;
  let generation = 0;
  let scopeKey = "";
  let invalidated = false;
  let refreshTimer: ReturnType<typeof setTimeout> | undefined;
  const cursors = new Set<string>();
  const sourceRefs = new Set<string>();
  const statesFor = (view: GateCatalogScope["view"]): OwnerGate["state"][] =>
    view === "PENDING"
      ? ["OPEN"]
      : ["APPROVED", "REJECTED", "CHANGES_REQUESTED", "CANCELLED", "EXPIRED"];
  const locallyFiltered = (scope: GateCatalogScope): boolean =>
    !scope.projectRef && !scope.query.trim();
  const matchesView = (gate: OwnerGate, scope: GateCatalogScope): boolean =>
    statesFor(scope.view).includes(gate.state);
  function keyFor(scope: GateCatalogScope): string {
    return JSON.stringify([
      scope.organizationRef,
      scope.projectRef,
      scope.query,
      scope.view,
    ]);
  }
  function invalidate(scope: GateCatalogScope): void {
    if (keyFor(scope) !== scopeKey) return;
    invalidated = true;
    if (loading.value || refreshTimer) return;
    refreshTimer = setTimeout(() => {
      refreshTimer = undefined;
      if (keyFor(scope) === scopeKey && !loading.value) void load(scope);
    }, 250);
  }

  function reset(): void {
    clearTimeout(refreshTimer);
    refreshTimer = undefined;
    invalidated = false;
    controller?.abort();
    generation++;
    items.value = [];
    total.value = undefined;
    pageToken.value = undefined;
    loading.value = false;
    problem.value = undefined;
    scopeKey = "";
    cursors.clear();
    sourceRefs.clear();
  }

  function applySnapshot(
    scope: GateCatalogScope,
    values: OwnerGate[],
    nextPageToken?: string,
  ): void {
    for (const gate of values) assertGateScope(gate, scope.organizationRef);
    controller?.abort();
    generation++;
    clearTimeout(refreshTimer);
    refreshTimer = undefined;
    invalidated = false;
    cursors.clear();
    sourceRefs.clear();
    for (const gate of values) sourceRefs.add(gate.ref);
    if (nextPageToken) cursors.add(nextPageToken);
    scopeKey = keyFor(scope);
    items.value = values.filter((gate) => matchesView(gate, scope));
    pageToken.value = nextPageToken;
    total.value = nextPageToken ? undefined : items.value.length;
    loading.value = false;
    problem.value = undefined;
  }

  async function load(scope: GateCatalogScope, more = false): Promise<void> {
    const key = keyFor(scope);
    if (more && (loading.value || !pageToken.value || key !== scopeKey)) return;
    controller?.abort();
    const active = new AbortController();
    controller = active;
    const current = ++generation;
    const cursor = more ? pageToken.value : undefined;
    const states = statesFor(scope.view);
    const filterLocally = locallyFiltered(scope);
    if (!more) {
      clearTimeout(refreshTimer);
      refreshTimer = undefined;
      invalidated = false;
      items.value = [];
      total.value = undefined;
      pageToken.value = undefined;
      cursors.clear();
      sourceRefs.clear();
      scopeKey = key;
    }
    loading.value = true;
    problem.value = undefined;
    try {
      const page = (
        await unwrap(
          listOwnerGates({
            query: {
              projectRef: scope.projectRef,
              query: scope.query,
              states: filterLocally ? undefined : states,
              pageSize: scope.pageSize ?? 20,
              pageToken: cursor,
            },
            signal: requestSignal(active.signal),
            cache: "no-store",
          }),
        )
      ).data;
      if (current !== generation || active.signal.aborted) return;
      if (
        !Array.isArray(page.items) ||
        !Number.isSafeInteger(page.total) ||
        page.total < page.items.length ||
        page.items.some(
          (gate) =>
            !hasValidGateScope(gate, scope.organizationRef) ||
            (!filterLocally && !states.includes(gate.state)) ||
            (scope.projectRef && gate.projectRef !== scope.projectRef),
        ) ||
        (page.nextPageToken &&
          (page.nextPageToken === cursor || cursors.has(page.nextPageToken)))
      )
        throw new Error("Invalid owner gate catalog page");
      if (page.items.some((gate) => sourceRefs.has(gate.ref)))
        throw new Error("Invalid owner gate catalog sequence");
      const visiblePage = filterLocally
        ? page.items.filter((gate) => matchesView(gate, scope))
        : page.items;
      const next = more ? [...items.value, ...visiblePage] : visiblePage;
      if (
        new Set(next.map((gate) => gate.ref)).size !== next.length ||
        next.length > page.total
      )
        throw new Error("Invalid owner gate catalog sequence");
      items.value = next;
      total.value = filterLocally
        ? page.nextPageToken
          ? undefined
          : next.length
        : page.total;
      pageToken.value = page.nextPageToken;
      for (const gate of page.items) sourceRefs.add(gate.ref);
      if (page.nextPageToken) cursors.add(page.nextPageToken);
    } catch (error) {
      if (current === generation && !active.signal.aborted) {
        problem.value = asProblem(error);
        if ([401, 403, 404].includes(problem.value.status)) {
          items.value = [];
          total.value = undefined;
          pageToken.value = undefined;
          cursors.clear();
          sourceRefs.clear();
        }
      }
    } finally {
      if (current === generation) {
        loading.value = false;
        if (invalidated) invalidate(scope);
      }
    }
  }
  return {
    items,
    total,
    pageToken,
    loading,
    problem,
    load,
    reset,
    invalidate,
    applySnapshot,
  };
}
