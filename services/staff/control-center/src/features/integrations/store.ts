import { defineStore } from "pinia";
import { reactive } from "vue";

import {
  decideApproval,
  fetchConnections,
  fetchIntegrationApprovals,
  fetchIntegrationConfigurations,
  fetchIntegrationDefinitions,
  fetchIntegrationTest,
  saveIntegrationConfiguration,
  testIntegration,
} from "@/shared/api/adapters/owner-control";
import { createFeatureRuntime } from "@/shared/lib/feature-store";
import { invalidate, remoteState, resetRemoteState } from "@/shared/lib/remote";
import { subscribeRealtimeSnapshot } from "@/shared/realtime/snapshot-bus";
import {
  type IntegrationApprovalModel,
  type IntegrationConnectionModel,
  type IntegrationDefinitionModel,
  type IntegrationTestModel,
  type IntegrationView,
  toIntegrationApprovalModel,
  toIntegrationConnectionModel,
  toIntegrationDefinitionModel,
  toIntegrationTestModel,
  toIntegrationView,
} from "./model";

export const useIntegrationsStore = defineStore("integrations", () => {
  const integrationDefinitions = reactive(
    remoteState<IntegrationDefinitionModel[]>([]),
  );
  const integrationConfigurations = reactive(
    remoteState<IntegrationView[]>([]),
  );
  const approvals = reactive(remoteState<IntegrationApprovalModel[]>([]));
  const connections = reactive(remoteState<IntegrationConnectionModel[]>([]));
  const integrationTest = reactive(
    remoteState<IntegrationTestModel | null>(null),
  );
  const runtime = createFeatureRuntime();
  const loadIntegrations = () =>
    Promise.all([
      runtime.loadInto(
        integrationDefinitions,
        async () =>
          (await fetchIntegrationDefinitions()).definitions.map(
            toIntegrationDefinitionModel,
          ),
        (items) => items.length === 0,
      ),
      runtime.loadInto(
        integrationConfigurations,
        async () =>
          (await fetchIntegrationConfigurations()).configurations.map(
            toIntegrationView,
          ),
        (items) => items.length === 0,
      ),
      runtime.loadInto(
        approvals,
        async () =>
          (await fetchIntegrationApprovals()).approvals.map(
            toIntegrationApprovalModel,
          ),
        (items) => items.length === 0,
      ),
      runtime.loadInto(
        connections,
        async () =>
          (await fetchConnections()).connections.map(
            toIntegrationConnectionModel,
          ),
        (items) => items.length === 0,
      ),
    ]).then(() => undefined);
  const saveIntegrationDraft = (
    value: IntegrationView | null,
    draft: {
      stableKey: string;
      definitionRef: string;
      connectionRef: string;
      capabilities: string[];
      effectKind: IntegrationView["effectKind"];
    },
  ) => {
    const definition = integrationDefinitions.data.find(
      (item) => item.definitionRef === draft.definitionRef,
    );
    const connection = connections.data.find(
      (item) => item.connectionRef === draft.connectionRef,
    );
    if (!definition || !connection) return Promise.resolve(false);
    const permitted = new Set(
      definition.capabilities
        .map((item) => item.name)
        .filter((item) => connection.capabilities.includes(item)),
    );
    const capabilities = draft.capabilities.filter((item) =>
      permitted.has(item),
    );
    if (!capabilities.length) return Promise.resolve(false);
    return runtime.mutate(
      () =>
        saveIntegrationConfiguration(
          {
            ...(value ? { configurationRef: value.configurationRef } : {}),
            stableKey: draft.stableKey,
            definitionRef: definition.definitionRef,
            definitionVersion: definition.version,
            definitionDigestSha256: definition.digestSha256,
            connectionRef: connection.connectionRef,
            connectionVersion: connection.version,
            connectionGeneration: connection.generation,
            capabilities,
            effectKind: draft.effectKind,
          },
          value?.version,
        ),
      loadIntegrations,
      integrationConfigurations,
    );
  };
  const runIntegrationTest = (value: IntegrationView) =>
    runtime.mutate(
      async () => {
        integrationTest.data = toIntegrationTestModel(
          await testIntegration({
            connectionRef: value.connectionRef,
            connectionVersion: value.connectionVersion,
            connectionGeneration: value.connectionGeneration,
            definitionRef: value.definitionRef,
            definitionVersion: value.definitionVersion,
            definitionDigestSha256: value.definitionDigestSha256,
            configurationRef: value.configurationRef,
            configurationVersion: value.version,
            configurationDigestSha256: value.digestSha256,
          }),
        );
        integrationTest.phase = "ready";
      },
      () => Promise.resolve(),
      integrationTest,
    );
  const refreshIntegrationTest = (ref: string) =>
    runtime.loadInto(
      integrationTest,
      async () => toIntegrationTestModel(await fetchIntegrationTest(ref)),
      (value) => value === null,
    );
  const reviewApproval = (
    value: IntegrationApprovalModel,
    decision: "APPROVE" | "REJECT",
    reasonCode: string,
  ) =>
    runtime.mutate(
      () =>
        decideApproval(value.approvalRef, value.version, {
          expectedRequestHash: value.requestHash,
          decision,
          reasonCode,
        }),
      loadIntegrations,
      approvals,
    );
  function replaceConnections(
    items: Parameters<typeof toIntegrationConnectionModel>[0][],
  ): void {
    invalidate(connections);
    connections.data = items.map(toIntegrationConnectionModel);
    connections.phase = items.length ? "ready" : "empty";
  }
  function replaceIntegrations(
    items: Parameters<typeof toIntegrationView>[0][],
  ): void {
    invalidate(integrationConfigurations);
    integrationConfigurations.data = items.map(toIntegrationView);
    integrationConfigurations.phase = items.length ? "ready" : "empty";
  }
  function replaceApprovals(
    items: Parameters<typeof toIntegrationApprovalModel>[0][],
  ): void {
    invalidate(approvals);
    approvals.data = items.map(toIntegrationApprovalModel);
    approvals.phase = items.length ? "ready" : "empty";
  }
  subscribeRealtimeSnapshot("INTEGRATIONS", (snapshot) =>
    replaceIntegrations(snapshot.items.integrationConfigurations ?? []),
  );
  subscribeRealtimeSnapshot("APPROVALS", (snapshot) =>
    replaceApprovals(snapshot.items.approvals ?? []),
  );
  function reset(): void {
    runtime.invalidate();
    resetRemoteState(integrationDefinitions, []);
    resetRemoteState(integrationConfigurations, []);
    resetRemoteState(approvals, []);
    resetRemoteState(connections, []);
    resetRemoteState(integrationTest, null);
  }
  return {
    integrationDefinitions,
    integrationConfigurations,
    approvals,
    connections,
    integrationTest,
    mutationProblem: runtime.mutationProblem,
    mutating: runtime.mutating,
    loadIntegrations,
    saveIntegrationDraft,
    runIntegrationTest,
    refreshIntegrationTest,
    reviewApproval,
    replaceConnections,
    replaceIntegrations,
    replaceApprovals,
    reset,
  };
});

export type { IntegrationView } from "./model";
