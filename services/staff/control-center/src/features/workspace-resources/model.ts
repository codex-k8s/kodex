import type {
  AccessResourceKind,
  AccessSpecInput,
  IntegrationDefinition,
  MutableResourceKind,
  Resource,
  ResourceSpecInput,
} from "@/shared/api/generated/openapi/types.gen";
import { resourceOwnership } from "@/shared/lib/resources";

export type WorkspaceMutableKind =
  | "CHAT"
  | "CREDENTIAL_BINDING"
  | "REPOSITORY_WORKSPACE"
  | "INTEGRATION";
export type WorkspaceAccessKind = "TEAM" | "ROLE" | "PROMPT_PROFILE";
export type WorkspaceResourceKind = WorkspaceMutableKind | WorkspaceAccessKind;
export type WorkspaceResourceState =
  | "ACTIVE"
  | "PAUSED"
  | "ARCHIVED"
  | "DELETION_PENDING"
  | "DELETED";
export type WorkspaceResourceAction =
  | "UPDATE"
  | "ACTIVATE"
  | "PAUSE"
  | "ARCHIVE"
  | "DELETE"
  | "DETACH"
  | "COPY";

export interface WorkspaceOwnershipModel {
  managedBy: "ui" | "git";
  source: string;
  revision: number;
  sourceSha256?: string;
  drift: "NOT_APPLICABLE" | "IN_SYNC" | "DRIFTED" | "UNKNOWN";
}

export interface WorkspaceResourceModel {
  id: string;
  selector: string;
  name: string;
  kind: WorkspaceResourceKind;
  state: WorkspaceResourceState;
  version: number;
  nextActions: WorkspaceResourceAction[];
  ownership: WorkspaceOwnershipModel | null;
  credential: WorkspaceCredentialBindingModel | null;
  draft: WorkspaceResourceDraft;
}

export interface WorkspaceCredentialBindingModel {
  purpose: string;
  revision: number;
  providerEligible: boolean;
  expiresAt?: string;
}

export interface WorkspaceSelectorModel {
  selector: string;
  name: string;
  kind:
    | "AGENT"
    | "ROLE_IMAGE_RECIPE"
    | "PROVIDER_CONNECTION_REFERENCE"
    | "REPOSITORY_WORKSPACE"
    | "INTEGRATION"
    | "TEAM"
    | "PROMPT_PROFILE";
  version: number;
}

export interface WorkspaceIntegrationDefinitionModel {
  definitionRef: string;
  version: number;
  displayName: string;
  state: string;
  capabilities: Array<{
    name: string;
    risk: string;
    requiresApproval: boolean;
  }>;
}

export interface WorkspaceResourceDraft {
  stableKey: string;
  roomType: "USER" | "COORDINATION" | "WORK_CONTROL" | "RUNS";
  workPolicy: string;
  defaultAgentSelector: string;
  channelSelector: string;
  purpose: string;
  sourceKind: "PROVIDER_CONNECTION_REFERENCE" | "CREDENTIAL_BINDING";
  sourceSelector: string;
  revision: number;
  repositorySelector: string;
  workspaceMode: string;
  defaultBranch: string;
  credentialBindingSelector: string;
  definitionRef: string;
  definitionVersion: number;
  capabilities: string[];
  credentialBindingSelectors: string[];
  memberActorSelectors: string[];
  roleSelectors: string[];
  allowedTargetRoleSelectors: string[];
  promptProfileSelector: string;
  roleImageRecipeSelector: string;
  repositoryWorkspaceSelectors: string[];
  integrationSelectors: string[];
  contentSha256: string;
  locale: string;
}

