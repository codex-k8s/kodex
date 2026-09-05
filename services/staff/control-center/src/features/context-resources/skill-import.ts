import type {
  Artifact,
  SkillBundleSpecification,
} from "@/shared/api/generated/openapi/types.gen";
import {
  getArtifact,
  getProject,
} from "@/shared/api/generated/openapi/sdk.gen";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";
export const skillExtensions = [
  ".md",
  ".txt",
  ".json",
  ".csv",
  ".png",
  ".jpg",
  ".jpeg",
  ".webp",
];
export function skillPathBytes(path: string): number {
  return new TextEncoder().encode(path).length;
}
export function validSkillPath(path: string): boolean {
  if (!path || skillPathBytes(path) > 240 || /[\\\0\n\r:]/.test(path))
    return false;
  if (
    path
      .split("/")
      .some((part) => !part || part.trim() !== part || part.startsWith("."))
  )
    return false;
  if (path.toLowerCase() === "skill.md") return path === "SKILL.md";
  return skillExtensions.some((extension) =>
    path.toLowerCase().endsWith(extension),
  );
}
export function validSkillSpecification(
  value: SkillBundleSpecification,
): boolean {
  const paths = value.files.map((file) => file.path);
  return (
    !!value.name.trim() &&
    Array.from(value.name).length <= 160 &&
    Array.from(value.description).length <= 2000 &&
    !value.name.includes("\0") &&
    !value.description.includes("\0") &&
    paths.length > 0 &&
    paths.length <= 128 &&
    paths.includes("SKILL.md") &&
    paths.every(validSkillPath) &&
    new Set(paths.map((path) => path.toLowerCase())).size === paths.length &&
    value.files.every(
      (file) =>
        !!file.artifactRef &&
        Number.isSafeInteger(file.artifactRevision) &&
        file.artifactRevision > 0,
    )
  );
}
export function importFiles(
  files: File[],
  existingPaths: string[],
  queuedBytes = 0,
): { file: File; path: string }[] {
  if (!files.length || files.length + existingPaths.length > 128)
    throw new Error("Invalid Skill file count");
  if (
    files.reduce((total, file) => total + file.size, queuedBytes) >
    64 * 1024 * 1024
  )
    throw new Error("Skill import size exceeds limit");
  const seen = new Set(existingPaths.map((path) => path.toLowerCase()));
  return files.map((file) => {
    const path = file.webkitRelativePath
      ? file.webkitRelativePath.split("/").slice(1).join("/")
      : file.name;
    const folded = path.toLowerCase();
    if (
      !validSkillPath(path) ||
      seen.has(folded) ||
      file.size > 32 << 20 ||
      (path === "SKILL.md" && file.size > 256 << 10)
    )
      throw new Error("Invalid Skill import file");
    seen.add(folded);
    return { file, path };
  });
}
export async function canUploadSkill(
  projectRef: string,
  signal: AbortSignal,
): Promise<boolean> {
  const project = (
    await unwrap(
      getProject({ path: { projectRef }, signal: requestSignal(signal) }),
    )
  ).data;
  if (project.ref !== projectRef)
    throw new Error("Invalid Skill upload project scope");
  return project.nextActions.includes("UPLOAD_ARTIFACT");
}
export function checkImportedArtifact(
  artifact: Artifact,
  projectRef: string,
  ref?: string,
  revision?: number,
): Artifact {
  if (
    artifact.projectRef !== projectRef ||
    (ref !== undefined && artifact.ref !== ref) ||
    (revision !== undefined && artifact.revision !== revision) ||
    artifact.lifecycleState !== "ACTIVE" ||
    !Number.isSafeInteger(artifact.revision) ||
    artifact.revision < 1
  )
    throw new Error("Invalid Skill upload receipt");
  return artifact;
}
export async function refreshImportedArtifact(
  artifact: Artifact,
  signal: AbortSignal,
): Promise<Artifact> {
  if (!artifact.projectRef)
    throw new Error("Skill upload project scope is unavailable");
  const result = (
    await unwrap(
      getArtifact({
        path: { artifactRef: artifact.ref },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  return checkImportedArtifact(
    result,
    artifact.projectRef,
    artifact.ref,
    artifact.revision,
  );
}
