import { defineStore } from "pinia";
import { reactive } from "vue";

import {
  commandInstructionSet,
  compareInstructionVersions,
  fetchConfigurationDiff,
  fetchConfigurationSource,
  fetchInstructionHistory,
  fetchInstructionSets,
} from "@/shared/api/adapters/owner-control";
import { createFeatureRuntime } from "@/shared/lib/feature-store";
import { remoteState, resetRemoteState } from "@/shared/lib/remote";
import {
  type InstructionComparisonModel,
  type InstructionConfigurationDiffModel,
  type InstructionConfigurationSourceModel,
  type InstructionHistoryModel,
  type InstructionSetModel,
  toInstructionComparisonModel,
  toInstructionConfigurationDiffModel,
  toInstructionConfigurationSourceModel,
  toInstructionHistoryModel,
  toInstructionSetModel,
} from "./model";

export const useInstructionsStore = defineStore("instructions", () => {
  const instructionSets = reactive(remoteState<InstructionSetModel[]>([]));
  const history = reactive(remoteState<InstructionHistoryModel[]>([]));
  const comparison = reactive(
    remoteState<InstructionComparisonModel | null>(null),
  );
  const configurationDiff = reactive(
    remoteState<InstructionConfigurationDiffModel | null>(null),
  );
  const configurationSource = reactive(
    remoteState<InstructionConfigurationSourceModel | null>(null),
  );
  const runtime = createFeatureRuntime();

  const loadInstructions = () =>
    runtime.loadInto(
      instructionSets,
      async () => {
        const resources = (await fetchInstructionSets()).resources;
        return resources.map(toInstructionSetModel);
      },
      (items) => items.length === 0,
    );
  const saveDraft = (
    value: InstructionSetModel | null,
    draft: { name: string; stableKey: string; locale: string; content: string },
  ) =>
    runtime.mutate(
      () =>
        commandInstructionSet(
          {
            action: value ? "UPDATE" : "CREATE",
            ...(value ? { resourceRef: value.id } : {}),
            ...draft,
          },
          value?.version,
        ),
      loadInstructions,
      instructionSets,
    );
  const executeInstruction = (
    item: InstructionSetModel,
    action:
      | "VALIDATE"
      | "PUBLISH"
      | "ROLLBACK"
      | "DETACH"
      | "COPY"
      | "ARCHIVE"
      | "DELETE",
    targetVersion?: number,
  ) =>
    runtime.mutate(
      () =>
        commandInstructionSet(
          {
            action,
            resourceRef: item.id,
            ...(targetVersion ? { targetVersion } : {}),
            ...(action === "COPY"
              ? { name: `${item.name.slice(0, 155)}-copy` }
              : {}),
          },
          item.version,
        ),
      loadInstructions,
      instructionSets,
    );
  const loadInstructionHistory = (resourceRef: string) =>
    runtime.loadInto(
      history,
      async () =>
        (await fetchInstructionHistory(resourceRef)).entries.map(
          toInstructionHistoryModel,
        ),
      (items) => items.length === 0,
    );
  const compareInstructions = (
    resourceRef: string,
    leftVersion: number,
    rightVersion: number,
  ) =>
    runtime.loadInto(
      comparison,
      async () =>
        toInstructionComparisonModel(
          await compareInstructionVersions(
            resourceRef,
            leftVersion,
            rightVersion,
          ),
        ),
      (value) => value === null,
    );
  const loadConfigurationDiff = (
    resourceRef: string,
    leftVersion: number,
    rightVersion: number,
  ) =>
    runtime.loadInto(
      configurationDiff,
      async () =>
        toInstructionConfigurationDiffModel(
          await fetchConfigurationDiff(resourceRef, leftVersion, rightVersion),
        ),
      (value) => value === null,
    );
  const loadConfigurationSource = (
    resourceRef: string,
    kind:
      | "ROLE_DEFINITION"
      | "AGENT"
      | "INSTRUCTION_SET"
      | "PROVIDER_POOL" = "INSTRUCTION_SET",
  ) => {
    configurationSource.data = null;
    return runtime.loadInto(
      configurationSource,
      async () =>
        toInstructionConfigurationSourceModel(
          await fetchConfigurationSource(resourceRef, kind),
        ),
      (value) => value === null,
    );
  };
  function reset(): void {
    runtime.invalidate();
    resetRemoteState(instructionSets, []);
    resetRemoteState(history, []);
    resetRemoteState(comparison, null);
    resetRemoteState(configurationDiff, null);
    resetRemoteState(configurationSource, null);
  }
  return {
    instructionSets,
    history,
    instructionComparison: comparison,
    configurationDiff,
    configurationSource,
    mutationProblem: runtime.mutationProblem,
    mutating: runtime.mutating,
    loadInstructions,
    saveDraft,
    executeInstruction,
    loadInstructionHistory,
    compareInstructions,
    loadConfigurationDiff,
    loadConfigurationSource,
    reset,
  };
});
