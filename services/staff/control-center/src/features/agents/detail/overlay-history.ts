import {
  getConfigOverlayRevision,
  listConfigOverlayRevisions,
  rollbackConfigOverlay,
} from "@/shared/api/generated/openapi/sdk.gen";
import type { ConfigOverlayVersion } from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { etag, mutateWithRetry } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";

function publishedRevision(
  revision: ConfigOverlayVersion,
): ConfigOverlayVersion {
  if (
    !revision.ref ||
    !Number.isSafeInteger(revision.revision) ||
    revision.revision < 1 ||
    !["PUBLISHED", "SUPERSEDED"].includes(revision.state) ||
    !/^[a-f0-9]{64}$/.test(revision.digest)
  )
    throw new Error("Invalid published overlay revision");
  return revision;
}

export async function loadOverlayHistory(
  agentRef: string,
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
) {
  const page = (
    await unwrap(
      listConfigOverlayRevisions({
        path: { agentRef },
        query: { query, pageToken, pageSize: 30 },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  signal.throwIfAborted();
  if (
    !Number.isSafeInteger(page.total) ||
    page.total < page.items.length ||
    new Set(page.items.map((item) => item.ref)).size !== page.items.length
  )
    throw new Error("Invalid overlay history page");
  page.items.forEach(publishedRevision);
  return page;
}

export async function readOverlayRevision(
  agentRef: string,
  revisionRef: string,
  signal: AbortSignal,
): Promise<ConfigOverlayVersion> {
  const revision = (
    await unwrap(
      getConfigOverlayRevision({
        path: { agentRef, revisionRef },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  signal.throwIfAborted();
  if (revision.ref !== revisionRef)
    throw new Error("Overlay revision readback mismatch");
  return publishedRevision(revision);
}

export async function restoreOverlayRevision(
  agentRef: string,
  revisionRef: string,
  agentVersion: number,
) {
  return (
    await mutateWithRetry(
      (headers) =>
        rollbackConfigOverlay({
          path: { agentRef },
          body: { publishedOverlayRef: revisionRef },
          headers: { ...headers, "If-Match": etag(agentVersion) },
          signal: requestSignal(),
        }),
      agentVersion,
    )
  ).data;
}
