import type {
  ConfigurationSourceDetail,
  Provider,
  ProviderAuthorization,
  ProviderConnection,
  ProviderPoolView,
  Resource,
} from "@/shared/api/generated/openapi/types.gen";

export interface ProviderCatalogModel {
  providerRef: string;
  displayName: string;
  authorizationModes: string[];
}

export interface ProviderAuthorizationModel {
  authorizationRef: string;
  providerRef: string;
  version: number;
  state: string;
  verificationUrl: string;
  userCode: string;
}

export interface ProviderConnectionModel {
  connectionRef: string;
  stableKey: string;
  displayName: string;
  version: number;
  generation: number;
  state: string;
  maskedLabel: string;
  maskedAccount: string;
  capabilities: string[];
  capacity: { usage: number; limit: number } | null;
}

export interface ProviderPoolModel {
  poolRef: string;
  stableKey: string;
  displayName: string;
  policy: "least_used" | "weighted";
  version: number;
  state: string;
  members: Array<{
    connectionRef: string;
    connectionVersion: number;
    connectionGeneration: number;
    weight: number;
    eligible: boolean;
  }>;
  ownership: {
    managedBy: "ui" | "git";
    source: string;
    revision: number;
    drift: string;
  } | null;
}

export interface ProviderConfigurationSourceModel {
  managedBy: "ui" | "git";
  source: string;
  sourceRevision: number;
  sourceSha256: string;
  drift: string;
  version: number;
}

export const toProviderCatalogModel = (
  value: Provider,
): ProviderCatalogModel => ({
  providerRef: value.providerRef,
  displayName: value.displayName,
  authorizationModes: [...value.authorizationModes],
});

export const toProviderAuthorizationModel = (
  value: ProviderAuthorization,
): ProviderAuthorizationModel => ({
  authorizationRef: value.authorizationRef,
  providerRef: value.providerRef,
  version: value.version,
  state: value.state,
  verificationUrl: value.verificationUrl ?? "",
  userCode: value.userCode ?? "",
});

export const toProviderConnectionModel = (
  value: ProviderConnection,
): ProviderConnectionModel => ({
  connectionRef: value.connectionRef,
  stableKey: value.stableKey,
  displayName: value.displayName,
  version: value.version,
  generation: value.generation,
  state: value.state,
  maskedLabel: value.maskedLabel,
  maskedAccount: value.maskedAccount,
  capabilities: [...value.capabilities],
  capacity: value.capacity
    ? { usage: value.capacity.usage, limit: value.capacity.limit }
    : null,
});

export const toProviderPoolModel = (
  value: ProviderPoolView,
  resource?: Resource,
): ProviderPoolModel => {
  const source = resource?.spec.providerPool?.ownership;
  return {
    poolRef: value.poolRef,
    stableKey: value.stableKey,
    displayName: value.displayName,
    policy: value.policy,
    version: value.version,
    state: value.state,
    members: value.members.map((item) => ({
      connectionRef: item.connectionRef,
      connectionVersion: item.connectionVersion,
      connectionGeneration: item.connectionGeneration,
      weight: item.weight,
      eligible: item.eligible,
    })),
    ownership: source
      ? {
          managedBy: source.managedBy,
          source: source.source,
          revision: source.revision,
          drift: source.drift,
        }
      : null,
  };
};

export const toProviderConfigurationSourceModel = (
  value: ConfigurationSourceDetail,
): ProviderConfigurationSourceModel => ({
  managedBy: value.managedBy,
  source: value.source,
  sourceRevision: value.sourceRevision,
  sourceSha256: value.sourceSha256 ?? "",
  drift: value.drift,
  version: value.version,
});
