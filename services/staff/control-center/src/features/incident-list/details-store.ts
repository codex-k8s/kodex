import { defineStore } from "pinia";
import { reactive } from "vue";

import {
  commandIncident,
  fetchIncident,
  fetchIncidentTimeline,
} from "@/shared/api/adapters/owner-control";
import { createFeatureRuntime } from "@/shared/lib/feature-store";
import { remoteState, resetRemoteState } from "@/shared/lib/remote";
import {
  type IncidentAction,
  type IncidentDetailModel,
  type IncidentHistoryModel,
  toIncidentDetailModel,
  toIncidentHistoryModel,
} from "./model";

export const useIncidentDetailsStore = defineStore("incident-details", () => {
  const incident = reactive(remoteState<IncidentDetailModel | null>(null));
  const incidentHistory = reactive(remoteState<IncidentHistoryModel[]>([]));
  const runtime = createFeatureRuntime();
  const loadIncident = (ref: string) =>
    Promise.all([
      runtime.loadInto(
        incident,
        async () => toIncidentDetailModel(await fetchIncident(ref)),
        (value) => value === null,
      ),
      runtime.loadInto(
        incidentHistory,
        async () =>
          (await fetchIncidentTimeline(ref)).entries.map(
            toIncidentHistoryModel,
          ),
        (items) => items.length === 0,
      ),
    ]).then(() => undefined);
  const executeIncidentAction = (
    action: IncidentAction,
    reasonCode: string,
  ) => {
    const value = incident.data;
    return value && value.nextActions.includes(action)
      ? runtime.mutate(
          () =>
            commandIncident(value.incidentRef, value.version, {
              action,
              reasonCode,
            }),
          () => loadIncident(value.incidentRef),
          incident,
        )
      : Promise.resolve(false);
  };
  function reset(): void {
    runtime.invalidate();
    resetRemoteState(incident, null);
    resetRemoteState(incidentHistory, []);
  }
  return {
    incident,
    incidentHistory,
    mutationProblem: runtime.mutationProblem,
    mutating: runtime.mutating,
    loadIncident,
    executeIncidentAction,
    reset,
  };
});

export type { IncidentAction } from "./model";
