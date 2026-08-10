import { defineStore } from "pinia";
import { reactive } from "vue";

import { fetchRuns, resolveGate } from "@/shared/api/adapters/operations";
import { fetchResources } from "@/shared/api/adapters/resources";
import {
  toOwnerGateListItem,
  toRunListItem,
  type OwnerGateListItem,
  type OwnerGateResolution,
  type RunListItem,
} from "@/features/runs/model";
import type { Resource } from "@/shared/api/generated/openapi/types.gen";
import { createFeatureRuntime } from "@/shared/lib/feature-store";
import { invalidate, remoteState, resetRemoteState } from "@/shared/lib/remote";
import { subscribeRealtimeSnapshot } from "@/shared/realtime/snapshot-bus";

export const useRunsStore = defineStore("runs", () => {
  const runs = reactive(remoteState<RunListItem[]>([]));
  const gates = reactive(remoteState<OwnerGateListItem[]>([]));
  const runRuntime = createFeatureRuntime();
  const gateRuntime = createFeatureRuntime();
  const authoritativeGates = new Map<string, Resource>();
  const loadRuns = () =>
    runRuntime.loadInto(
      runs,
      async () => (await fetchRuns()).runs.map(toRunListItem),
      (items) => items.length === 0,
    );
  const loadGates = () =>
    gateRuntime.loadInto(
      gates,
      async () => {
        const values = (await fetchResources("OWNER_GATE")).resources;
        authoritativeGates.clear();
        values.forEach((value) => authoritativeGates.set(value.id, value));
        return values.map(toOwnerGateListItem);
      },
      (items) => items.length === 0,
    );
  const resolveOwnerGate = (
    resource: OwnerGateListItem,
    decision: OwnerGateResolution,
    reason: string,
  ) => {
    const authoritative = authoritativeGates.get(resource.id);
    if (!authoritative)
      return Promise.reject(
        new Error("Authoritative owner gate is unavailable"),
      );
    return gateRuntime.mutate(
      () => resolveGate(authoritative, { decision, reason }),
      loadGates,
      gates,
    );
  };
  const replaceRuns = (items: Parameters<typeof toRunListItem>[0][]) => {
    invalidate(runs);
    runs.data = items.map(toRunListItem);
    runs.phase = items.length ? "ready" : "empty";
  };
  const replaceGates = (items: Resource[]) => {
    invalidate(gates);
    const values = items.filter((item) => item.kind === "OWNER_GATE");
    authoritativeGates.clear();
    values.forEach((value) => authoritativeGates.set(value.id, value));
    gates.data = values.map(toOwnerGateListItem);
    gates.phase = gates.data.length ? "ready" : "empty";
  };
  subscribeRealtimeSnapshot("RUNS", (snapshot) =>
    replaceRuns(snapshot.items.runs ?? []),
  );
  subscribeRealtimeSnapshot("RESOURCES", (snapshot) =>
    replaceGates(snapshot.items.resources ?? []),
  );
  function reset(): void {
    runRuntime.invalidate();
    gateRuntime.invalidate();
    authoritativeGates.clear();
    resetRemoteState(runs, []);
    resetRemoteState(gates, []);
  }
  return {
    runs,
    gates,
    mutationProblem: gateRuntime.mutationProblem,
    mutating: gateRuntime.mutating,
    loadRuns,
    loadGates,
    resolveOwnerGate,
    reset,
  };
});
