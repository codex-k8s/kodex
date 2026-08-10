import { defineStore } from "pinia";
import { reactive } from "vue";

import { fetchHealthSeries } from "@/shared/api/adapters/owner-control";
import { fetchDiagnostics } from "@/shared/api/adapters/operations";
import {
  toDiagnosticsModel,
  toHealthObservationModel,
  type DiagnosticsModel,
  type HealthObservationModel,
} from "@/features/diagnostics/model";
import { createFeatureRuntime } from "@/shared/lib/feature-store";
import { invalidate, remoteState, resetRemoteState } from "@/shared/lib/remote";
import { subscribeRealtimeSnapshot } from "@/shared/realtime/snapshot-bus";

export const useDiagnosticsStore = defineStore("health-diagnostics", () => {
  const health = reactive(remoteState<HealthObservationModel[]>([]));
  const diagnostics = reactive(remoteState<DiagnosticsModel | null>(null));
  const runtime = createFeatureRuntime();
  const loadHealth = () =>
    runtime.loadInto(
      health,
      async () =>
        (await fetchHealthSeries()).observations.map(toHealthObservationModel),
      (items) => items.length === 0,
    );
  const loadDiagnostics = () =>
    runtime.loadInto(
      diagnostics,
      async () => toDiagnosticsModel(await fetchDiagnostics()),
      (value) => value === null,
    );
  function replaceHealth(
    items: Parameters<typeof toHealthObservationModel>[0][],
  ): void {
    invalidate(health);
    health.data = items.map(toHealthObservationModel);
    health.phase = items.length ? "ready" : "empty";
  }
  subscribeRealtimeSnapshot("HEALTH", (snapshot) =>
    replaceHealth(snapshot.items.health ?? []),
  );
  function reset(): void {
    runtime.invalidate();
    resetRemoteState(health, []);
    resetRemoteState(diagnostics, null);
  }
  return {
    health,
    diagnostics,
    loadHealth,
    loadDiagnostics,
    replaceHealth,
    reset,
  };
});
