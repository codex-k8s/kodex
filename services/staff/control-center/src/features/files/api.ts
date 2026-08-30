import { requestSignal } from "@/shared/api/client";
import {
  deleteArtifact,
  listArtifacts,
  purgeArtifact,
  restoreArtifact,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  Artifact,
  ArtifactPurgeReceipt,
} from "@/shared/api/generated/openapi/types.gen";
import { mutate, type MutationHeaders } from "@/shared/api/mutation";
import { asProblem, unwrap, type AppProblem } from "@/shared/api/problem";
import type {
  AsyncEntityLoadRequest,
  AsyncEntityPage,
  AsyncEntityPickerItem,
} from "@/shared/ui/async-entity-picker";

export interface ArtifactListItem extends AsyncEntityPickerItem {
  artifact: Artifact;
}

export interface ArtifactBulkReceipt {
  artifact: Artifact;
  problem?: AppProblem;
  status: "SUCCEEDED" | "FAILED";
}

export async function mutateArtifactsSequentially(
  artifacts: readonly Artifact[],
  command: (artifact: Artifact) => Promise<unknown>,
): Promise<ArtifactBulkReceipt[]> {
  const receipts: ArtifactBulkReceipt[] = [];
  for (const artifact of artifacts) {
    try {
      await command(artifact);
      receipts.push({ artifact, status: "SUCCEEDED" });
    } catch (error) {
      receipts.push({ artifact, problem: asProblem(error), status: "FAILED" });
    }
  }
  return receipts;
}

export async function loadArtifactPage(
  projectRef: string,
  request: AsyncEntityLoadRequest,
  lifecycleState: Artifact["lifecycleState"] = "ACTIVE",
): Promise<AsyncEntityPage<ArtifactListItem>> {
  const query = request.query.trim();
  const result = await unwrap(
    listArtifacts({
      path: { projectRef },
      query: {
        lifecycleState,
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

function versionedHeaders(headers: MutationHeaders): {
  "Idempotency-Key": string;
  "If-Match": string;
  "X-CSRF-Token": string;
} {
  if (!headers["If-Match"])
    throw new Error("Artifact version header is unavailable");
  return {
    "Idempotency-Key": headers["Idempotency-Key"],
    "If-Match": headers["If-Match"],
    "X-CSRF-Token": headers["X-CSRF-Token"],
  };
}

export async function deleteArtifactItem(
  artifact: Artifact,
): Promise<Artifact> {
  return (
    await mutate(
      (headers) =>
        deleteArtifact({
          path: { artifactRef: artifact.ref },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      artifact.version,
    )
  ).data;
}

export async function restoreArtifactItem(
  artifact: Artifact,
): Promise<Artifact> {
  return (
    await mutate(
      (headers) =>
        restoreArtifact({
          path: { artifactRef: artifact.ref },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      artifact.version,
    )
  ).data;
}

export async function purgeArtifactItem(
  artifact: Artifact,
): Promise<ArtifactPurgeReceipt> {
  return (
    await mutate(
      (headers) =>
        purgeArtifact({
          path: { artifactRef: artifact.ref },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      artifact.version,
    )
  ).data;
}
