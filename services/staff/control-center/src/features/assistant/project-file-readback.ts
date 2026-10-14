import {
  readArtifactRevision,
  readProjectArtifact,
} from "@/shared/api/artifact-revisions";
import type { ArtifactRevision } from "@/shared/api/generated/openapi/types.gen";
import { sameArtifactRevision } from "./project-file-plan";

export async function readAppliedProjectFileRevision(
  projectRef: string,
  receipt: ArtifactRevision,
  signal: AbortSignal,
): Promise<ArtifactRevision> {
  await readProjectArtifact(projectRef, receipt.artifactRef, signal);
  const revision = await readArtifactRevision(
    receipt.artifactRef,
    receipt.ref,
    signal,
  );
  if (!sameArtifactRevision(receipt, revision))
    throw new Error("Assistant file revision receipt readback mismatch");
  return revision;
}
