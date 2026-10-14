import type {
  AssistantPlan,
  AssistantPlanOperation,
  ArtifactRevision,
} from "@/shared/api/generated/openapi/types.gen";
import { assertArtifactRevision } from "@/shared/api/artifact-revisions";

export function isProjectFileOperation(type: string): boolean {
  return (
    type === "CREATE_PROJECT_FILE" || type === "CREATE_PROJECT_FILE_REVISION"
  );
}

export function projectFileRevisionSource(
  operation: AssistantPlanOperation,
): { artifactRef: string; revisionRef: string; revision: number } | undefined {
  const { target, before, after, parameters } = operation;
  const replacing = Object.hasOwn(parameters, "content");
  if (
    !["text/plain", "text/markdown", "text/csv", "application/json"].includes(
      String(parameters.mediaType),
    ) ||
    parameters.contentEncoding !== "UTF8" ||
    (replacing
      ? typeof parameters.content !== "string" ||
        parameters.content.includes("\0") ||
        new TextEncoder().encode(parameters.content).length > 1 << 20 ||
        ["contentRef", "digest", "sizeBytes"].some((key) =>
          Object.hasOwn(parameters, key),
        )
      : typeof parameters.contentRef !== "string" ||
        !parameters.contentRef ||
        parameters.contentRef !== after.contentRef ||
        parameters.digest !== after.digest ||
        parameters.sizeBytes !== after.sizeBytes ||
        parameters.mediaType !== after.mediaType)
  )
    return;
  if (
    operation.type !== "CREATE_PROJECT_FILE_REVISION" ||
    operation.action !== "UPDATE" ||
    target.kind !== "ARTIFACT" ||
    !target.ref ||
    target.ref !== before.artifactRef ||
    target.ref !== after.artifactRef ||
    target.ref !== parameters.artifactRef ||
    !Number.isSafeInteger(target.version) ||
    (target.version ?? 0) < 1 ||
    target.version !== operation.expectedVersion ||
    target.version !== before.version ||
    typeof before.currentRevisionRef !== "string" ||
    !before.currentRevisionRef ||
    before.currentRevisionRef !== after.previousRevisionRef ||
    typeof before.revision !== "number" ||
    !Number.isSafeInteger(before.revision) ||
    before.revision < 1 ||
    target.name !== before.fileName ||
    after.fileName !== before.fileName ||
    after.createsImmutableRevision !== true ||
    before.lifecycleState !== "ACTIVE" ||
    before.scanState !== "CLEAN" ||
    typeof before.digest !== "string" ||
    !/^sha256:[a-f0-9]{64}$/u.test(before.digest) ||
    typeof after.digest !== "string" ||
    !/^sha256:[a-f0-9]{64}$/u.test(after.digest) ||
    typeof after.sizeBytes !== "number" ||
    !Number.isSafeInteger(after.sizeBytes) ||
    after.sizeBytes < 0 ||
    after.sizeBytes > 1 << 20
  )
    return;
  return {
    artifactRef: target.ref,
    revisionRef: before.currentRevisionRef,
    revision: before.revision,
  };
}

export function appliedProjectFileRevision(
  plan: AssistantPlan,
  operationRef: string,
): { projectRef: string; revision: ArtifactRevision } | undefined {
  const operation = plan.operations.find((item) => item.ref === operationRef);
  const source = operation && projectFileRevisionSource(operation);
  const receipt = plan.receipt;
  if (
    !operation ||
    !source ||
    !operation.selected ||
    !plan.projectRef ||
    plan.state !== "APPLIED" ||
    !receipt ||
    receipt.planRef !== plan.ref ||
    receipt.planRevision !== plan.revision ||
    receipt.outcome !== "APPLIED"
  )
    return;
  const matches = receipt.operationReceipts.filter(
    (item) => item.operationRef === operationRef,
  );
  const applied = matches[0];
  const revision = applied?.artifactRevision;
  if (
    matches.length !== 1 ||
    applied?.outcome !== "APPLIED" ||
    applied.resourceRef !== source.artifactRef ||
    !revision
  )
    return;
  try {
    assertArtifactRevision(revision, source.artifactRef);
  } catch {
    return;
  }
  if (
    revision.revision <= source.revision ||
    revision.digest !== operation.after.digest ||
    revision.fileName !== operation.after.fileName ||
    revision.mediaType !== operation.after.mediaType ||
    revision.sizeBytes !== operation.after.sizeBytes
  )
    return;
  return { projectRef: plan.projectRef, revision };
}

export function sameArtifactRevision(
  left: ArtifactRevision,
  right: ArtifactRevision,
): boolean {
  return (
    [
      "ref",
      "artifactRef",
      "revision",
      "fileName",
      "mediaType",
      "sizeBytes",
      "digest",
      "scanState",
      "source",
      "previewAvailable",
      "createdAt",
    ] as const
  ).every((key) => left[key] === right[key]);
}
