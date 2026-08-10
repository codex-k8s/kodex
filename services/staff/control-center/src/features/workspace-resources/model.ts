import type {
  AccessResourceKind,
  AccessSpecInput,
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
  id: string;
  name: string;
  kind: "AGENT" | "ROLE_IMAGE_RECIPE";
  version: number;
}

export interface WorkspaceResourceDraft {
  stableKey: string;
  roomType: "USER" | "COORDINATION" | "WORK_CONTROL" | "RUNS";
  workPolicy: string;
  defaultAgentId: string;
  externalChannelRef: string;
  purpose: string;
  immutableSecretRef: string;
  principalRef: string;
  revision: number;
  repositoryRef: string;
  workspaceMode: string;
  defaultBranch: string;
  credentialBindingId: string;
  definitionRef: string;
  definitionVersion: number;
  capabilities: string[];
  credentialBindingIds: string[];
  endpointRef: string;
  externalTeamRef: string;
  memberActorIds: string[];
  roleIds: string[];
  allowedTargetRoleIds: string[];
  promptProfileId: string;
  roleImageRecipeId: string;
  repositoryWorkspaceIds: string[];
  integrationIds: string[];
  contentSha256: string;
  sourceRef: string;
  locale: string;
}

export function emptyWorkspaceResourceDraft(): WorkspaceResourceDraft {
  return {
    stableKey: "",
    roomType: "USER",
    workPolicy: "default",
    defaultAgentId: "",
    externalChannelRef: "",
    purpose: "",
    immutableSecretRef: "",
    principalRef: "",
    revision: 1,
    repositoryRef: "",
    workspaceMode: "ISOLATED_WORKTREE",
    defaultBranch: "main",
    credentialBindingId: "",
    definitionRef: "",
    definitionVersion: 1,
    capabilities: [],
    credentialBindingIds: [],
    endpointRef: "",
    externalTeamRef: "",
    memberActorIds: [],
    roleIds: [],
    allowedTargetRoleIds: [],
    promptProfileId: "",
    roleImageRecipeId: "",
    repositoryWorkspaceIds: [],
    integrationIds: [],
    contentSha256: "",
    sourceRef: "",
    locale: "ru",
  };
}

export function buildMutableSpec(
  kind: MutableResourceKind,
  draft: WorkspaceResourceDraft,
  preserved: { credentialLocator: string; credentialPrincipal: string },
): ResourceSpecInput {
  if (kind === "CHAT") {
    return {
      chat: {
        stableKey: draft.stableKey.trim(),
        roomType: draft.roomType,
        workPolicy: draft.workPolicy.trim(),
        ...(draft.defaultAgentId
          ? { defaultAgentId: draft.defaultAgentId }
          : {}),
        ...(draft.externalChannelRef.trim()
          ? { externalChannelRef: draft.externalChannelRef.trim() }
          : {}),
      },
    };
  }
  if (kind === "CREDENTIAL_BINDING") {
    return {
      credentialBinding: {
        purpose: draft.purpose.trim(),
        immutableSecretRef:
          draft.immutableSecretRef.trim() || preserved.credentialLocator,
        principalRef:
          draft.principalRef.trim() || preserved.credentialPrincipal,
        revision: draft.revision,
      },
    };
  }
  if (kind === "REPOSITORY_WORKSPACE") {
    return {
      repositoryWorkspace: {
        repositoryRef: draft.repositoryRef.trim(),
        workspaceMode: draft.workspaceMode.trim(),
        defaultBranch: draft.defaultBranch.trim(),
        ...(draft.credentialBindingId
          ? { credentialBindingId: draft.credentialBindingId }
          : {}),
      },
    };
  }
  return {
    integration: {
      definitionRef: draft.definitionRef,
      definitionVersion: draft.definitionVersion,
      capabilities: draft.capabilities,
      credentialBindingIds: draft.credentialBindingIds,
      endpointRef: draft.endpointRef.trim(),
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
        ...(draft.externalTeamRef.trim()
          ? { externalTeamRef: draft.externalTeamRef.trim() }
          : {}),
        memberActorIds: draft.memberActorIds,
        roleIds: draft.roleIds,
      },
    };
  }
  if (kind === "ROLE") {
    return {
      role: {
        stableKey: draft.stableKey.trim(),
        capabilities: draft.capabilities,
        allowedTargetRoleIds: draft.allowedTargetRoleIds,
        ...(draft.promptProfileId
          ? { promptProfileId: draft.promptProfileId }
          : {}),
        roleImageRecipeId: draft.roleImageRecipeId,
        providerCredentialBindingIds: draft.credentialBindingIds,
        repositoryWorkspaceIds: draft.repositoryWorkspaceIds,
        integrationIds: draft.integrationIds,
      },
    };
  }
  return {
    promptProfile: {
      revision: draft.revision,
      contentSha256: draft.contentSha256.trim(),
      sourceRef: draft.sourceRef.trim(),
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
      draft.credentialBindingIds.length <= 32 &&
      draft.capabilities.every((item) => item.length <= 120)
    );
  if (kind === "TEAM")
    return draft.memberActorIds.length <= 200 && draft.roleIds.length <= 64;
  if (kind === "ROLE")
    return (
      draft.capabilities.length <= 64 &&
      draft.allowedTargetRoleIds.length <= 64 &&
      draft.credentialBindingIds.length <= 32 &&
      draft.repositoryWorkspaceIds.length <= 32 &&
      draft.integrationIds.length <= 32
    );
  return true;
}

/** UI-проекция исключает private locator и raw DTO из Pinia/component state. */
export function toWorkspaceResourceModel(
  resource: Resource,
): WorkspaceResourceModel {
  const draft = emptyWorkspaceResourceDraft();
  if (resource.spec.chat) Object.assign(draft, resource.spec.chat);
  if (resource.spec.repositoryWorkspace) {
    Object.assign(draft, resource.spec.repositoryWorkspace);
    draft.repositoryRef = "";
  }
  if (resource.spec.integration) {
    Object.assign(draft, resource.spec.integration);
    draft.endpointRef = "";
  }
  if (resource.spec.credentialBinding) {
    draft.purpose = resource.spec.credentialBinding.purpose;
    draft.revision = resource.spec.credentialBinding.revision;
  }
  if (resource.spec.team) Object.assign(draft, resource.spec.team);
  if (resource.spec.role) Object.assign(draft, resource.spec.role);
  if (resource.spec.promptProfile)
    Object.assign(draft, resource.spec.promptProfile);
  const ownership = resourceOwnership(resource);
  return {
    id: resource.id,
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
  id: resource.id,
  name: resource.name,
  kind: resource.kind as WorkspaceSelectorModel["kind"],
  version: resource.version,
});
