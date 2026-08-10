import { defineStore } from "pinia";
import { reactive } from "vue";

import {
  fetchBackups,
  fetchDiagnostics,
  fetchIncidents,
  fetchRuns,
} from "@/shared/api/adapters/operations";
import { fetchResources } from "@/shared/api/adapters/resources";
import { fetchProjects } from "@/shared/api/adapters/projects";
import type {
  Backup,
  Diagnostics,
  IncidentView,
  Resource,
  RunView,
} from "@/shared/api/generated/openapi/types.gen";
import { createFeatureRuntime } from "@/shared/lib/feature-store";
import { remoteState, resetRemoteState } from "@/shared/lib/remote";

export const useOverviewStore = defineStore("overview", () => {
  const runs = reactive(remoteState<RunView[]>([]));
  const projects = reactive(remoteState<Resource[]>([]));
  const gates = reactive(remoteState<Resource[]>([]));
  const incidents = reactive(remoteState<IncidentView[]>([]));
  const backups = reactive(remoteState<Backup[]>([]));
  const diagnostics = reactive(remoteState<Diagnostics | null>(null));
  const runtime = createFeatureRuntime();
  const load = () =>
    Promise.all([
      runtime.loadInto(
        projects,
        async () => (await fetchProjects()).resources,
        (v) => !v.length,
      ),
      runtime.loadInto(
        runs,
        async () => (await fetchRuns()).runs,
        (v) => !v.length,
      ),
      runtime.loadInto(
        gates,
        async () => (await fetchResources("OWNER_GATE")).resources,
        (v) => !v.length,
      ),
      runtime.loadInto(
        incidents,
        async () => (await fetchIncidents()).incidents,
        (v) => !v.length,
      ),
      runtime.loadInto(
        backups,
        async () => (await fetchBackups()).backups,
        (v) => !v.length,
      ),
      runtime.loadInto(diagnostics, fetchDiagnostics, (v) => v === null),
    ]).then(() => undefined);
  function reset(): void {
    runtime.invalidate();
    resetRemoteState(runs, []);
    resetRemoteState(projects, []);
    resetRemoteState(gates, []);
    resetRemoteState(incidents, []);
    resetRemoteState(backups, []);
    resetRemoteState(diagnostics, null);
  }
  return {
    projects,
    runs,
    gates,
    incidents,
    backups,
    diagnostics,
    load,
    reset,
  };
});
