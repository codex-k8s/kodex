import { requestSignal } from "@/shared/api/client";
import { listArtifacts } from "@/shared/api/generated/openapi/sdk.gen";
import type { Artifact } from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";
import type {
  AsyncEntityLoader,
  AsyncEntityPage,
  AsyncEntityPickerItem,
} from "@/shared/ui/async-entity-picker";

const artifactPageSize = 40;

export interface AttachmentArtifactPickerItem extends AsyncEntityPickerItem {
  artifact: Artifact;
}

function combinedSignal(signal: AbortSignal): AbortSignal {
  return AbortSignal.any([signal, requestSignal()]);
}

function optionalQuery(query: string): string | undefined {
  const value = query.trim();
  return value || undefined;
}

function isAttachable(artifact: Artifact): boolean {
  return artifact.lifecycleState === "ACTIVE" && artifact.scanState === "CLEAN";
}

function toPickerItem(artifact: Artifact): AttachmentArtifactPickerItem {
  return {
    artifact,
    description: artifact.mediaType,
    id: artifact.ref,
    label: artifact.fileName,
  };
}

export function createAttachmentArtifactLoader(
  projectRef: string,
): AsyncEntityLoader<AttachmentArtifactPickerItem> {
  return async ({ cursor, query, signal }) => {
    const visitedPageTokens = new Set(cursor ? [cursor] : []);
    const searchQuery = optionalQuery(query);

    async function loadAttachablePage(
      pageToken?: string,
    ): Promise<AsyncEntityPage<AttachmentArtifactPickerItem>> {
      const response = await unwrap(
        listArtifacts({
          path: { projectRef },
          query: {
            lifecycleState: "ACTIVE",
            pageSize: artifactPageSize,
            ...(pageToken ? { pageToken } : {}),
            ...(searchQuery ? { query: searchQuery } : {}),
          },
          signal: combinedSignal(signal),
        }),
      );
      const items = response.data.items.filter(isAttachable).map(toPickerItem);
      const candidateCursor = response.data.nextPageToken || null;
      const nextCursor =
        candidateCursor && !visitedPageTokens.has(candidateCursor)
          ? candidateCursor
          : null;
      if (items.length > 0 || nextCursor === null) {
        return { items, nextCursor };
      }
      visitedPageTokens.add(nextCursor);
      return loadAttachablePage(nextCursor);
    }

    return loadAttachablePage(cursor);
  };
}
