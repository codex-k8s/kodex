import { ref } from "vue";
import { listOwnerGates } from "@/shared/api/generated/openapi/sdk.gen";
import type { OwnerGate } from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { asProblem, unwrap, type AppProblem } from "@/shared/api/problem";

export interface GateCatalogScope {
  projectRef?: string;
  query: string;
  view: "PENDING" | "HISTORY";
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
  function keyFor(scope: GateCatalogScope): string {
    return JSON.stringify([scope.projectRef, scope.query, scope.view]);
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
  }

  async function load(scope: GateCatalogScope, more = false): Promise<void> {
    const key = keyFor(scope);
    if (more && (loading.value || !pageToken.value || key !== scopeKey)) return;
    controller?.abort();
    const active = new AbortController();
    controller = active;
    const current = ++generation;
    const cursor = more ? pageToken.value : undefined;
    const states: OwnerGate["state"][] =
      scope.view === "PENDING"
        ? ["OPEN"]
        : ["APPROVED", "REJECTED", "CHANGES_REQUESTED", "CANCELLED", "EXPIRED"];
    if (!more) {
      clearTimeout(refreshTimer);
      refreshTimer = undefined;
      invalidated = false;
      items.value = [];
      total.value = undefined;
      pageToken.value = undefined;
      cursors.clear();
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
              states,
              pageSize: 30,
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
            !states.includes(gate.state) ||
            (scope.projectRef && gate.projectRef !== scope.projectRef),
        ) ||
        (page.nextPageToken &&
          (page.nextPageToken === cursor || cursors.has(page.nextPageToken)))
      )
        throw new Error("Invalid owner gate catalog page");
      const next = more ? [...items.value, ...page.items] : page.items;
      if (
        new Set(next.map((gate) => gate.ref)).size !== next.length ||
        next.length > page.total
      )
        throw new Error("Invalid owner gate catalog sequence");
      items.value = next;
      total.value = page.total;
      pageToken.value = page.nextPageToken;
      if (page.nextPageToken) cursors.add(page.nextPageToken);
    } catch (error) {
      if (current === generation && !active.signal.aborted) {
        problem.value = asProblem(error);
        if ([401, 403, 404].includes(problem.value.status)) {
          items.value = [];
          total.value = undefined;
          pageToken.value = undefined;
          cursors.clear();
        }
      }
    } finally {
      if (current === generation) {
        loading.value = false;
        if (invalidated) invalidate(scope);
      }
    }
  }
  return { items, total, pageToken, loading, problem, load, reset, invalidate };
}
