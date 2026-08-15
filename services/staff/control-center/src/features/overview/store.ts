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
import { createFeatureRuntime } from "@/shared/lib/feature-store";
import {
  beginRequest,
  finishRequest,
  remoteState,
  resetRemoteState,
  type RemoteState,
} from "@/shared/lib/remote";
import { projectReference } from "@/shared/lib/project-scope";
import {
  type OverviewBackup,
  type OverviewDiagnostics,
  type OverviewIncident,
  type OverviewResource,
  type OverviewRun,
  toOverviewBackup,
  toOverviewDiagnostics,
  toOverviewIncident,
  toOverviewResource,
  toOverviewRun,
} from "./model";

export const useOverviewStore = defineStore("overview", () => {
  const runs = reactive(remoteState<OverviewRun[]>([]));
  const projects = reactive(remoteState<OverviewResource[]>([]));
  const gates = reactive(remoteState<OverviewResource[]>([]));
  const incidents = reactive(remoteState<OverviewIncident[]>([]));
  const backups = reactive(remoteState<OverviewBackup[]>([]));
  const diagnostics = reactive(remoteState<OverviewDiagnostics | null>(null));
  const runtime = createFeatureRuntime();
  function markEmpty<T>(state: RemoteState<T>, value: T): void {
    const request = beginRequest(state);
    finishRequest(state, request, value, true);
  }
  async function load(): Promise<void> {
    await runtime.loadInto(
      projects,
      async () => (await fetchProjects()).resources.map(toOverviewResource),
      (v) => !v.length,
    );
    if (projects.phase !== "ready" && projects.phase !== "empty") return;
    if (!projectReference()) {
      markEmpty(runs, []);
      markEmpty(gates, []);
      markEmpty(incidents, []);
      markEmpty(backups, []);
      markEmpty(diagnostics, null);
      return;
    }
    await Promise.all([
      runtime.loadInto(
        runs,
        async () => (await fetchRuns()).runs.map(toOverviewRun),
        (v) => !v.length,
      ),
      runtime.loadInto(
        gates,
        async () =>
          (await fetchResources("OWNER_GATE")).resources.map(
            toOverviewResource,
          ),
        (v) => !v.length,
      ),
      runtime.loadInto(
        incidents,
        async () => (await fetchIncidents()).incidents.map(toOverviewIncident),
        (v) => !v.length,
      ),
      runtime.loadInto(
        backups,
        async () => (await fetchBackups()).backups.map(toOverviewBackup),
        (v) => !v.length,
      ),
      runtime.loadInto(
        diagnostics,
        async () => toOverviewDiagnostics(await fetchDiagnostics()),
        (v) => v === null,
      ),
    ]);
  }
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