export function emptyWorkspaceResourceDraft(): WorkspaceResourceDraft {
  return {
    stableKey: "",
    roomType: "USER",
    workPolicy: "default",
    defaultAgentSelector: "",
    channelSelector: "",
    purpose: "",
    sourceKind: "PROVIDER_CONNECTION_REFERENCE",
    sourceSelector: "",
    revision: 1,
    repositorySelector: "",
    workspaceMode: "GIT",
    defaultBranch: "main",
    credentialBindingSelector: "",
    definitionRef: "",
    definitionVersion: 1,
    capabilities: [],
    credentialBindingSelectors: [],
    memberActorSelectors: [],
    roleSelectors: [],
    allowedTargetRoleSelectors: [],
    promptProfileSelector: "",
    roleImageRecipeSelector: "",
    repositoryWorkspaceSelectors: [],
    integrationSelectors: [],
    contentSha256: "",
    locale: "ru",
  };
}

export const toWorkspaceIntegrationDefinitionModel = (
  value: IntegrationDefinition,
): WorkspaceIntegrationDefinitionModel => ({
  definitionRef: value.definitionRef,
  version: value.version,
  displayName: value.displayName,
  state: value.state,
  capabilities: value.capabilities.map((item) => ({
    name: item.name,
    risk: item.risk,
    requiresApproval: item.requiresApproval,
  })),
});

export function buildMutableSpec(
  kind: MutableResourceKind,
  draft: WorkspaceResourceDraft,
): ResourceSpecInput {
  if (kind === "CHAT") {
    return {
      chat: {
        stableKey: draft.stableKey.trim(),
        roomType: draft.roomType,
        workPolicy: draft.workPolicy.trim(),
        ...(draft.defaultAgentSelector
          ? { defaultAgentSelector: draft.defaultAgentSelector }
          : {}),
        ...(draft.channelSelector.trim()
          ? { channelSelector: draft.channelSelector.trim() }
          : {}),
      },
    };
  }
  if (kind === "CREDENTIAL_BINDING") {
    return {
      credentialBinding: {
        purpose: draft.purpose.trim(),
        ...(draft.sourceSelector
          ? {
              sourceKind: draft.sourceKind,
              sourceSelector: draft.sourceSelector,
            }
          : {}),
        revision: draft.revision,
      },
    };
  }
  if (kind === "REPOSITORY_WORKSPACE") {
    return {
      repositoryWorkspace: {
        ...(draft.repositorySelector
          ? { repositorySelector: draft.repositorySelector }
          : {}),
        workspaceMode: draft.workspaceMode.trim(),
        defaultBranch: draft.defaultBranch.trim(),
        ...(draft.credentialBindingSelector
          ? { credentialBindingSelector: draft.credentialBindingSelector }
          : {}),
      },
    };
  }
  return {
    integration: {
      definitionRef: draft.definitionRef,
      definitionVersion: draft.definitionVersion,
      capabilities: draft.capabilities,
      credentialBindingSelectors: draft.credentialBindingSelectors,
      ...(draft.sourceSelector ? { sourceSelector: draft.sourceSelector } : {}),
    },
  };
}

export function buildAccessSpec(
  kind: AccessResourceKind,
  draft: WorkspaceResourceDraft,
): AccessSpecInput {
  if (kind === "TEAM") {
    return {
      team: {
        stableKey: draft.stableKey.trim(),
        ...(draft.sourceSelector.trim()
          ? { sourceSelector: draft.sourceSelector.trim() }
          : {}),
        memberActorSelectors: draft.memberActorSelectors,
        roleSelectors: draft.roleSelectors,
      },
    };
  }
  if (kind === "ROLE") {
    return {
      role: {
        stableKey: draft.stableKey.trim(),
        capabilities: draft.capabilities,
        allowedTargetRoleSelectors: draft.allowedTargetRoleSelectors,
        ...(draft.promptProfileSelector
          ? { promptProfileSelector: draft.promptProfileSelector }
          : {}),
        roleImageRecipeSelector: draft.roleImageRecipeSelector,
        providerCredentialBindingSelectors: draft.credentialBindingSelectors,
        repositoryWorkspaceSelectors: draft.repositoryWorkspaceSelectors,
        integrationSelectors: draft.integrationSelectors,
      },
    };
  }
  return {
    promptProfile: {
      revision: draft.revision,
      contentSha256: draft.contentSha256.trim(),
      ...(draft.sourceSelector ? { sourceSelector: draft.sourceSelector } : {}),
      locale: draft.locale.trim(),
    },
  };
}

