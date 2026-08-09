import { defineStore } from "pinia";
import { reactive } from "vue";

import {
  commandRun,
  fetchRunArtifacts,
  fetchRunDetail,
  fetchRunLineage,
  fetchRunTimeline,
} from "@/shared/api/adapters/owner-control";
import type {
  RunArtifactView,
  RunDetail,
  RunLineage,
  RunNextAction,
  RunTimelineEntry,
} from "@/shared/api/generated/openapi/types.gen";
import { createFeatureRuntime } from "@/shared/lib/feature-store";
import { remoteState, resetRemoteState } from "@/shared/lib/remote";

export const useRunDetailsStore = defineStore("run-details", () => {
  const runDetail = reactive(remoteState<RunDetail | null>(null));
  const runTimeline = reactive(remoteState<RunTimelineEntry[]>([]));
  const runLineage = reactive(remoteState<RunLineage | null>(null));
  const runArtifacts = reactive(remoteState<RunArtifactView[]>([]));
  const runtime = createFeatureRuntime();
  const loadRun = (ref: string) =>
    Promise.all([
      runtime.loadInto(
        runDetail,
        () => fetchRunDetail(ref),
        (value) => value === null,
      ),
      runtime.loadInto(
        runTimeline,
        async () => (await fetchRunTimeline(ref)).entries,
        (items) => items.length === 0,
      ),
      runtime.loadInto(
        runLineage,
        () => fetchRunLineage(ref),
        (value) => value === null,
      ),
      runtime.loadInto(
        runArtifacts,
        async () => (await fetchRunArtifacts(ref)).artifacts,
        (items) => items.length === 0,
      ),
    ]).then(() => undefined);
  const executeRunAction = (action: RunNextAction, reasonCode: string) => {
    const value = runDetail.data?.run;
    return value && value.nextActions.includes(action)
      ? runtime.mutate(
          () => commandRun(value.runRef, value.version, { action, reasonCode }),
          () => loadRun(value.runRef),
          runDetail,
        )
      : Promise.resolve(false);
  };
  function reset(): void {
    runtime.invalidate();
    resetRemoteState(runDetail, null);
    resetRemoteState(runTimeline, []);
    resetRemoteState(runLineage, null);
    resetRemoteState(runArtifacts, []);
  }
  return {
    runDetail,
    runTimeline,
    runLineage,
    runArtifacts,
    mutationProblem: runtime.mutationProblem,
    mutating: runtime.mutating,
    loadRun,
    executeRunAction,
    reset,
  };
});

export type RunAction = RunNextAction;
