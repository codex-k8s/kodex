import { listArtifacts } from "@/shared/api/generated/openapi/sdk.gen";
import type { Artifact } from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";
import type {
  AsyncEntityLoadRequest,
  AsyncEntityPage,
  AsyncEntityPickerItem,
} from "@/shared/ui/async-entity-picker";

export interface ArtifactListItem extends AsyncEntityPickerItem {
  artifact: Artifact;
}

export async function loadArtifactPage(
  projectRef: string,
  request: AsyncEntityLoadRequest,
): Promise<AsyncEntityPage<ArtifactListItem>> {
  const query = request.query.trim();
  const result = await unwrap(
    listArtifacts({
      path: { projectRef },
      query: {
        pageSize: 40,
        ...(query ? { query } : {}),
        ...(request.cursor ? { pageToken: request.cursor } : {}),
      },
      signal: request.signal,
    }),
  );
  return {
    items: result.data.items.map((artifact) => ({
      artifact,
      description: artifact.mediaType,
      id: artifact.ref,
      label: artifact.fileName,
    })),
    nextCursor: result.data.nextPageToken,
  };
}
