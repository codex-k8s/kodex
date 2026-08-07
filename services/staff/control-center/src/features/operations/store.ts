import { defineStore } from "pinia";
import { reactive, ref } from "vue";

import {
  fetchAudit,
  fetchBackups,
  fetchConfigurationChanges,
  fetchDiagnostics,
  fetchIncidents,
  fetchRestoreOperations,
  fetchRuns,
  resolveGate,
  restoreSessionBackup,
} from "@/shared/api/adapters/operations";
import { fetchResources } from "@/shared/api/adapters/resources";
import type {
  AuditEvent,
  Backup,
  ConfigurationChange,
  Diagnostics,
  ResolveOwnerGate,
  Resource,
  RestoreOperation,
  RuntimeIncident,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import {
  beginRequest,
  failRequest,
  finishRequest,
  invalidate,
  remoteState,
  type RemoteState,
} from "@/shared/lib/remote";

export const useOperationsStore = defineStore("operations", () => {
  const runs = reactive(remoteState<Resource[]>([]));
  const gates = reactive(remoteState<Resource[]>([]));
  const incidents = reactive(remoteState<RuntimeIncident[]>([]));
  const backups = reactive(remoteState<Backup[]>([]));
  const restores = reactive(remoteState<RestoreOperation[]>([]));
  const audit = reactive(remoteState<AuditEvent[]>([]));
  const changes = reactive(remoteState<ConfigurationChange[]>([]));
  const diagnostics = reactive(remoteState<Diagnostics | null>(null));
  const mutationProblem = ref<AppProblem | null>(null);
  const mutating = ref(false);
  let mutationVersion = 0;

  async function loadInto<T>(
    state: RemoteState<T>,
    loader: () => Promise<T>,
    isEmpty: (value: T) => boolean,
  ): Promise<void> {
    const request = beginRequest(state);
    try {
      const data = await loader();
      finishRequest(state, request, data, isEmpty(data));
    } catch (error) {
      failRequest(state, request, asProblem(error));
    }
  }

  const loadRuns = () =>
    loadInto(
      runs,
      async () => (await fetchRuns()).resources,
      (items) => items.length === 0,
    );
  const loadGates = () =>
    loadInto(
      gates,
      async () => (await fetchResources("OWNER_GATE")).resources,
      (items) => items.length === 0,
    );
  const loadIncidents = () =>
    loadInto(
      incidents,
      async () => (await fetchIncidents()).incidents,
      (items) => items.length === 0,
    );
  const loadBackups = () =>
    loadInto(
      backups,
      async () => (await fetchBackups()).backups,
      (items) => items.length === 0,
    );
  const loadRestores = () =>
    loadInto(
      restores,
      async () => (await fetchRestoreOperations()).operations,
      (items) => items.length === 0,
    );
  const loadAudit = () =>
    loadInto(
      audit,
      async () => (await fetchAudit()).events,
      (items) => items.length === 0,
    );
  const loadChanges = () =>
    loadInto(
      changes,
      async () => (await fetchConfigurationChanges()).changes,
      (items) => items.length === 0,
    );
  const loadDiagnostics = () =>
    loadInto(diagnostics, fetchDiagnostics, (value) => value === null);

  async function mutate<T>(
    operation: () => Promise<unknown>,
    reload: () => Promise<void>,
    state: RemoteState<T>,
  ): Promise<boolean> {
    const version = ++mutationVersion;
    invalidate(state);
    mutationProblem.value = null;
    mutating.value = true;
    try {
      await operation();
      if (version !== mutationVersion) return false;
      await reload();
      if (version !== mutationVersion) return false;
      return true;
    } catch (error) {
      if (version !== mutationVersion) return false;
      mutationProblem.value = asProblem(error);
      if (mutationProblem.value.kind === "conflict") state.phase = "conflict";
      return false;
    } finally {
      if (version === mutationVersion) mutating.value = false;
    }
  }

  const resolveOwnerGate = (resource: Resource, body: ResolveOwnerGate) =>
    mutate(() => resolveGate(resource, body), loadGates, gates);
  const restore = (backup: Backup) => {
    invalidate(restores);
    return mutate(
      () => restoreSessionBackup(backup),
      async () =>
        Promise.all([loadBackups(), loadRestores()]).then(() => undefined),
      backups,
    );
  };

  function replaceRealtimeResources(
    channel: "RUNS" | "RESOURCES",
    items: Resource[],
  ): void {
    if (channel === "RUNS") {
      invalidate(runs);
      runs.data = items.filter((item) => item.kind === "PROCESS_RUN");
      runs.phase = runs.data.length ? "ready" : "empty";
    } else {
      invalidate(gates);
      gates.data = items.filter((item) => item.kind === "OWNER_GATE");
      gates.phase = gates.data.length ? "ready" : "empty";
    }
  }

  function replaceRealtimeIncidents(items: RuntimeIncident[]): void {
    invalidate(incidents);
    incidents.data = items;
    incidents.phase = items.length ? "ready" : "empty";
  }

  function replaceRealtimeChanges(items: ConfigurationChange[]): void {
    invalidate(changes);
    changes.data = items;
    changes.phase = items.length ? "ready" : "empty";
  }

  return {
    runs,
    gates,
    incidents,
    backups,
    restores,
    audit,
    changes,
    diagnostics,
    mutationProblem,
    mutating,
    loadRuns,
    loadGates,
    loadIncidents,
    loadBackups,
    loadRestores,
    loadAudit,
    loadChanges,
    loadDiagnostics,
    resolveOwnerGate,
    restore,
    replaceRealtimeResources,
    replaceRealtimeIncidents,
    replaceRealtimeChanges,
  };
});
