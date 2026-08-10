import { defineStore } from "pinia";
import { reactive } from "vue";

import {
  commandRun,
  fetchRunArtifacts,
  fetchRunDetail,
  fetchRunLineage,
  fetchRunTimeline,
} from "@/shared/api/adapters/owner-control";
import { createFeatureRuntime } from "@/shared/lib/feature-store";
import { remoteState, resetRemoteState } from "@/shared/lib/remote";
import {
  type RunAction,
  type RunArtifactModel,
  type RunDetailModel,
  type RunLineageModel,
  type RunTimelineModel,
  toRunArtifactModel,
  toRunDetailModel,
  toRunLineageModel,
  toRunTimelineModel,
} from "./model";

export const useRunDetailsStore = defineStore("run-details", () => {
  const runDetail = reactive(remoteState<RunDetailModel | null>(null));
  const runTimeline = reactive(remoteState<RunTimelineModel[]>([]));
  const runLineage = reactive(remoteState<RunLineageModel | null>(null));
  const runArtifacts = reactive(remoteState<RunArtifactModel[]>([]));
  const runtime = createFeatureRuntime();
  const loadRun = (ref: string) =>
    Promise.all([
      runtime.loadInto(
        runDetail,
        async () => toRunDetailModel(await fetchRunDetail(ref)),
        (value) => value === null,
      ),
      runtime.loadInto(
        runTimeline,
        async () =>
          (await fetchRunTimeline(ref)).entries.map(toRunTimelineModel),
        (items) => items.length === 0,
      ),
      runtime.loadInto(
        runLineage,
        async () => toRunLineageModel(await fetchRunLineage(ref)),
        (value) => value === null,
      ),
      runtime.loadInto(
        runArtifacts,
        async () =>
          (await fetchRunArtifacts(ref)).artifacts.map(toRunArtifactModel),
        (items) => items.length === 0,
      ),
    ]).then(() => undefined);
  const executeRunAction = (action: RunAction, reasonCode: string) => {
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

export type { RunAction } from "./model";