export function isWorkspaceDraftBounded(
  kind: MutableResourceKind | AccessResourceKind,
  draft: WorkspaceResourceDraft,
): boolean {
  if (kind === "INTEGRATION")
    return (
      draft.capabilities.length <= 32 &&
      draft.credentialBindingSelectors.length <= 32 &&
      draft.capabilities.every((item) => item.length <= 120)
    );
  if (kind === "TEAM")
    return (
      draft.memberActorSelectors.length <= 200 &&
      draft.roleSelectors.length <= 64
    );
  if (kind === "ROLE")
    return (
      draft.capabilities.length <= 64 &&
      draft.allowedTargetRoleSelectors.length <= 64 &&
      draft.credentialBindingSelectors.length <= 32 &&
      draft.repositoryWorkspaceSelectors.length <= 32 &&
      draft.integrationSelectors.length <= 32
    );
  return true;
}

/** UI-проекция исключает private locator и raw DTO из Pinia/component state. */
export function toWorkspaceResourceModel(
  resource: Resource,
): WorkspaceResourceModel {
  const draft = emptyWorkspaceResourceDraft();
  if (resource.spec.chat) {
    draft.stableKey = resource.spec.chat.stableKey;
    draft.roomType = resource.spec.chat.roomType;
    draft.workPolicy = resource.spec.chat.workPolicy;
  }
  if (resource.spec.repositoryWorkspace) {
    draft.workspaceMode = resource.spec.repositoryWorkspace.workspaceMode;
    draft.defaultBranch = resource.spec.repositoryWorkspace.defaultBranch;
  }
  if (resource.spec.integration) {
    draft.definitionRef = resource.spec.integration.definitionRef;
    draft.definitionVersion = resource.spec.integration.definitionVersion;
    draft.capabilities = [...resource.spec.integration.capabilities];
  }
  if (resource.spec.credentialBinding) {
    draft.purpose = resource.spec.credentialBinding.purpose;
    draft.revision = resource.spec.credentialBinding.revision;
  }
  if (resource.spec.team) draft.stableKey = resource.spec.team.stableKey;
  if (resource.spec.role) {
    draft.stableKey = resource.spec.role.stableKey;
    draft.capabilities = [...resource.spec.role.capabilities];
  }
  if (resource.spec.promptProfile) {
    draft.revision = resource.spec.promptProfile.revision;
    draft.contentSha256 = resource.spec.promptProfile.contentSha256;
    draft.locale = resource.spec.promptProfile.locale;
  }
  const ownership = resourceOwnership(resource);
  return {
    id: resource.id,
    selector:
      resource.spec.chat?.stableKey ??
      resource.spec.team?.stableKey ??
      resource.spec.role?.stableKey ??
      resource.name,
    name: resource.name,
    kind: resource.kind as WorkspaceResourceKind,
    state: resource.state as WorkspaceResourceState,
    version: resource.version,
    nextActions: resource.nextActions as WorkspaceResourceAction[],
    ownership: ownership
      ? {
          managedBy: ownership.managedBy,
          source: ownership.source,
          revision: ownership.revision,
          sourceSha256: ownership.sourceSha256,
          drift: ownership.drift,
        }
      : null,
    credential: resource.spec.credentialBinding
      ? {
          purpose: resource.spec.credentialBinding.purpose,
          revision: resource.spec.credentialBinding.revision,
          providerEligible: resource.spec.credentialBinding.providerEligible,
          ...(resource.spec.credentialBinding.expiresAt
            ? { expiresAt: resource.spec.credentialBinding.expiresAt }
            : {}),
        }
      : null,
    draft,
  };
}

export const toWorkspaceSelectorModel = (
  resource: Resource,
): WorkspaceSelectorModel => ({
  selector:
    resource.spec.agent?.stableKey ??
    resource.spec.providerConnectionReference?.stableKey ??
    resource.name,
  name: resource.name,
  kind: resource.kind as WorkspaceSelectorModel["kind"],
  version: resource.version,
});
