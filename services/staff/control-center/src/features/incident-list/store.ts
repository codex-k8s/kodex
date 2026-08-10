import { defineStore } from "pinia";
import { reactive } from "vue";

import { fetchIncidents } from "@/shared/api/adapters/operations";
import type { IncidentView } from "@/shared/api/generated/openapi/types.gen";
import { createFeatureRuntime } from "@/shared/lib/feature-store";
import { invalidate, remoteState, resetRemoteState } from "@/shared/lib/remote";
import { subscribeRealtimeSnapshot } from "@/shared/realtime/snapshot-bus";

export const useIncidentListStore = defineStore("incident-list", () => {
  const incidents = reactive(remoteState<IncidentView[]>([]));
  const runtime = createFeatureRuntime();
  const load = () =>
    runtime.loadInto(
      incidents,
      async () => (await fetchIncidents()).incidents,
      (items) => items.length === 0,
    );
  subscribeRealtimeSnapshot("INCIDENTS", (snapshot) => {
    invalidate(incidents);
    incidents.data = snapshot.items.incidents ?? [];
    incidents.phase = incidents.data.length ? "ready" : "empty";
  });
  function reset(): void {
    runtime.invalidate();
    resetRemoteState(incidents, []);
  }
  return { incidents, load, reset };
});
