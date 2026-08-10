import { defineStore } from "pinia";
import { reactive } from "vue";

import { fetchIncidents } from "@/shared/api/adapters/operations";
import {
  toIncidentListItem,
  type IncidentListItem,
} from "@/features/incident-list/model";
import { createFeatureRuntime } from "@/shared/lib/feature-store";
import { invalidate, remoteState, resetRemoteState } from "@/shared/lib/remote";
import { subscribeRealtimeSnapshot } from "@/shared/realtime/snapshot-bus";

export const useIncidentListStore = defineStore("incident-list", () => {
  const incidents = reactive(remoteState<IncidentListItem[]>([]));
  const runtime = createFeatureRuntime();
  const load = () =>
    runtime.loadInto(
      incidents,
      async () => (await fetchIncidents()).incidents.map(toIncidentListItem),
      (items) => items.length === 0,
    );
  subscribeRealtimeSnapshot("INCIDENTS", (snapshot) => {
    invalidate(incidents);
    incidents.data = (snapshot.items.incidents ?? []).map(toIncidentListItem);
    incidents.phase = incidents.data.length ? "ready" : "empty";
  });
  function reset(): void {
    runtime.invalidate();
    resetRemoteState(incidents, []);
  }
  return { incidents, load, reset };
});
