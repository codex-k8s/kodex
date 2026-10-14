import type {
  RoleImageArtifact,
  RuntimeEnvironmentImage,
  RuntimeEnvironmentTool,
} from "@/shared/api/generated/openapi/types.gen";
import { AppProblem } from "@/shared/api/problem";
import {
  verifiedImageInventoryAvailable,
  verifiedImageTools,
} from "@/shared/lib/verified-image-tools";
import type {
  AsyncEntityOption,
  AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";
import type { RuntimeResourceScope } from "./resource-scope";

export interface RuntimeImageOption extends AsyncEntityOption {
  recipeRef: string;
  generation: number;
}

// Только подписи текущей страницы; идентичность и проверка выбора не меняются.
export function runtimeImagePagePresentation<T extends AsyncEntityOption>(
  items: readonly T[],
  generationLabel: (generation: number) => string,
): (T & AsyncEntityOption)[] {
  const recipeRef = (item: AsyncEntityOption): string | undefined =>
    "recipeRef" in item && typeof item.recipeRef === "string" && item.recipeRef
      ? item.recipeRef
      : undefined;
  const shortRef = (value: string): string =>
    value.length > 12 ? `…${value.slice(-8)}` : value;
  const identities = new Map<string, Set<string>>();
  for (const item of items) {
    const ref = recipeRef(item);
    if (!ref) continue;
    const label = shortRef(ref);
    const refs = identities.get(label) ?? new Set<string>();
    refs.add(ref);
    identities.set(label, refs);
  }
  return items.map((item) => {
    const ref = recipeRef(item);
    if (
      !ref ||
      !("generation" in item) ||
      typeof item.generation !== "number" ||
      !Number.isSafeInteger(item.generation) ||
      item.generation < 1
    )
      return item;
    const short = shortRef(ref);
    const identity = identities.get(short)?.size === 1 ? short : ref;
    return {
      ...item,
      description: `${generationLabel(item.generation)} · ${identity}`,
      meta: undefined,
      tooltip: [item.title, ref, item.description].filter(Boolean).join("\n"),
    };
  });
}

export interface RuntimeImageCatalog {
  loadPage(
    scope: RuntimeResourceScope,
    query: string,
    cursor: string | undefined,
    signal: AbortSignal,
    pageSize?: number,
  ): Promise<AsyncEntityOptionPage>;
  loadArtifact(
    scope: RuntimeResourceScope,
    recipeRef: string,
    artifactRef: string,
    signal: AbortSignal,
  ): Promise<{ artifact: RoleImageArtifact; recipeName: string }>;
}

export function assertPromotedRuntimeImage(
  artifact: RoleImageArtifact,
  expected: Pick<
    RuntimeEnvironmentImage,
    "artifactRef" | "recipeRef" | "recipeGeneration"
  >,
): void {
  assertPromotedRuntimeImageIdentity(artifact, expected);
  if (!verifiedImageInventoryAvailable(artifact))
    throw new AppProblem({
      status: 409,
      code: "IMAGE_ARTIFACT_NOT_CURRENT",
      retryable: false,
      kind: "conflict",
    });
}

// Только проверка metadata. Выбор образа дополнительно требует VERIFIED inventory.
export function assertPromotedRuntimeImageIdentity(
  artifact: RoleImageArtifact,
  expected: Pick<
    RuntimeEnvironmentImage,
    "artifactRef" | "recipeRef" | "recipeGeneration"
  >,
): void {
  const digest = artifact.manifestDigest.replace(/^sha256:/, "");
  if (
    artifact.ref !== expected.artifactRef ||
    artifact.recipeRef !== expected.recipeRef ||
    artifact.recipeGeneration !== expected.recipeGeneration ||
    artifact.admissionVerdict !== "ACCEPTED" ||
    artifact.promotionState !== "PROMOTED" ||
    !/^[a-f0-9]{64}$/.test(digest) ||
    !artifact.promotedReference?.endsWith(`@sha256:${digest}`)
  )
    throw new AppProblem({
      status: 409,
      code: "IMAGE_ARTIFACT_NOT_CURRENT",
      retryable: false,
      kind: "conflict",
    });
}

export function toolsForRuntimeImage(
  tools: readonly RuntimeEnvironmentTool[],
  artifact: RoleImageArtifact,
): RuntimeEnvironmentTool[] {
  const commands = new Set(
    verifiedImageTools(artifact).map((tool) => tool.name),
  );
  return tools
    .filter((tool) => commands.has(tool.command))
    .map((tool) => ({ ...tool }));
}

export function runtimeImageOption(
  value: AsyncEntityOption,
): RuntimeImageOption {
  if (
    !("recipeRef" in value) ||
    typeof value.recipeRef !== "string" ||
    !value.recipeRef ||
    !("generation" in value) ||
    typeof value.generation !== "number" ||
    !Number.isSafeInteger(value.generation) ||
    value.generation < 1
  )
    throw new Error("Runtime image catalog identity is invalid");
  return { ...value, recipeRef: value.recipeRef, generation: value.generation };
}

export async function restoreRuntimeImageOption(
  catalog: RuntimeImageCatalog,
  scope: RuntimeResourceScope,
  artifactRef: string,
  signal: AbortSignal,
): Promise<RuntimeImageOption> {
  const visited = new Set<string>();
  let cursor: string | undefined;
  for (let pageNumber = 0; pageNumber < 100; pageNumber += 1) {
    signal.throwIfAborted();
    const page = await catalog.loadPage(scope, "", cursor, signal);
    signal.throwIfAborted();
    const option = page.items.find((item) => item.ref === artifactRef);
    if (option) return runtimeImageOption(option);
    if (!page.nextPageToken) break;
    if (visited.has(page.nextPageToken))
      throw new Error("Runtime image catalog returned a repeated cursor");
    cursor = page.nextPageToken;
    visited.add(cursor);
  }
  throw new AppProblem({
    status: 409,
    code: "IMAGE_ARTIFACT_NOT_CURRENT",
    retryable: false,
    kind: "conflict",
  });
}
