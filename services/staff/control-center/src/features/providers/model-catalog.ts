import { requestSignal } from "@/shared/api/client";
import { listModelCapabilities } from "@/shared/api/generated/openapi/sdk.gen";
import type {
  ModelCapability,
  ModelCapabilityPage,
} from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";

export type ModelCatalogSnapshot = Pick<
  ModelCapabilityPage,
  "catalogRevision" | "catalogDigest"
>;

export async function loadModelCatalog(
  providerDefinitionKey: string,
  providerAccountRef: string | undefined,
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
  snapshot?: ModelCatalogSnapshot,
): Promise<ModelCapabilityPage> {
  if (cursor && !snapshot)
    throw new Error("Model catalog cursor requires a pinned snapshot");
  const page = (
    await unwrap(
      listModelCapabilities({
        query: {
          providerDefinitionKey,
          ...(providerAccountRef ? { providerAccountRef } : {}),
          ...(query.trim() ? { query: query.trim() } : {}),
          ...(cursor ? { pageToken: cursor } : {}),
          ...(snapshot
            ? {
                expectedCatalogRevision: snapshot.catalogRevision,
                expectedCatalogDigest: snapshot.catalogDigest,
              }
            : {}),
          pageSize: 40,
        },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  if (
    !/^[a-f0-9]{64}$/.test(page.catalogDigest) ||
    page.catalogRevision !== `mcat_${page.catalogDigest}` ||
    (snapshot &&
      (page.catalogRevision !== snapshot.catalogRevision ||
        page.catalogDigest !== snapshot.catalogDigest))
  )
    throw new Error("Model catalog snapshot mismatch");
  if (
    page.items.some(
      (item) => item.providerDefinitionKey !== providerDefinitionKey,
    )
  )
    throw new Error("Model catalog scope mismatch");
  return page;
}

export async function resolveAccountModel(
  providerDefinitionKey: string,
  providerAccountRef: string,
  modelId: string,
  signal: AbortSignal,
): Promise<ModelCapability | undefined> {
  let cursor: string | undefined;
  let snapshot: ModelCatalogSnapshot | undefined;
  const seen = new Set<string>();
  for (let count = 0; count < 30; count += 1) {
    signal.throwIfAborted();
    const page = await loadModelCatalog(
      providerDefinitionKey,
      providerAccountRef,
      modelId,
      cursor,
      signal,
      snapshot,
    );
    signal.throwIfAborted();
    snapshot = page;
    const model = page.items.find((item) => item.id === modelId);
    if (model) return model;
    if (!page.nextPageToken) return undefined;
    if (seen.has(page.nextPageToken))
      throw new Error("Model catalog cursor repeated");
    seen.add(page.nextPageToken);
    cursor = page.nextPageToken;
  }
  throw new Error("Model catalog lookup page limit exceeded");
}

export function accountModelAvailable(
  model: ModelCapability | undefined,
  accountRef: string,
): boolean {
  return (
    !!model?.available &&
    model.readinessBlockers.length === 0 &&
    model.eligibleProviderAccountRefs.includes(accountRef)
  );
}
