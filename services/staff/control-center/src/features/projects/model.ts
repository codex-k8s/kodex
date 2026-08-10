import type { Resource } from "@/shared/api/generated/openapi/types.gen";

export interface ProjectModel {
  id: string;
  name: string;
  state: string;
  version: number;
  slug: string;
  description: string;
  locale: string;
  nextActions: string[];
}

export const toProjectModel = (value: Resource): ProjectModel => ({
  id: value.id,
  name: value.name,
  state: value.state,
  version: value.version,
  slug: value.spec.project?.slug ?? "",
  description: value.spec.project?.description ?? "",
  locale: value.spec.project?.locale ?? "ru",
  nextActions: [...value.nextActions],
});
