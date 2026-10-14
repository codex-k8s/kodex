import { requestSignal } from "./client";
import {
  downloadArtifactRevision,
  getArtifact,
  getArtifactRevision,
  listArtifactRevisions,
} from "./generated/openapi/sdk.gen";
import type {
  Artifact,
  ArtifactRevision,
  ArtifactRevisionPage,
} from "./generated/openapi/types.gen";
import { unwrap } from "./problem";
import { readWithRetry } from "./read-retry";

export function assertArtifactRevision(
  value: ArtifactRevision,
  artifactRef: string,
  revisionRef?: string,
): ArtifactRevision {
  if (
    !value.ref ||
    value.artifactRef !== artifactRef ||
    (revisionRef !== undefined && value.ref !== revisionRef) ||
    !Number.isSafeInteger(value.revision) ||
    value.revision < 1 ||
    !Number.isSafeInteger(value.sizeBytes) ||
    value.sizeBytes < 0 ||
    value.sizeBytes > 512 << 20 ||
    !/^sha256:[a-f0-9]{64}$/u.test(value.digest) ||
    !["PENDING", "SCANNING", "CLEAN", "QUARANTINED", "FAILED"].includes(
      value.scanState,
    ) ||
    ![
      "CONTROL_CENTER",
      "AGENT_RESULT",
      "INTEGRATION_RESULT",
      "KNOWLEDGE_SOURCE",
      "INTERACTION_ATTACHMENT",
    ].includes(value.source) ||
    typeof value.previewAvailable !== "boolean" ||
    !value.fileName ||
    value.fileName.length > 255 ||
    !value.mediaType ||
    value.mediaType.length > 160 ||
    !Number.isFinite(Date.parse(value.createdAt))
  )
    throw new Error("Invalid artifact revision readback");
  return value;
}

export async function readProjectArtifact(
  projectRef: string,
  artifactRef: string,
  signal: AbortSignal,
): Promise<Artifact> {
  const artifact = await readWithRetry(
    async () =>
      (
        await unwrap(
          getArtifact({
            path: { artifactRef },
            signal: requestSignal(signal),
          }),
        )
      ).data,
    undefined,
    signal,
  );
  if (artifact.ref !== artifactRef || artifact.projectRef !== projectRef)
    throw new Error("Artifact project readback mismatch");
  return artifact;
}

export async function readArtifactRevision(
  artifactRef: string,
  revisionRef: string,
  signal: AbortSignal,
): Promise<ArtifactRevision> {
  const value = await readWithRetry(
    async () =>
      (
        await unwrap(
          getArtifactRevision({
            path: { artifactRef, revisionRef },
            signal: requestSignal(signal),
          }),
        )
      ).data,
    undefined,
    signal,
  );
  return assertArtifactRevision(value, artifactRef, revisionRef);
}

export async function loadArtifactRevisionPage(
  artifactRef: string,
  pageToken: string | undefined,
  signal: AbortSignal,
): Promise<ArtifactRevisionPage> {
  const page = await readWithRetry(
    async () =>
      (
        await unwrap(
          listArtifactRevisions({
            path: { artifactRef },
            query: { pageSize: 5, ...(pageToken ? { pageToken } : {}) },
            signal: requestSignal(signal),
          }),
        )
      ).data,
    undefined,
    signal,
  );
  if (
    !Array.isArray(page.items) ||
    page.items.length > 5 ||
    !Number.isSafeInteger(page.total) ||
    page.total < page.items.length ||
    new Set(page.items.map((item) => item.ref)).size !== page.items.length ||
    new Set(page.items.map((item) => item.revision)).size !==
      page.items.length ||
    page.items.some((item, index) => {
      const previous = page.items[index - 1];
      return index > 0 && (!previous || item.revision >= previous.revision);
    }) ||
    (page.nextPageToken !== undefined &&
      (typeof page.nextPageToken !== "string" || !page.nextPageToken))
  )
    throw new Error("Invalid artifact revision page");
  page.items.forEach((item) => assertArtifactRevision(item, artifactRef));
  return page;
}

export async function downloadRevisionContent(
  revision: ArtifactRevision,
  purpose: "DOWNLOAD" | "PREVIEW",
  signal: AbortSignal,
): Promise<Blob> {
  assertArtifactRevision(revision, revision.artifactRef, revision.ref);
  if (revision.scanState !== "CLEAN")
    throw new Error("Artifact revision content requires clean scan state");
  const body = await readWithRetry(
    async () =>
      (
        await unwrap(
          downloadArtifactRevision({
            path: {
              artifactRef: revision.artifactRef,
              revisionRef: revision.ref,
            },
            query: { purpose },
            parseAs: "blob",
            signal: requestSignal(signal),
          }),
        )
      ).data,
    undefined,
    signal,
  );
  if (!(body instanceof Blob) || body.size !== revision.sizeBytes)
    throw new Error("Artifact revision content readback mismatch");
  return body;
}
