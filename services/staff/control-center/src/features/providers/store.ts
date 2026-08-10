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
import type {
  ConfigurationSourceDetail,
  Provider,
  ProviderAuthorization,
  ProviderConnection,
  ProviderPoolCommand,
  ProviderPoolView,
} from "@/shared/api/generated/openapi/types.gen";
import { createFeatureRuntime } from "@/shared/lib/feature-store";
import { invalidate, remoteState, resetRemoteState } from "@/shared/lib/remote";
import { resourceOwnership } from "@/shared/lib/resources";
import { subscribeRealtimeSnapshot } from "@/shared/realtime/snapshot-bus";

export const useProvidersStore = defineStore("providers", () => {
  const providers = reactive(remoteState<Provider[]>([]));
  const authorization = reactive(
    remoteState<ProviderAuthorization | null>(null),
  );
  const connections = reactive(remoteState<ProviderConnection[]>([]));
  const pools = reactive(remoteState<ProviderPoolView[]>([]));
  const configurationSource = reactive(
    remoteState<ConfigurationSourceDetail | null>(null),
  );
  const poolOwnership = reactive(
    new Map<
      string,
      {
        managedBy: "ui" | "git";
        source: string;
        revision: number;
        drift: "NOT_APPLICABLE" | "IN_SYNC" | "DRIFTED" | "UNKNOWN";
      }
    >(),
  );
  const runtime = createFeatureRuntime();
  const loadProviders = () =>
    Promise.all([
      runtime.loadInto(
        providers,
        async () => (await fetchProviders()).providers,
        (items) => items.length === 0,
      ),
      runtime.loadInto(
        connections,
        async () => (await fetchConnections()).connections,
        (items) => items.length === 0,
      ),
      runtime.loadInto(
        pools,
        async () => {
          const [views, resources] = await Promise.all([
            fetchProviderPools(),
            fetchResources("PROVIDER_POOL"),
          ]);
          poolOwnership.clear();
          resources.resources.forEach((resource) => {
            const ownership = resourceOwnership(resource);
            if (ownership)
              poolOwnership.set(resource.id, {
                managedBy: ownership.managedBy,
                source: ownership.source,
                revision: ownership.revision,
                drift: ownership.drift,
              });
          });
          return views.pools;
        },
        (items) => items.length === 0,
      ),
    ]).then(() => undefined);
  const beginAuthorization = (body: Parameters<typeof startAuthorization>[0]) =>
    runtime.mutate(
      async () => {
        authorization.data = await startAuthorization(body);
        authorization.phase = "ready";
      },
      loadProviders,
      connections,
    );
  const refreshAuthorization = (ref: string) =>
    runtime.loadInto(
      authorization,
      () => fetchAuthorization(ref),
      (value) => value === null,
    );
  const newAuthorizationCode = (value: ProviderAuthorization) =>
    runtime.mutate(
      async () => {
        authorization.data = await restartAuthorization(
          value.authorizationRef,
          value.version,
        );
        authorization.phase = "ready";
      },
      loadProviders,
      authorization,
    );
  const stopAuthorization = (value: ProviderAuthorization) =>
    runtime.mutate(
      async () => {
        authorization.data = await cancelAuthorization(
          value.authorizationRef,
          value.version,
        );
        authorization.phase = "ready";
      },
      loadProviders,
      authorization,
    );
  const revokeProvider = (value: ProviderConnection) =>
    runtime.mutate(
      () =>
        revokeConnection(value.connectionRef, value.version, value.generation),
      loadProviders,
      connections,
    );
  const reauthorizeProvider = (value: ProviderConnection) =>
    runtime.mutate(
      async () => {
        authorization.data = await reauthorizeConnection(
          value.connectionRef,
          value.version,
          value.generation,
        );
        authorization.phase = "ready";
      },
      loadProviders,
      connections,
    );
  const savePool = (
    value: ProviderPoolView | null,
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
    value: ProviderPoolView,
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
      () => fetchConfigurationSource(resourceRef, "PROVIDER_POOL"),
      (value) => value === null,
    );
  };
  function replaceConnections(items: ProviderConnection[]): void {
    invalidate(connections);
    connections.data = items;
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
    poolOwnership.clear();
  }
  return {
    providers,
    authorization,
    connections,
    pools,
    configurationSource,
    poolOwnership,
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
