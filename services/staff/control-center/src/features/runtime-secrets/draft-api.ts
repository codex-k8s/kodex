import { requestSignal } from "@/shared/api/client";
import {
  createRuntimeSecretDraft,
  saveRuntimeSecretDraft,
  getRuntimeSecretDraft,
  validateRuntimeSecretDraft,
  discardRuntimeSecretDraft,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  RuntimeSecretDraft,
  RuntimeSecretCreateInput,
  RuntimeSecretRotateInput,
} from "@/shared/api/generated/openapi/types.gen";
import { mutate, type MutationHeaders } from "@/shared/api/mutation";
import { AppProblem, asProblem, unwrap } from "@/shared/api/problem";

export type { RuntimeSecretDraft };

export function safeDraftProblem(error: unknown): AppProblem {
  const problem = asProblem(error);
  return new AppProblem({
    status: problem.status,
    code: problem.code,
    kind: problem.kind,
    retryable: problem.retryable,
  });
}

export function checkedDraft(
  draft: RuntimeSecretDraft | null | undefined,
  projectRef: string,
  expected?: { ref?: string; secretRef?: string },
): RuntimeSecretDraft {
  if (
    !draft ||
    !draft.ref ||
    !draft.secretRef ||
    draft.projectRef !== projectRef ||
    (expected?.ref && draft.ref !== expected.ref) ||
    (expected?.secretRef && draft.secretRef !== expected.secretRef) ||
    ![draft.version, draft.generation, draft.secretVersion].every(
      (value) => Number.isSafeInteger(value) && value > 0,
    ) ||
    !Number.isSafeInteger(draft.publishedRevision) ||
    draft.publishedRevision < 0 ||
    !["STRING", "JSON", "BINARY"].includes(draft.valueType) ||
    ![
      "PREPARING",
      "DRAFT",
      "VALID",
      "PUBLISHING",
      "PUBLISHED",
      "DISCARDED",
      "EXPIRED",
      "FAILED",
    ].includes(draft.state) ||
    ![draft.createdAt, draft.updatedAt, draft.expiresAt].every(
      (value) =>
        typeof value === "string" && Number.isFinite(Date.parse(value)),
    ) ||
    typeof draft.name !== "string" ||
    typeof draft.description !== "string"
  )
    throw new Error("Runtime secret draft receipt is invalid");
  // В состояние формы попадает только закрытый набор безопасных метаданных.
  return {
    ref: draft.ref,
    version: draft.version,
    generation: draft.generation,
    projectRef: draft.projectRef,
    secretRef: draft.secretRef,
    secretVersion: draft.secretVersion,
    name: draft.name,
    description: draft.description,
    valueType: draft.valueType,
    state: draft.state,
    publishedRevision: draft.publishedRevision,
    createdAt: draft.createdAt,
    updatedAt: draft.updatedAt,
    expiresAt: draft.expiresAt,
  };
}

function versioned(headers: MutationHeaders) {
  if (!headers["If-Match"]) throw new Error("Draft version is unavailable");
  return { ...headers, "If-Match": headers["If-Match"] };
}

export async function createSecretDraft(
  projectRef: string,
  input: RuntimeSecretCreateInput,
  key: string,
): Promise<RuntimeSecretDraft> {
  const result = await mutate(
    (headers) =>
      createRuntimeSecretDraft({
        path: { projectRef },
        body: input,
        headers: {
          "Idempotency-Key": headers["Idempotency-Key"],
          "X-CSRF-Token": headers["X-CSRF-Token"],
        },
        signal: requestSignal(),
      }),
    undefined,
    key,
  );
  return checkedDraft(result.data, projectRef);
}

export async function saveSecretDraft(
  projectRef: string,
  secret: { ref: string; version: number },
  input: RuntimeSecretRotateInput,
  key: string,
): Promise<RuntimeSecretDraft> {
  const result = await mutate(
    (headers) =>
      saveRuntimeSecretDraft({
        path: { secretRef: secret.ref },
        body: input,
        headers: versioned(headers),
        signal: requestSignal(),
      }),
    secret.version,
    key,
  );
  return checkedDraft(result.data, projectRef, { secretRef: secret.ref });
}

export async function readSecretDraft(
  projectRef: string,
  draftRef: string,
  signal: AbortSignal,
): Promise<RuntimeSecretDraft> {
  const result = await unwrap(
    getRuntimeSecretDraft({
      path: { draftRef },
      signal: requestSignal(signal),
      cache: "no-store",
    }),
  );
  return checkedDraft(result.data, projectRef, { ref: draftRef });
}

export async function changeSecretDraft(
  draft: RuntimeSecretDraft,
  action: "validate" | "discard",
  key: string,
): Promise<RuntimeSecretDraft> {
  const operation =
    action === "validate"
      ? validateRuntimeSecretDraft
      : discardRuntimeSecretDraft;
  const result = await mutate(
    (headers) =>
      operation({
        path: { draftRef: draft.ref },
        headers: versioned(headers),
        signal: requestSignal(),
      }),
    draft.version,
    key,
  );
  return checkedDraft(result.data, draft.projectRef, draft);
}
