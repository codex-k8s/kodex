import { getOwnerGateRepositoryBundle } from "@/shared/api/generated/openapi/sdk.gen";
import type { RepositoryBundleManifestPage } from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";
import type { BundlePageRequest } from "./repository-bundle-manifest";

// Единственная transport boundary feature: компонент управляет состоянием,
// а exact owner Gate/cursor mapping и HTTP decoding остаются в API adapter.
export async function readRepositoryBundlePage(
  gateRef: string,
  page: BundlePageRequest,
  signal: AbortSignal,
): Promise<RepositoryBundleManifestPage> {
  return (await unwrap(getOwnerGateRepositoryBundle({
    path: { gateRef }, query: { ...page, limit: 16 }, signal: requestSignal(signal), cache: "no-store",
  }))).data;
}
