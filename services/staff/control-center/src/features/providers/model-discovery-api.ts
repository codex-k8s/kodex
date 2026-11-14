import { requestSignal } from "@/shared/api/client";
import { listModelDiscovery } from "@/shared/api/generated/openapi/sdk.gen";
import type { ModelDiscoveryPage } from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";

export async function loadModelDiscovery(
  query: string,
  pageToken: string,
  signal: AbortSignal,
): Promise<ModelDiscoveryPage> {
  return (
    await unwrap(
      listModelDiscovery({
        query: {
          pageSize: 20,
          ...(query.trim() ? { query: query.trim() } : {}),
          ...(pageToken ? { pageToken } : {}),
        },
        signal: requestSignal(signal),
      }),
    )
  ).data;
}
