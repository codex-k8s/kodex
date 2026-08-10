import { defineStore } from "pinia";
import { reactive } from "vue";

import { configurationApi } from "@/features/configuration/api";
import {
  type ConfigurationChangeModel,
  type ConfigurationDiffModel,
  type ConfigurationInstructionOption,
  type ConfigurationSourceKind,
  type ConfigurationSourceModel,
  toConfigurationChangeModel,
  toConfigurationDiffModel,
  toConfigurationHistoryVersion,
  toConfigurationInstructionOption,
  toConfigurationSourceModel,
} from "@/features/configuration/model";
import { createFeatureRuntime } from "@/shared/lib/feature-store";
import { remoteState, resetRemoteState } from "@/shared/lib/remote";
import { subscribeRealtimeSnapshot } from "@/shared/realtime/snapshot-bus";

export const useConfigurationStore = defineStore("configuration", () => {
  const changes = reactive(remoteState<ConfigurationChangeModel[]>([]));
  const instructions = reactive(
    remoteState<ConfigurationInstructionOption[]>([]),
  );
  const historyVersions = reactive(remoteState<number[]>([]));
  const diff = reactive(remoteState<ConfigurationDiffModel | null>(null));
  const source = reactive(remoteState<ConfigurationSourceModel | null>(null));
  const catalogRuntime = createFeatureRuntime();
  const historyRuntime = createFeatureRuntime();
  const detailRuntime = createFeatureRuntime();

  const load = () =>
    Promise.all([
      catalogRuntime.loadInto(
        changes,
        async () =>
          (await configurationApi.listChanges()).changes.map(
            toConfigurationChangeModel,
          ),
        (items) => items.length === 0,
      ),
      catalogRuntime.loadInto(
        instructions,
        async () =>
          (await configurationApi.listInstructions()).resources.map(
            toConfigurationInstructionOption,
          ),
        (items) => items.length === 0,
      ),
    ]).then(() => undefined);

  const loadHistory = (resourceRef: string) =>
    historyRuntime.loadInto(
      historyVersions,
      async () =>
        (await configurationApi.instructionHistory(resourceRef)).entries.map(
          toConfigurationHistoryVersion,
        ),
      (items) => items.length === 0,
    );

  const compare = (resourceRef: string, left: number, right: number) =>
    detailRuntime.loadInto(
      diff,
      async () =>
        toConfigurationDiffModel(
          await configurationApi.diff(resourceRef, left, right),
        ),
      (value) => value === null,
    );

  const loadSource = (resourceRef: string, kind: ConfigurationSourceKind) => {
    source.data = null;
    return detailRuntime.loadInto(
      source,
      async () =>
        toConfigurationSourceModel(
          await configurationApi.source(resourceRef, kind),
        ),
      (value) => value === null,
    );
  };

  subscribeRealtimeSnapshot("CONFIGURATION_CHANGES", (snapshot) => {
    changes.data = (snapshot.items.configurationChanges ?? []).map(
      toConfigurationChangeModel,
    );
    changes.phase = changes.data.length ? "ready" : "empty";
  });

  function reset(): void {
    catalogRuntime.invalidate();
    historyRuntime.invalidate();
    detailRuntime.invalidate();
    resetRemoteState(changes, []);
    resetRemoteState(instructions, []);
    resetRemoteState(historyVersions, []);
    resetRemoteState(diff, null);
    resetRemoteState(source, null);
  }

  return {
    changes,
    instructions,
    historyVersions,
    diff,
    source,
    load,
    loadHistory,
    compare,
    loadSource,
    reset,
  };
});
