import { requestSignal } from "@/shared/api/client";
import {
  addAttachmentSetItems,
  createAttachmentSetDraft,
  createOrganizationAttachmentSetDraft,
  finalizeAttachmentSet,
  getArtifact,
  getAttachmentSet,
  removeAttachmentSetItems,
  uploadArtifact,
  uploadOrganizationArtifact,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  Artifact,
  AttachmentSet,
  AttachmentSetPurpose,
} from "@/shared/api/generated/openapi/types.gen";
import { mutate } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";

const attachmentMutationBatchSize = 100;
const artifactScanIntervalMs = 1_000;
const artifactScanAttempts = 120;

export interface AttachmentUploadRequest {
  idempotencyKey: string;
  signal: AbortSignal;
  onProgress: (progress: { loadedBytes: number; totalBytes: number }) => void;
  onScanning: () => void;
}

function abortError(): DOMException {
  return new DOMException("Attachment operation was cancelled", "AbortError");
}

async function wait(intervalMs: number, signal: AbortSignal): Promise<void> {
  if (signal.aborted) throw abortError();
  await new Promise<void>((resolve, reject) => {
    const handleAbort = () => {
      globalThis.clearTimeout(timeout);
      signal.removeEventListener("abort", handleAbort);
      reject(abortError());
    };
    const timeout = globalThis.setTimeout(() => {
      signal.removeEventListener("abort", handleAbort);
      resolve();
    }, intervalMs);
    signal.addEventListener("abort", handleAbort, { once: true });
  });
}

export async function readAttachmentArtifact(
  artifactRef: string,
  signal?: AbortSignal,
): Promise<Artifact> {
  return (
    await unwrap(
      getArtifact({
        path: { artifactRef },
        signal: requestSignal(signal),
      }),
    )
  ).data;
}

export async function waitForCleanAttachmentArtifact(
  initial: Artifact,
  signal: AbortSignal,
): Promise<Artifact> {
  let artifact = initial;
  for (let attempt = 0; attempt < artifactScanAttempts; attempt += 1) {
    if (signal.aborted) throw abortError();
    if (artifact.lifecycleState === "ACTIVE" && artifact.scanState === "CLEAN")
      return artifact;
    if (
      artifact.lifecycleState !== "ACTIVE" ||
      artifact.scanState === "QUARANTINED" ||
      artifact.scanState === "FAILED"
    )
      throw new Error(`Attachment is not safe to use: ${artifact.scanState}`);

    await wait(artifactScanIntervalMs, signal);
    artifact = await readAttachmentArtifact(artifact.ref, signal);
  }
  throw new Error("Attachment scan did not complete in time");
}

export async function uploadCleanAttachmentArtifact(
  projectRef: string | undefined,
  purpose: AttachmentSetPurpose,
  file: File,
  request: AttachmentUploadRequest,
): Promise<Artifact> {
  assertAttachmentScope(projectRef, purpose);
  const uploaded = (
    await mutate(
      (headers) =>
        projectRef
          ? uploadArtifact({
              path: { projectRef },
              body: file,
              headers: {
                "Idempotency-Key": headers["Idempotency-Key"],
                "X-CSRF-Token": headers["X-CSRF-Token"],
                "X-File-Name": file.name,
              },
              signal: requestSignal(request.signal),
            })
          : uploadOrganizationArtifact({
              body: file,
              headers: {
                "Idempotency-Key": headers["Idempotency-Key"],
                "X-CSRF-Token": headers["X-CSRF-Token"],
                "X-File-Name": file.name,
              },
              signal: requestSignal(request.signal),
            }),
      undefined,
      request.idempotencyKey,
    )
  ).data;
  request.onProgress({ loadedBytes: file.size, totalBytes: file.size });
  if (uploaded.scanState !== "CLEAN") request.onScanning();
  return waitForCleanAttachmentArtifact(uploaded, request.signal);
}

