import { defineStore } from "pinia";
import { reactive } from "vue";

import {
  cancelAuthorization,
  commandProviderPool,
  fetchAuthorization,
  fetchConfigurationSource,
  fetchConnections,
  fetchProviderPools,
  fetchProviders,
  reauthorizeConnection,
  restartAuthorization,
  revokeConnection,
  startAuthorization,
} from "@/shared/api/adapters/owner-control";
import { fetchResources } from "@/shared/api/adapters/resources";
import type { ProviderPoolCommand } from "@/shared/api/generated/openapi/types.gen";
import { createFeatureRuntime } from "@/shared/lib/feature-store";
import { invalidate, remoteState, resetRemoteState } from "@/shared/lib/remote";
import { subscribeRealtimeSnapshot } from "@/shared/realtime/snapshot-bus";
import {
  type ProviderAuthorizationModel,
  type ProviderCatalogModel,
  type ProviderConfigurationSourceModel,
  type ProviderConnectionModel,
  type ProviderPoolModel,
  toProviderAuthorizationModel,
  toProviderCatalogModel,
  toProviderConfigurationSourceModel,
  toProviderConnectionModel,
  toProviderPoolModel,
} from "./model";

export const useProvidersStore = defineStore("providers", () => {
  const providers = reactive(remoteState<ProviderCatalogModel[]>([]));
  const authorization = reactive(
    remoteState<ProviderAuthorizationModel | null>(null),
  );
  const connections = reactive(remoteState<ProviderConnectionModel[]>([]));
  const pools = reactive(remoteState<ProviderPoolModel[]>([]));
  const configurationSource = reactive(
    remoteState<ProviderConfigurationSourceModel | null>(null),
  );
  const runtime = createFeatureRuntime();
  const loadProviders = () =>
    Promise.all([
      runtime.loadInto(
        providers,
        async () =>
          (await fetchProviders()).providers.map(toProviderCatalogModel),
        (items) => items.length === 0,
      ),
      runtime.loadInto(
        connections,
        async () =>
          (await fetchConnections()).connections.map(toProviderConnectionModel),
        (items) => items.length === 0,
      ),
      runtime.loadInto(
        pools,
        async () => {
          const [views, resources] = await Promise.all([
            fetchProviderPools(),
            fetchResources("PROVIDER_POOL"),
          ]);
          const byID = new Map(
            resources.resources.map((resource) => [resource.id, resource]),
          );
          return views.pools.map((item) =>
            toProviderPoolModel(item, byID.get(item.poolRef)),
          );
        },
        (items) => items.length === 0,
      ),
    ]).then(() => undefined);
  const beginAuthorization = (body: Parameters<typeof startAuthorization>[0]) =>
    runtime.mutate(
      async () => {
        authorization.data = toProviderAuthorizationModel(
          await startAuthorization(body),
        );
        authorization.phase = "ready";
      },
      loadProviders,
      connections,
    );
  const refreshAuthorization = (ref: string) =>
    runtime.loadInto(
      authorization,
      async () => toProviderAuthorizationModel(await fetchAuthorization(ref)),
      (value) => value === null,
    );
  const newAuthorizationCode = (value: ProviderAuthorizationModel) =>
    runtime.mutate(
      async () => {
        authorization.data = toProviderAuthorizationModel(
          await restartAuthorization(value.authorizationRef, value.version),
        );
        authorization.phase = "ready";
      },
      loadProviders,
      authorization,
    );
  const stopAuthorization = (value: ProviderAuthorizationModel) =>
    runtime.mutate(
      async () => {
        authorization.data = toProviderAuthorizationModel(
          await cancelAuthorization(value.authorizationRef, value.version),
        );
        authorization.phase = "ready";
      },
      loadProviders,
      authorization,
    );
  const revokeProvider = (value: ProviderConnectionModel) =>
    runtime.mutate(
      () =>
        revokeConnection(value.connectionRef, value.version, value.generation),
      loadProviders,
      connections,
    );
  const reauthorizeProvider = (value: ProviderConnectionModel) =>
    runtime.mutate(
      async () => {
        authorization.data = toProviderAuthorizationModel(
          await reauthorizeConnection(
            value.connectionRef,
            value.version,
            value.generation,
          ),
        );
        authorization.phase = "ready";
      },
      loadProviders,
      connections,
    );
  const savePool = (
    value: ProviderPoolModel | null,
    draft: {
      stableKey: string;
      displayName: string;
      policy: NonNullable<ProviderPoolCommand["policy"]>;
      members: NonNullable<ProviderPoolCommand["members"]>;
    },
  ) =>
    runtime.mutate(
      () =>
        commandProviderPool(
          {
            action: value ? "UPDATE" : "CREATE",
            ...(value ? { poolRef: value.poolRef } : {}),
            ...draft,
          },
          value?.version,
        ),
      loadProviders,
      pools,
    );
  const executePoolAction = (
    value: ProviderPoolModel,
    action: "ARCHIVE" | "DELETE",
  ) =>
    runtime.mutate(
      () =>
        commandProviderPool({ action, poolRef: value.poolRef }, value.version),
      loadProviders,
      pools,
    );
  const loadConfigurationSource = (resourceRef: string) => {
    configurationSource.data = null;
    return runtime.loadInto(
      configurationSource,
      async () =>
        toProviderConfigurationSourceModel(
          await fetchConfigurationSource(resourceRef, "PROVIDER_POOL"),
        ),
      (value) => value === null,
    );
  };
  function replaceConnections(
    items: Parameters<typeof toProviderConnectionModel>[0][],
  ): void {
    invalidate(connections);
    connections.data = items.map(toProviderConnectionModel);
    connections.phase = items.length ? "ready" : "empty";
  }
  subscribeRealtimeSnapshot("PROVIDERS", (snapshot) =>
    replaceConnections(snapshot.items.providerConnections ?? []),
  );
  function reset(): void {
    runtime.invalidate();
    resetRemoteState(providers, []);
    resetRemoteState(authorization, null);
    resetRemoteState(connections, []);
    resetRemoteState(pools, []);
    resetRemoteState(configurationSource, null);
  }
  return {
    providers,
    authorization,
    connections,
    pools,
    configurationSource,
    mutationProblem: runtime.mutationProblem,
    mutating: runtime.mutating,
    loadProviders,
    beginAuthorization,
    refreshAuthorization,
    newAuthorizationCode,
    stopAuthorization,
    revokeProvider,
    reauthorizeProvider,
    savePool,
    executePoolAction,
    loadConfigurationSource,
    replaceConnections,
    reset,
  };
});
