import type {
  ModelDiscoveryModel,
  ModelDiscoveryPage,
} from "@/shared/api/generated/openapi/types.gen";

export interface ModelDiscoveryFilters {
  providerId: string;
  family: string;
  minimumContextTokens: number;
  inputModality: string;
  outputModality: string;
  tools: ModelDiscoveryModel["tools"] | "";
  reasoning: ModelDiscoveryModel["reasoning"] | "";
  cache: ModelDiscoveryModel["cache"] | "";
  groupBy: ModelDiscoveryPage["groupBy"];
}

export function defaultModelDiscoveryFilters(): ModelDiscoveryFilters {
  return {
    providerId: "",
    family: "",
    minimumContextTokens: 0,
    inputModality: "",
    outputModality: "",
    tools: "",
    reasoning: "",
    cache: "",
    groupBy: "PROVIDER",
  };
}
