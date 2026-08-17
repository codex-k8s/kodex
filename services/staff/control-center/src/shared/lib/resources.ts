import type {
  ConfigurationOwnershipProjection,
  Resource,
  ResourceKind,
} from "@/shared/api/generated/openapi/types.gen";

const resourceKindRegistry: Record<ResourceKind, true> = {
  PROJECT: true,
  TEAM: true,
  CHAT: true,
  ROLE: true,
  PROMPT_PROFILE: true,
  CREDENTIAL_BINDING: true,
  REPOSITORY_WORKSPACE: true,
  INTEGRATION: true,
  RUNTIME_REVISION: true,
  SESSION: true,
  TURN: true,
  PROCESS_RUN: true,
  SCHEDULE: true,
  OWNER_GATE: true,
  MEMORY_RECORD: true,
  WORK_CLAIM: true,
  ARTIFACT: true,
  ROLE_IMAGE_RECIPE: true,
  IMAGE_BUILD: true,
  IMAGE_ARTIFACT: true,
  ROLE_DEFINITION: true,
  AGENT: true,
  AGENT_ASSIGNMENT: true,
  INSTRUCTION_SET: true,
  PROVIDER_CONNECTION_REFERENCE: true,
  PROVIDER_POOL: true,
  WORKSPACE_BACKUP: true,
  WORKSPACE_RESTORE: true,
  WORKSPACE_MATTERMOST_MAPPING: true,
};

export const resourceKinds = Object.freeze(
  Object.keys(resourceKindRegistry) as ResourceKind[],
);

// PROJECT принадлежит global owner catalog. Project-scoped generic ListResources
// намеренно отклоняет этот kind, поэтому realtime snapshot запрашивает только
// ресурсы внутри уже выбранного Project.
export const projectResourceKinds = Object.freeze(
  resourceKinds.filter((kind) => kind !== "PROJECT"),
);

export function resourceOwnership(
  resource: Resource,
): ConfigurationOwnershipProjection | undefined {
  const values: unknown[] = Object.values(resource.spec);
  for (const value of values) {
    if (typeof value === "object" && value !== null && "ownership" in value) {
      return (value as { ownership?: ConfigurationOwnershipProjection })
        .ownership;
    }
  }
  return undefined;
}

export function projectDescription(resource: Resource): string {
  return resource.spec.project?.description ?? "";
}

export function projectSlug(resource: Resource): string {
  return resource.spec.project?.slug ?? "";
}
