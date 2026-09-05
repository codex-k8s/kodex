import {
  createRuntimeEnvironmentDraft,
  getRuntimeEnvironmentDraft,
  saveRuntimeEnvironmentDraft,
  validateRuntimeEnvironmentDraft,
  publishRuntimeEnvironmentDraft,
  discardRuntimeEnvironmentDraft,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  RuntimeEnvironmentDraft,
  RuntimeEnvironmentDraftSpecification,
  RuntimeEnvironmentSet,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { etag, mutate } from "@/shared/api/mutation";
import { unwrap, type ApiReadback } from "@/shared/api/problem";

export function environmentDraftFingerprint(
  specification: RuntimeEnvironmentDraftSpecification,
): string {
  return JSON.stringify(specification, (_key: string, value: unknown) => {
    if (value && typeof value === "object" && !Array.isArray(value))
      return Object.fromEntries(
        Object.entries(value).sort(([left], [right]) =>
          left.localeCompare(right),
        ),
      );
    return value;
  });
}

function readback(
  result: ApiReadback<RuntimeEnvironmentDraft>,
  projectRef: string,
  draftRef?: string,
): RuntimeEnvironmentDraft {
  const draft = result.data;
  if (
    draft.projectRef !== projectRef ||
    (draftRef && draft.ref !== draftRef) ||
    !draft.ref ||
    result.etag !== etag(draft.version) ||
    (["VALID", "PUBLISHED"].includes(draft.state) && !draft.validationDigest) ||
    (draft.state === "PUBLISHED" && !draft.publishedEnvironmentRef)
  )
    throw new Error("Invalid runtime environment draft readback");
  return draft;
}
export async function readEnvironmentDraft(
  projectRef: string,
  draftRef: string,
  signal: AbortSignal,
): Promise<RuntimeEnvironmentDraft> {
  return readback(
    await unwrap(
      getRuntimeEnvironmentDraft({
        path: { draftRef },
        signal: requestSignal(signal),
      }),
    ),
    projectRef,
    draftRef,
  );
}
export async function createEnvironmentDraft(
  projectRef: string,
  specification: RuntimeEnvironmentDraftSpecification,
  signal: AbortSignal,
  environment?: Pick<RuntimeEnvironmentSet, "ref" | "version">,
): Promise<RuntimeEnvironmentDraft> {
  const result = await mutate((headers) =>
    createRuntimeEnvironmentDraft({
      headers: { ...headers },
      path: { projectRef },
      body: {
        specification,
        ...(environment
          ? {
              environmentRef: environment.ref,
              expectedEnvironmentVersion: environment.version,
            }
          : {}),
      },
      signal: requestSignal(signal),
    }),
  );
  const draft = readback(result, projectRef);
  if (
    draft.state !== "DRAFT" ||
    (draft.environmentRef || undefined) !== environment?.ref ||
    draft.expectedEnvironmentVersion !== (environment?.version ?? 0)
  )
    throw new Error("Invalid runtime environment draft origin");
  return draft;
}
export async function saveEnvironmentDraft(
  draft: RuntimeEnvironmentDraft,
  specification: RuntimeEnvironmentDraftSpecification,
  signal: AbortSignal,
): Promise<RuntimeEnvironmentDraft> {
  const result = await mutate(
    (headers) =>
      saveRuntimeEnvironmentDraft({
        headers: { ...headers, "If-Match": etag(draft.version) },
        path: { draftRef: draft.ref },
        body: specification,
        signal: requestSignal(signal),
      }),
    draft.version,
  );
  const saved = readback(result, draft.projectRef, draft.ref);
  if (saved.state !== "DRAFT")
    throw new Error("Invalid runtime environment save state");
  return saved;
}
export async function transitionEnvironmentDraft(
  action: "validate" | "publish" | "discard",
  draft: RuntimeEnvironmentDraft,
  signal: AbortSignal,
): Promise<RuntimeEnvironmentDraft> {
  const operation = {
    validate: validateRuntimeEnvironmentDraft,
    publish: publishRuntimeEnvironmentDraft,
    discard: discardRuntimeEnvironmentDraft,
  }[action];
  const result = await mutate(
    (headers) =>
      operation({
        headers: { ...headers, "If-Match": etag(draft.version) },
        path: { draftRef: draft.ref },
        signal: requestSignal(signal),
      }),
    draft.version,
  );
  const saved = readback(result, draft.projectRef, draft.ref);
  if (
    !(
      action === "validate"
        ? ["VALID", "INVALID"]
        : action === "publish"
          ? ["PUBLISHED"]
          : ["DISCARDED"]
    ).includes(saved.state)
  )
    throw new Error("Invalid runtime environment draft transition");
  return saved;
}
