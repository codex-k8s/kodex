import type {
  ConfigurationOwnershipProjection,
  Resource,
} from "@/shared/api/generated/openapi/types.gen";

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
