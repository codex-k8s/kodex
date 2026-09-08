import { computed, reactive } from "vue";
import { defineStore } from "pinia";
import type { Run } from "@/shared/api/generated/openapi/types.gen";
import type { RunLane } from "./model";
import { createRunCatalog, type RunCatalogScope } from "./run-catalog";

export const runLaneStates: Record<RunLane, Run["state"][]> = {
  QUEUED: ["QUEUED"],
  RUNNING: ["RUNNING", "CANCELLING"],
  WAITING_HUMAN: ["WAITING_HUMAN"],
  TERMINAL: ["SUCCEEDED", "FAILED", "CANCELLED"],
};

export const useRunBoardStore = defineStore("run-board", () => {
  const columns = reactive({
    QUEUED: createRunCatalog(),
    RUNNING: createRunCatalog(),
    WAITING_HUMAN: createRunCatalog(),
    TERMINAL: createRunCatalog(),
  });
  const lanes = Object.keys(runLaneStates) as RunLane[];
  const items = computed(() => {
    const latest = new Map<string, Run>();
    for (const lane of lanes) {
      for (const run of columns[lane].items) {
        const previous = latest.get(run.ref);
        if (!previous || run.version > previous.version)
          latest.set(run.ref, run);
      }
    }
    return [...latest.values()];
  });
  const ready = computed(() => lanes.every((lane) => columns[lane].ready));
  const loading = computed(() => lanes.some((lane) => columns[lane].loading));
  const problem = computed(() =>
    lanes.map((lane) => columns[lane].problem).find(Boolean),
  );
  const pageToken = computed(() =>
    lanes.some((lane) => columns[lane].pageToken) ? "more" : undefined,
  );
  function enabled(scope: RunCatalogScope, lane: RunLane): boolean {
    return (
      scope.filter === "ALL" ||
      (scope.filter === "TERMINAL" ? lane === "TERMINAL" : lane !== "TERMINAL")
    );
  }
  async function load(
    scope: RunCatalogScope,
    more = false,
    target?: RunLane,
  ): Promise<void> {
    await Promise.all(
      lanes.map(async (lane) => {
        if (target && target !== lane) return;
        if (!enabled(scope, lane)) {
          columns[lane].reset();
          columns[lane].ready = true;
          return;
        }
        await columns[lane].load(
          { ...scope, states: runLaneStates[lane] },
          more,
        );
      }),
    );
  }
  function reset(): void {
    for (const lane of lanes) columns[lane].reset();
  }
  function invalidate(scope: RunCatalogScope): void {
    for (const lane of lanes) {
      if (enabled(scope, lane))
        columns[lane].invalidate({ ...scope, states: runLaneStates[lane] });
    }
  }
  return {
    columns,
    items,
    ready,
    loading,
    problem,
    pageToken,
    load,
    reset,
    invalidate,
  };
});