export async function createAttachmentDraft(
  projectRef: string | undefined,
  purpose: AttachmentSetPurpose,
  signal?: AbortSignal,
): Promise<AttachmentSet> {
  assertAttachmentScope(projectRef, purpose);
  return (
    await mutate((headers) =>
      projectRef
        ? createAttachmentSetDraft({
            path: { projectRef },
            body: { purpose },
            headers: {
              "Idempotency-Key": headers["Idempotency-Key"],
              "X-CSRF-Token": headers["X-CSRF-Token"],
            },
            signal: requestSignal(signal),
          })
        : createOrganizationAttachmentSetDraft({
            body: { purpose },
            headers: {
              "Idempotency-Key": headers["Idempotency-Key"],
              "X-CSRF-Token": headers["X-CSRF-Token"],
            },
            signal: requestSignal(signal),
          }),
    )
  ).data;
}

export function assertAttachmentScope(
  projectRef: string | undefined,
  purpose: AttachmentSetPurpose,
): void {
  if (!isAttachmentScopeAvailable(projectRef, purpose))
    throw new Error("AttachmentSet purpose requires a Project context");
}

export function isAttachmentScopeAvailable(
  projectRef: string | undefined,
  purpose: AttachmentSetPurpose,
): boolean {
  return Boolean(projectRef) || purpose === "ASSISTANT_MESSAGE";
}

export async function readAttachmentSet(
  attachmentSetRef: string,
  signal?: AbortSignal,
): Promise<AttachmentSet> {
  return (
    await unwrap(
      getAttachmentSet({
        path: { attachmentSetRef },
        query: { pageSize: attachmentMutationBatchSize },
        signal: requestSignal(signal),
      }),
    )
  ).data.attachmentSet;
}

export async function addAttachmentItems(
  draft: AttachmentSet,
  artifactRefs: string[],
  insertAfterPosition: number,
  signal?: AbortSignal,
): Promise<AttachmentSet> {
  return (
    await mutate(
      (headers) =>
        addAttachmentSetItems({
          path: { attachmentSetRef: draft.ref },
          body: { artifactRefs, insertAfterPosition },
          headers: {
            "Idempotency-Key": headers["Idempotency-Key"],
            "X-CSRF-Token": headers["X-CSRF-Token"],
            "If-Match": headers["If-Match"] ?? "",
          },
          signal: requestSignal(signal),
        }),
      draft.version,
    )
  ).data;
}

export async function removeAttachmentItems(
  draft: AttachmentSet,
  artifactRefs: string[],
  signal?: AbortSignal,
): Promise<AttachmentSet> {
  return (
    await mutate(
      (headers) =>
        removeAttachmentSetItems({
          path: { attachmentSetRef: draft.ref },
          body: { artifactRefs },
          headers: {
            "Idempotency-Key": headers["Idempotency-Key"],
            "X-CSRF-Token": headers["X-CSRF-Token"],
            "If-Match": headers["If-Match"] ?? "",
          },
          signal: requestSignal(signal),
        }),
      draft.version,
    )
  ).data;
}

export async function finalizeAttachmentDraft(
  draft: AttachmentSet,
  signal?: AbortSignal,
): Promise<AttachmentSet> {
  return (
    await mutate(
      (headers) =>
        finalizeAttachmentSet({
          path: { attachmentSetRef: draft.ref },
          headers: {
            "Idempotency-Key": headers["Idempotency-Key"],
            "X-CSRF-Token": headers["X-CSRF-Token"],
            "If-Match": headers["If-Match"] ?? "",
          },
          signal: requestSignal(signal),
        }),
      draft.version,
    )
  ).data;
}

export function attachmentMutationBatches(
  references: readonly string[],
): string[][] {
  const batches: string[][] = [];
  for (
    let offset = 0;
    offset < references.length;
    offset += attachmentMutationBatchSize
  )
    batches.push(
      references.slice(offset, offset + attachmentMutationBatchSize),
    );
  return batches;
}
