import { requestSignal } from "@/shared/api/client";
import { listModelDiscovery } from "@/shared/api/generated/openapi/sdk.gen";
import type { ModelDiscoveryPage } from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";
import {
  defaultModelDiscoveryFilters,
  type ModelDiscoveryFilters,
} from "./model-discovery-filters";

export async function loadModelDiscovery(
  query: string,
  pageToken: string,
  signal: AbortSignal,
  filters: ModelDiscoveryFilters = defaultModelDiscoveryFilters(),
): Promise<ModelDiscoveryPage> {
  const minimumContextTokens = filters.minimumContextTokens;
  if (
    !Number.isSafeInteger(minimumContextTokens) ||
    minimumContextTokens < 0 ||
    minimumContextTokens > 1_000_000_000_000
  )
    throw new Error("Model discovery context filter is invalid");
  return (
    await unwrap(
      listModelDiscovery({
        query: {
          pageSize: 20,
          ...(query.trim() ? { query: query.trim() } : {}),
          ...(pageToken ? { pageToken } : {}),
          ...(filters.providerId.trim()
            ? { providerId: filters.providerId.trim() }
            : {}),
          ...(filters.family.trim() ? { family: filters.family.trim() } : {}),
          ...(minimumContextTokens > 0 ? { minimumContextTokens } : {}),
          ...(filters.inputModality.trim()
            ? { inputModality: filters.inputModality.trim() }
            : {}),
          ...(filters.outputModality.trim()
            ? { outputModality: filters.outputModality.trim() }
            : {}),
          ...(filters.tools ? { tools: filters.tools } : {}),
          ...(filters.reasoning ? { reasoning: filters.reasoning } : {}),
          ...(filters.cache ? { cache: filters.cache } : {}),
          groupBy: filters.groupBy,
        },
        signal: requestSignal(signal),
      }),
    )
  ).data;
}
