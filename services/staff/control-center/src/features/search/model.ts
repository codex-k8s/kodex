import type {
  LifecycleState,
  Resource,
  ResourceKind,
} from "@/shared/api/generated/openapi/types.gen";

export interface SearchResult {
  key: string;
  name: string;
  kind: ResourceKind;
  state: LifecycleState;
  version: number;
}

/** Search state не удерживает Resource.spec с private/internal projections. */
export function toSearchResult(resource: Resource): SearchResult {
  return {
    key: `${resource.kind}:${resource.name}:${String(resource.version)}`,
    name: resource.name,
    kind: resource.kind,
    state: resource.state,
    version: resource.version,
  };
}
