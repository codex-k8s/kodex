import { requestSignal } from "@/shared/api/client";
import { listIntegrationDefinitions } from "@/shared/api/generated/openapi/sdk.gen";
import type { IntegrationDefinition } from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";

export async function loadExactIntegrationDefinition(
  key: string,
  signal: AbortSignal,
): Promise<IntegrationDefinition> {
  if (!/^[a-z][a-z0-9._-]{0,127}$/.test(key))
    throw new Error("Integration definition key is invalid");
  const seen = new Set<string>();
  let cursor: string | undefined;
  for (let pageNumber = 0; pageNumber < 10; pageNumber += 1) {
    const page = (
      await unwrap(
        listIntegrationDefinitions({
          query: {
            query: key,
            pageSize: 100,
            ...(cursor ? { pageToken: cursor } : {}),
          },
          signal: requestSignal(signal),
        }),
      )
    ).data;
    const exact = page.items.find((item) => item.key === key);
    if (exact) return exact;
    cursor = page.nextPageToken || undefined;
    if (!cursor) break;
    if (seen.has(cursor))
      throw new Error("Integration definition cursor repeated");
    seen.add(cursor);
  }
  throw new Error("Integration definition is unavailable");
}
