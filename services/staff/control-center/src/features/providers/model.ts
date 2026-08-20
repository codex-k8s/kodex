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
  operational: boolean;
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
  operational: boolean;
  memberCount: number;
  eligibleMemberCount: number;
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
  operational: true,
});

export const toImportedProviderConnectionModel = (
  resource: Resource,
): ProviderConnectionModel | null => {
  const value = resource.spec.providerConnectionReference;
  if (!value) return null;
  return {
    connectionRef: resource.id,
    stableKey: value.stableKey,
    displayName: resource.name,
    version: resource.version,
    generation: value.referenceVersion,
    state: value.maskedStatus,
    maskedLabel: value.maskedLabel,
    maskedAccount: value.provider,
    capabilities: [...value.capabilities],
    capacity:
      value.observedLimit > 0
        ? { usage: value.observedUsage, limit: value.observedLimit }
        : null,
    operational: false,
  };
};

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
    operational: true,
    memberCount: value.members.length,
    eligibleMemberCount: value.members.filter((item) => item.eligible).length,
  };
};

export const toImportedProviderPoolModel = (
  resource: Resource,
): ProviderPoolModel | null => {
  const value = resource.spec.providerPool;
  if (!value) return null;
  return {
    poolRef: resource.id,
    stableKey: value.stableKey,
    displayName: resource.name,
    policy: value.policy,
    version: resource.version,
    state: resource.state,
    members: [],
    ownership: {
      managedBy: value.ownership.managedBy,
      source: value.ownership.source,
      revision: value.ownership.revision,
      drift: value.ownership.drift,
    },
    operational: false,
    memberCount: value.totalMembers,
    eligibleMemberCount: value.eligibleMembers,
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
