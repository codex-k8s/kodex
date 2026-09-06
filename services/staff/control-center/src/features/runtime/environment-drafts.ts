import {
  createRuntimeEnvironmentDraft,
  getRuntimeEnvironmentDraft,
  saveRuntimeEnvironmentDraft,
  validateRuntimeEnvironmentDraft,
  publishRuntimeEnvironmentDraft,
  prepareEnvironmentDraftImpact,
  discardRuntimeEnvironmentDraft,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  RuntimeEnvironmentDraft,
  RuntimeEnvironmentDraftSpecification,
  RuntimeEnvironmentSet,
  RevisionImpactPlan,
  RuntimeEnvironmentPublicationResult,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { etag, mutate } from "@/shared/api/mutation";
import { unwrap, type ApiReadback } from "@/shared/api/problem";
import {
  checkedPublicationPlan,
  publicationPlanIdentity,
  publicationSelection,
} from "./publication-impact";

export async function prepareEnvironmentPublication(
  draft: RuntimeEnvironmentDraft,
  signal: AbortSignal,
): Promise<RevisionImpactPlan> {
  const fresh = await readEnvironmentDraft(draft.projectRef, draft.ref, signal);
  if (
    fresh.version !== draft.version ||
    fresh.state !== "VALID" ||
    !fresh.validationDigest
  )
    throw new Error("Environment draft changed before impact preparation");
  const plan = checkedPublicationPlan(
    (
      await mutate(
        (headers) =>
          prepareEnvironmentDraftImpact({
            path: { draftRef: fresh.ref },
            headers: { ...headers, "If-Match": etag(fresh.version) },
            signal: requestSignal(signal),
          }),
        fresh.version,
      )
    ).data,
  );
  if (
    plan.kind !== "RUNTIME_ENVIRONMENT" ||
    plan.draftRef !== fresh.ref ||
    plan.draftVersion !== fresh.version ||
    (plan.sourceRef ?? "") !== (fresh.environmentRef ?? "") ||
    plan.sourceVersion !== fresh.expectedEnvironmentVersion ||
    (plan.sourceRevisionRef ?? "") !== (fresh.baseVersionRef ?? "") ||
    plan.state !== "PREPARED"
  )
    throw new Error("Environment publication plan mismatch");
  return plan;
}

export async function publishEnvironmentDraft(
  draft: RuntimeEnvironmentDraft,
  plan: RevisionImpactPlan,
  selected: string[],
  signal: AbortSignal,
  key: string,
): Promise<RuntimeEnvironmentPublicationResult> {
  if (
    plan.kind !== "RUNTIME_ENVIRONMENT" ||
    plan.draftRef !== draft.ref ||
    plan.draftVersion !== draft.version
  )
    throw new Error("Environment publication intent mismatch");
  const result = (
    await mutate(
      (headers) =>
        publishRuntimeEnvironmentDraft({
          path: { draftRef: draft.ref },
          body: publicationSelection(plan, selected),
          headers: { ...headers, "If-Match": etag(draft.version) },
          signal: requestSignal(signal),
        }),
      draft.version,
      key,
    )
  ).data;
  checkedPublicationPlan(result.plan);
  if (
    publicationPlanIdentity(result.plan) !== publicationPlanIdentity(plan) ||
    result.plan.state !== "APPLIED" ||
    result.draft.ref !== draft.ref ||
    result.draft.projectRef !== draft.projectRef ||
    result.draft.version !== draft.version + 1 ||
    result.draft.state !== "PUBLISHED" ||
    result.draft.publishedEnvironmentRef !== result.environment.ref ||
    result.environment.projectRef !== draft.projectRef ||
    result.environment.currentVersion.ref !==
      result.plan.publishedRevisionRef ||
    result.environment.currentVersion.digest !== plan.targetDigest
  )
    throw new Error("Environment publication receipt mismatch");
  return result;
}

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
  action: "validate" | "discard",
  draft: RuntimeEnvironmentDraft,
  signal: AbortSignal,
): Promise<RuntimeEnvironmentDraft> {
  const operation = {
    validate: validateRuntimeEnvironmentDraft,
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
    !(action === "validate" ? ["VALID", "INVALID"] : ["DISCARDED"]).includes(
      saved.state,
    )
  )
    throw new Error("Invalid runtime environment draft transition");
  return saved;
}
