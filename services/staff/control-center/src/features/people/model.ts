import type {
  AgentBotIdentity,
  AgentBotIdentityOperation,
  AgentView,
  ConfigurationSourceDetail,
  OwnerConfigurationCatalog,
  ProviderPoolView,
  Resource,
  ResourceHistoryEntry,
} from "@/shared/api/generated/openapi/types.gen";

/** Разбирает contract-bounded capability list без локального реестра permissions. */
export function parseCapabilities(input: string): string[] | null {
  const values = input
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
  if (
    values.length > 64 ||
    values.some((item) => item.length > 160) ||
    new Set(values).size !== values.length
  )
    return null;
  return values;
}

export interface PeopleOwnershipModel {
  managedBy: "ui" | "git";
  source: string;
  revision: number;
  drift: string;
}

export interface RoleDefinitionModel {
  id: string;
  name: string;
  state: string;
  version: number;
  stableKey: string;
  description: string;
  capabilities: string[];
  allowedTargetRoleDefinitionRefs: string[];
  roleImageRecipeRef: string;
  roleImageRecipeVersion: number;
  roleImageRecipeSha256: string;
  ownership: PeopleOwnershipModel;
}

export interface AgentModel {
  agentRef: string;
  displayName: string;
  stableKey: string;
  version: number;
  state: string;
  enabled: boolean;
  capabilities: string[];
  botIdentity: {
    status: string;
    username: string;
    maskedStatus: string;
    providerGeneration: number;
  };
  runtimeSelection: { selectionKey: string; displayName: string };
  instructionSelection: { selector: string; displayName: string };
  providerPoolSelection: { selector: string; displayName: string };
}

export interface BotIdentityModel {
  selector: string;
  username: string;
  displayName: string;
  status: string;
  providerVersion: number;
  providerGeneration: number;
}

export interface BotOperationModel {
  operationRef: string;
  action: string;
  state: string;
  agentRef: string;
}

export interface AssignmentModel {
  id: string;
  name: string;
  state: string;
  version: number;
  agentRef: string;
  roomRef: string;
}

export interface PeopleSelectionModel {
  id: string;
  name: string;
  version: number;
  stableKey: string;
  specSha256: string;
}

export interface ProviderPoolSelectionModel {
  poolRef: string;
  stableKey: string;
  displayName: string;
}

export interface PeopleHistoryModel {
  resourceId: string;
  resourceName: string;
  resourceVersion: number;
  action: string;
}

export interface PeopleCatalogModel {
  runtimeSelections: Array<{
    selectionKey: string;
    displayName: string;
    status: string;
  }>;
}

export interface PeopleConfigurationSourceModel {
  managedBy: "ui" | "git";
  source: string;
  sourceRevision: number;
  sourceSha256: string;
  drift: string;
  version: number;
}

const toOwnership = (value: Resource): PeopleOwnershipModel => {
  const source = value.spec.roleDefinition?.ownership;
  return {
    managedBy: source?.managedBy ?? "ui",
    source: source?.source ?? "",
    revision: source?.revision ?? 0,
    drift: source?.drift ?? "NOT_APPLICABLE",
  };
};

export const toRoleDefinitionModel = (value: Resource): RoleDefinitionModel => {
  const projection = value.spec.roleDefinition;
  return {
    id: value.id,
    name: value.name,
    state: value.state,
    version: value.version,
    stableKey: projection?.stableKey ?? "",
    description: projection?.description ?? "",
    capabilities: [...(projection?.capabilities ?? [])],
    allowedTargetRoleDefinitionRefs: [
      ...(projection?.allowedTargetRoleDefinitionRefs ?? []),
    ],
    roleImageRecipeRef: projection?.roleImageRecipeRef ?? "",
    roleImageRecipeVersion: projection?.roleImageRecipeVersion ?? 0,
    roleImageRecipeSha256: projection?.roleImageRecipeSha256 ?? "",
    ownership: toOwnership(value),
  };
};

export const toAgentModel = (value: AgentView): AgentModel => ({
  agentRef: value.agentRef,
  displayName: value.displayName,
  stableKey: value.stableKey,
  version: value.version,
  state: value.state,
  enabled: value.enabled,
  capabilities: [...value.capabilities],
  botIdentity: {
    status: value.botIdentity.status,
    username: value.botIdentity.username,
    maskedStatus: value.botIdentity.maskedStatus,
    providerGeneration: value.botIdentity.providerGeneration,
  },
  runtimeSelection: {
    selectionKey: value.runtimeSelection.selectionKey,
    displayName: value.runtimeSelection.displayName,
  },
  instructionSelection: {
    selector: value.instructionSelection.selector,
    displayName: value.instructionSelection.displayName,
  },
  providerPoolSelection: {
    selector: value.providerPoolSelection.selector,
    displayName: value.providerPoolSelection.displayName,
  },
});

export const toBotIdentityModel = (
  value: AgentBotIdentity,
): BotIdentityModel => ({
  selector: value.selector,
  username: value.username,
  displayName: value.displayName,
  status: value.status,
  providerVersion: value.providerVersion,
  providerGeneration: value.providerGeneration,
});

export const toBotOperationModel = (
  value: AgentBotIdentityOperation,
): BotOperationModel => ({
  operationRef: value.operationRef,
  action: value.action,
  state: value.state,
  agentRef: value.agentRef,
});

export const toAssignmentModel = (value: Resource): AssignmentModel => ({
  id: value.id,
  name: value.name,
  state: value.state,
  version: value.version,
  agentRef: value.spec.agentAssignment?.agentRef ?? "",
  roomRef: value.spec.agentAssignment?.roomRef ?? "",
});

export const toPeopleSelectionModel = (
  value: Resource,
): PeopleSelectionModel => ({
  id: value.id,
  name: value.name,
  version: value.version,
  stableKey:
    value.spec.chat?.stableKey ?? value.spec.instructionSet?.stableKey ?? "",
  specSha256: value.spec.roleImageRecipe?.specSha256 ?? "",
});

export const toProviderPoolSelectionModel = (
  value: ProviderPoolView,
): ProviderPoolSelectionModel => ({
  poolRef: value.poolRef,
  stableKey: value.stableKey,
  displayName: value.displayName,
});

export const toPeopleHistoryModel = (
  value: ResourceHistoryEntry,
): PeopleHistoryModel => ({
  resourceId: value.resource.id,
  resourceName: value.resource.name,
  resourceVersion: value.resource.version,
  action: value.action,
});

export const toPeopleCatalogModel = (
  value: OwnerConfigurationCatalog,
): PeopleCatalogModel => ({
  runtimeSelections: value.runtimeSelections.map((item) => ({
    selectionKey: item.selectionKey,
    displayName: item.displayName,
    status: item.status,
  })),
});

export const toPeopleConfigurationSourceModel = (
  value: ConfigurationSourceDetail,
): PeopleConfigurationSourceModel => ({
  managedBy: value.managedBy,
  source: value.source,
  sourceRevision: value.sourceRevision,
  sourceSha256: value.sourceSha256 ?? "",
  drift: value.drift,
  version: value.version,
});
