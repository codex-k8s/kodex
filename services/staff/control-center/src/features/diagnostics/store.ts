import { defineStore } from "pinia";
import { reactive } from "vue";

import { fetchHealthSeries } from "@/shared/api/adapters/owner-control";
import type { HealthObservation } from "@/shared/api/generated/openapi/types.gen";
import { createFeatureRuntime } from "@/shared/lib/feature-store";
import { invalidate, remoteState, resetRemoteState } from "@/shared/lib/remote";

export const useDiagnosticsStore = defineStore("health-diagnostics", () => {
  const health = reactive(remoteState<HealthObservation[]>([]));
  const runtime = createFeatureRuntime();
  const loadHealth = () =>
    runtime.loadInto(
      health,
      async () => (await fetchHealthSeries()).observations,
      (items) => items.length === 0,
    );
  function replaceHealth(items: HealthObservation[]): void {
    invalidate(health);
    health.data = items;
    health.phase = items.length ? "ready" : "empty";
  }
  function reset(): void {
    runtime.invalidate();
    resetRemoteState(health, []);
  }
  return { health, loadHealth, replaceHealth, reset };
});
