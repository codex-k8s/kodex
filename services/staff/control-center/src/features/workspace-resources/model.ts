import type {
  AccessResourceKind,
  AccessSpecInput,
  MutableResourceKind,
  Resource,
  ResourceSpecInput,
} from "@/shared/api/generated/openapi/types.gen";

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

/** UI-проекция исключает private locator из сериализуемого Pinia state. */
export function toWorkspaceResourceModel(resource: Resource): Resource {
  return {
    ...resource,
    spec: {
      ...resource.spec,
      ...(resource.spec.credentialBinding
        ? {
            credentialBinding: {
              ...resource.spec.credentialBinding,
              immutableSecretRef: "",
              principalRef: "",
            },
          }
        : {}),
      ...(resource.spec.repositoryWorkspace
        ? {
            repositoryWorkspace: {
              ...resource.spec.repositoryWorkspace,
              repositoryRef: "",
            },
          }
        : {}),
      ...(resource.spec.integration
        ? {
            integration: {
              ...resource.spec.integration,
              endpointRef: "",
            },
          }
        : {}),
    },
  };
}
